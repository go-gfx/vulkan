package vulkan

import (
	"unsafe"

	"github.com/go-gfx/vulkan/internal/vk"
)

// BufferUsage says what a buffer is for.
type BufferUsage uint32

// The usages a compute workload needs.
const (
	UsageStorage     = BufferUsage(vk.BufferUsageStorageBufferBit)
	UsageUniform     = BufferUsage(vk.BufferUsageUniformBufferBit)
	UsageTransferSrc = BufferUsage(vk.BufferUsageTransferSrcBit)
	UsageTransferDst = BufferUsage(vk.BufferUsageTransferDstBit)
)

// checkBufferSize refuses a size before anything is allocated: it must be
// positive, fit a Go slice, and respect the limits the device reports for the
// usage (maxStorageBufferRange, maxUniformBufferRange) and for any allocation
// (maxMemoryAllocationSize). The comparisons are on uint64, so no 32-bit
// wrap-around can make a huge request look small.
func checkBufferSize(size uint64, usage BufferUsage, lim DeviceInfo) error {
	switch {
	case size == 0, usage == 0:
		return ErrInvalid
	case size > maxInt:
		return ErrTooLarge
	case usage&UsageStorage != 0 && size > uint64(lim.MaxStorageBufferRange):
		return ErrTooLarge
	case usage&UsageUniform != 0 && size > uint64(lim.MaxUniformBufferRange):
		return ErrTooLarge
	case size > lim.MaxMemoryAllocationSize:
		return ErrTooLarge
	}
	return nil
}

// memoryType picks the first memory type allowed by typeBits that is host
// visible and host coherent, so that neither a flush nor an invalidate is
// ever needed.
func memoryType(mem *vk.PhysicalDeviceMemoryProperties, typeBits uint32) (uint32, bool) {
	want := vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit | vk.MemoryPropertyHostCoherentBit)
	for i := uint32(0); i < mem.MemoryTypeCount && i < uint32(len(mem.MemoryTypes)); i++ {
		if typeBits&(1<<i) != 0 && mem.MemoryTypes[i].PropertyFlags&want == want {
			return i, true
		}
	}
	return 0, false
}

// Buffer is a VkBuffer bound to host-visible, host-coherent memory that stays
// mapped for its whole life.
type Buffer struct {
	d      *Device
	buf    vk.Buffer
	mem    vk.DeviceMemory
	usage  BufferUsage
	data   []byte
	closed bool
}

// NewBuffer creates a buffer of size bytes. The size is checked against the
// device limits first; every handle made on the way is destroyed on an error.
func (d *Device) NewBuffer(size uint64, usage BufferUsage) (*Buffer, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if err := checkBufferSize(size, usage, d.pd.Info); err != nil {
		return nil, err
	}
	ci := vk.BufferCreateInfo{
		SType:       vk.StructureTypeBufferCreateInfo,
		Size:        vk.DeviceSize(size),
		Usage:       vk.BufferUsageFlags(usage),
		SharingMode: vk.SharingModeExclusive,
	}
	b := &Buffer{d: d, usage: usage}
	if err := check("vkCreateBuffer", d.f.CreateBuffer(d.h, &ci, nil, &b.buf)); err != nil {
		return nil, err
	}
	var req vk.MemoryRequirements
	d.f.GetBufferMemoryRequirements(d.h, b.buf, &req)
	// The allocation can be larger than the buffer (alignment): check it too.
	if uint64(req.Size) > d.pd.Info.MaxMemoryAllocationSize {
		d.f.DestroyBuffer(d.h, b.buf, nil)
		return nil, ErrTooLarge
	}
	mt, ok := memoryType(&d.mem, req.MemoryTypeBits)
	if !ok {
		d.f.DestroyBuffer(d.h, b.buf, nil)
		return nil, ErrNoMemoryType
	}
	ai := vk.MemoryAllocateInfo{
		SType:           vk.StructureTypeMemoryAllocateInfo,
		AllocationSize:  req.Size,
		MemoryTypeIndex: mt,
	}
	if err := check("vkAllocateMemory", d.f.AllocateMemory(d.h, &ai, nil, &b.mem)); err != nil {
		d.f.DestroyBuffer(d.h, b.buf, nil)
		return nil, err
	}
	if err := check("vkBindBufferMemory", d.f.BindBufferMemory(d.h, b.buf, b.mem, 0)); err != nil {
		b.release()
		return nil, err
	}
	var p unsafe.Pointer
	if err := check("vkMapMemory", d.f.MapMemory(d.h, b.mem, 0, uint64(vk.WholeSize), 0, &p)); err != nil {
		b.release()
		return nil, err
	}
	if p == nil {
		b.release()
		return nil, &Error{Op: "vkMapMemory", Result: ErrMemoryMapFailed.Result}
	}
	b.data = unsafe.Slice((*byte)(p), int(size))
	return b, nil
}

func (b *Buffer) release() {
	b.d.f.FreeMemory(b.d.h, b.mem, nil)
	b.d.f.DestroyBuffer(b.d.h, b.buf, nil)
}

// Bytes is the mapped memory: len(Bytes()) is the size asked for. It is valid
// until Close, and writes by the host are visible to the device at the next
// submission (the memory is coherent).
func (b *Buffer) Bytes() []byte { return b.data }

// Size is the size in bytes.
func (b *Buffer) Size() uint64 { return uint64(len(b.data)) }

// Close unmaps and frees the buffer. It is safe to call twice, and a no-op
// once the device is closed.
func (b *Buffer) Close() {
	if b.closed || b.d.closed {
		return
	}
	b.closed = true
	b.d.f.UnmapMemory(b.d.h, b.mem)
	b.release()
	b.data = nil
}
