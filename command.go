package vulkan

import (
	"runtime"
	"time"
	"unsafe"

	"github.com/go-gfx/vulkan/internal/vk"
)

// CommandBuffer is a primary command buffer with a command pool of its own.
type CommandBuffer struct {
	d         *Device
	pool      vk.CommandPool
	h         vk.CommandBuffer
	recording bool // between Begin and End
	ended     bool // End succeeded: the buffer can be submitted
	closed    bool
}

// NewCommandBuffer creates a command pool on the compute queue family and one
// command buffer from it. On an error nothing is left alive.
func (d *Device) NewCommandBuffer() (*CommandBuffer, error) {
	if d.closed {
		return nil, ErrClosed
	}
	c := &CommandBuffer{d: d}
	pci := vk.CommandPoolCreateInfo{
		SType:            vk.StructureTypeCommandPoolCreateInfo,
		Flags:            vk.CommandPoolCreateFlags(vk.CommandPoolCreateResetCommandBufferBit),
		QueueFamilyIndex: d.family,
	}
	if err := check("vkCreateCommandPool", d.f.CreateCommandPool(d.h, &pci, nil, &c.pool)); err != nil {
		return nil, err
	}
	ai := vk.CommandBufferAllocateInfo{
		SType:              vk.StructureTypeCommandBufferAllocateInfo,
		CommandPool:        c.pool,
		Level:              vk.CommandBufferLevelPrimary,
		CommandBufferCount: 1,
	}
	if err := check("vkAllocateCommandBuffers", d.f.AllocateCommandBuffers(d.h, &ai, &c.h)); err != nil {
		d.f.DestroyCommandPool(d.h, c.pool, nil)
		return nil, err
	}
	return c, nil
}

// Begin starts recording. Beginning an already recording buffer is ErrInvalid.
// The buffer is recorded for one submission at a time (reset by Begin).
func (c *CommandBuffer) Begin() error {
	if c.closed || c.d.closed {
		return ErrClosed
	}
	if c.recording {
		return ErrInvalid
	}
	bi := vk.CommandBufferBeginInfo{
		SType: vk.StructureTypeCommandBufferBeginInfo,
		Flags: vk.CommandBufferUsageFlags(vk.CommandBufferUsageOneTimeSubmitBit),
	}
	if err := check("vkBeginCommandBuffer", c.d.f.BeginCommandBuffer(c.h, &bi)); err != nil {
		return err
	}
	c.recording, c.ended = true, false
	return nil
}

// Dispatch records: bind the pipeline and its descriptor set, dispatch x*y*z
// workgroups, then a barrier that makes the shader's writes visible to the
// host, so the mapped buffers can be read after the fence. The counts are
// checked against maxComputeWorkGroupCount.
func (c *CommandBuffer) Dispatch(p *Pipeline, x, y, z uint32) error {
	if c.closed || c.d.closed || p.closed {
		return ErrClosed
	}
	if !c.recording || p.d != c.d || x == 0 || y == 0 || z == 0 {
		return ErrInvalid
	}
	lim := c.d.pd.Info.MaxComputeWorkGroupCount
	if x > lim[0] || y > lim[1] || z > lim[2] {
		return ErrTooLarge
	}
	f, h := c.d.f, c.h
	f.CmdBindPipeline(h, vk.PipelineBindPointCompute, p.pipeline)
	f.CmdBindDescriptorSets(h, vk.PipelineBindPointCompute, p.layout, 0, 1, &p.set, 0, nil)
	f.CmdDispatch(h, x, y, z)
	mb := vk.MemoryBarrier{
		SType:         vk.StructureTypeMemoryBarrier,
		SrcAccessMask: vk.AccessFlags(vk.AccessShaderWriteBit),
		DstAccessMask: vk.AccessFlags(vk.AccessHostReadBit),
	}
	f.CmdPipelineBarrier(h,
		uint32(vk.PipelineStageComputeShaderBit), uint32(vk.PipelineStageHostBit), 0,
		1, &mb, 0, nil, 0, nil)
	return nil
}

// End finishes recording.
func (c *CommandBuffer) End() error {
	if c.closed || c.d.closed {
		return ErrClosed
	}
	if !c.recording {
		return ErrInvalid
	}
	c.recording = false
	if err := check("vkEndCommandBuffer", c.d.f.EndCommandBuffer(c.h)); err != nil {
		return err
	}
	c.ended = true
	return nil
}

// Close destroys the command pool, which frees the buffer. It is safe to call
// twice, and a no-op once the device is closed. No submission may be pending.
func (c *CommandBuffer) Close() {
	if c.closed || c.d.closed {
		return
	}
	c.closed = true
	c.d.f.DestroyCommandPool(c.d.h, c.pool, nil)
}

// Fence is a VkFence that signals when a submission completes.
type Fence struct {
	d      *Device
	h      vk.Fence
	closed bool
}

// Submit sends the recorded buffer to the compute queue and returns a fence
// that signals when it has run.
func (d *Device) Submit(c *CommandBuffer) (*Fence, error) {
	if d.closed || c.closed {
		return nil, ErrClosed
	}
	if c.d != d || !c.ended {
		return nil, ErrInvalid
	}
	fence := &Fence{d: d}
	fci := vk.FenceCreateInfo{SType: vk.StructureTypeFenceCreateInfo}
	if err := check("vkCreateFence", d.f.CreateFence(d.h, &fci, nil, &fence.h)); err != nil {
		return nil, err
	}
	si := vk.SubmitInfo{
		SType:              vk.StructureTypeSubmitInfo,
		CommandBufferCount: 1,
		PCommandBuffers:    unsafe.Pointer(&c.h),
	}
	r := d.f.QueueSubmit(d.queue, 1, &si, fence.h)
	runtime.KeepAlive(&c.h)
	if err := check("vkQueueSubmit", r); err != nil {
		d.f.DestroyFence(d.h, fence.h, nil)
		return nil, err
	}
	return fence, nil
}

// Wait blocks until the fence signals or the timeout passes. It reports
// whether the fence is signalled; a timeout is (false, nil).
func (f *Fence) Wait(timeout time.Duration) (bool, error) {
	if f.closed || f.d.closed {
		return false, ErrClosed
	}
	ns := uint64(0)
	if timeout > 0 {
		ns = uint64(timeout)
	}
	r := f.d.f.WaitForFences(f.d.h, 1, &f.h, 1, ns)
	runtime.KeepAlive(&f.h)
	if err := check("vkWaitForFences", r); err != nil {
		return false, err
	}
	return r == vk.Success, nil
}

// Done polls the fence without blocking.
func (f *Fence) Done() (bool, error) {
	if f.closed || f.d.closed {
		return false, ErrClosed
	}
	r := f.d.f.GetFenceStatus(f.d.h, f.h)
	if err := check("vkGetFenceStatus", r); err != nil {
		return false, err
	}
	return r == vk.Success, nil
}

// Close destroys the fence. It is safe to call twice.
func (f *Fence) Close() {
	if f.closed || f.d.closed {
		return
	}
	f.closed = true
	f.d.f.DestroyFence(f.d.h, f.h, nil)
}
