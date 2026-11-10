package vulkan

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/go-gfx/vulkan/internal/vk"
)

// fakeLoader is a Vulkan driver in Go: it hands out handles, counts the live
// ones, and can fail the nth fallible call. It lets every pure-Go part of the
// binding, and every error path, run on any platform without a GPU.
type fakeLoader struct {
	failAt  int // fail the call numbered failAt (1-based); 0 = never
	failR   vk.Result
	calls   int
	next    uint64
	live    map[string]int
	maxLive map[string]int

	layers      []string
	incomplete  int // answer VK_INCOMPLETE to this many two-call enumerations
	devs        []fakeDev
	families    []vk.QueueFamilyProperties
	memTypes    []vk.MemoryType
	typeBits    uint32
	reqSize     uint64
	nilMap      bool // MapMemory succeeds with a NULL pointer
	instErr     error
	devErr      error
	closeErr    error
	closed      int
	waitResult  vk.Result
	fenceResult vk.Result
	backing     map[vk.DeviceMemory][]byte
	writes      []vk.WriteDescriptorSet
	dispatches  [][3]uint32
	barriers    []fakeBarrier
	copies      []vk.BufferCopy
	enabled     *vk.PhysicalDeviceFeatures // what CreateDevice was asked to enable
}

// fakeBarrier is one vkCmdPipelineBarrier with a global memory barrier.
type fakeBarrier struct {
	src, dst             uint32
	srcAccess, dstAccess vk.AccessFlags
}

type fakeDev struct {
	name     string
	typ      vk.PhysicalDeviceType
	vendor   uint32
	api      uint32
	storage  uint32
	uniform  uint32
	maxAlloc uint64
	groups   [3]uint32
	robust   bool // robustBufferAccess offered
	rte      bool // shaderRoundingModeRTEFloat32
}

func newFake() *fakeLoader {
	return &fakeLoader{
		failR: vk.ErrorOutOfHostMemory, reqSize: 4096,
		live: map[string]int{}, maxLive: map[string]int{}, backing: map[vk.DeviceMemory][]byte{},
		layers: []string{"VK_LAYER_MESA_overlay", validationLayer},
		devs: []fakeDev{
			{"cpu", vk.PhysicalDeviceTypeCpu, 0x10005, 1<<22 | 3<<12, 1 << 27, 1 << 16, 1 << 30, [3]uint32{65535, 65535, 65535}, false, false},
			{"gpu", vk.PhysicalDeviceTypeDiscreteGpu, VendorAMD, 1<<22 | 3<<12, 1 << 27, 1 << 16, 1 << 30, [3]uint32{65535, 65535, 65535}, true, true},
		},
		families: []vk.QueueFamilyProperties{
			{QueueFlags: vk.QueueFlags(vk.QueueGraphicsBit | vk.QueueComputeBit), QueueCount: 1},
			{QueueFlags: vk.QueueFlags(vk.QueueComputeBit), QueueCount: 2},
		},
		memTypes: []vk.MemoryType{
			{PropertyFlags: vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit)},
			{PropertyFlags: vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit | vk.MemoryPropertyHostCoherentBit)},
		},
		typeBits: 0b11,
	}
}

func (f *fakeLoader) open() (vk.Loader, error) { return f, nil }

// step counts a fallible call and reports the injected failure.
func (f *fakeLoader) step() vk.Result {
	f.calls++
	if f.calls == f.failAt {
		return f.failR
	}
	return vk.Success
}

func (f *fakeLoader) create(kind string) uint64 {
	f.next++
	f.live[kind]++
	if f.live[kind] > f.maxLive[kind] {
		f.maxLive[kind] = f.live[kind]
	}
	return f.next
}

func (f *fakeLoader) destroy(kind string) { f.live[kind]-- }

// leaks lists kinds with a live handle after everything was closed.
func (f *fakeLoader) leaks() map[string]int {
	out := map[string]int{}
	for k, n := range f.live {
		if n != 0 {
			out[k] = n
		}
	}
	return out
}

func (f *fakeLoader) Close() error { f.closed++; return f.closeErr }

func (f *fakeLoader) Global() *vk.Global {
	return &vk.Global{
		EnumerateInstanceLayerProperties: func(n *uint32, p *vk.LayerProperties) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			if p == nil {
				*n = uint32(len(f.layers))
				return vk.Success
			}
			props := unsafe.Slice(p, *n)
			for i := range props {
				if i < len(f.layers) {
					copy(props[i].LayerName[:], f.layers[i])
				}
			}
			if f.incomplete > 0 {
				f.incomplete--
				return vk.Incomplete
			}
			*n = uint32(min(int(*n), len(f.layers)))
			return vk.Success
		},
		CreateInstance: func(ci *vk.InstanceCreateInfo, _ vk.Alloc, out *vk.Instance) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			if ci.SType != vk.StructureTypeInstanceCreateInfo || (*vk.ApplicationInfo)(ci.PApplicationInfo).ApiVersion != apiVersion12 {
				return vk.ErrorUnknown
			}
			*out = vk.Instance(f.create("instance"))
			return vk.Success
		},
		DestroyInstance: func(vk.Instance, vk.Alloc) { f.destroy("instance") },
		DestroyDevice:   func(vk.Device, vk.Alloc) { f.destroy("device") },
	}
}

func (f *fakeLoader) Instance(vk.Instance) (*vk.InstanceFuncs, error) {
	if f.instErr != nil {
		return nil, f.instErr
	}
	return &vk.InstanceFuncs{
		EnumeratePhysicalDevices: func(_ vk.Instance, n *uint32, p *vk.PhysicalDevice) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			if p == nil {
				*n = uint32(len(f.devs))
				return vk.Success
			}
			out := unsafe.Slice(p, *n)
			for i := range min(len(out), len(f.devs)) {
				out[i] = vk.PhysicalDevice(i + 1)
			}
			if f.incomplete > 0 {
				f.incomplete--
				return vk.Incomplete
			}
			*n = uint32(min(int(*n), len(f.devs)))
			return vk.Success
		},
		GetPhysicalDeviceProperties2: func(h vk.PhysicalDevice, p2 *vk.PhysicalDeviceProperties2) {
			d := f.devs[h-1]
			p := &p2.Properties
			copy(p.DeviceName[:], d.name)
			p.DeviceType, p.VendorID, p.DeviceID, p.ApiVersion, p.DriverVersion = d.typ, d.vendor, 7, d.api, 1<<22|2<<12|3
			p.Limits.MaxStorageBufferRange, p.Limits.MaxUniformBufferRange = d.storage, d.uniform
			p.Limits.MaxComputeWorkGroupCount = d.groups
			maint := (*vk.PhysicalDeviceMaintenance3Properties)(p2.PNext)
			maint.MaxMemoryAllocationSize = vk.DeviceSize(d.maxAlloc)
			drv := (*vk.PhysicalDeviceDriverProperties)(maint.PNext)
			fc := (*vk.PhysicalDeviceFloatControlsProperties)(drv.PNext)
			if fc.SType != vk.StructureTypePhysicalDeviceFloatControlsProperties {
				panic("the property chain lacks the float controls")
			}
			if d.rte {
				fc.ShaderRoundingModeRTEFloat32 = 1
			}
			if maint.SType != vk.StructureTypePhysicalDeviceMaintenance3Properties || drv.SType != vk.StructureTypePhysicalDeviceDriverProperties {
				panic("the property chain is malformed")
			}
			copy(drv.DriverName[:], "fakedrv")
			copy(drv.DriverInfo[:], "fake 1.0")
		},
		GetPhysicalDeviceFeatures: func(h vk.PhysicalDevice, ft *vk.PhysicalDeviceFeatures) {
			if f.devs[h-1].robust {
				ft.RobustBufferAccess = 1
			}
		},
		GetPhysicalDeviceMemoryProperties: func(_ vk.PhysicalDevice, m *vk.PhysicalDeviceMemoryProperties) {
			m.MemoryTypeCount = uint32(len(f.memTypes))
			copy(m.MemoryTypes[:], f.memTypes)
		},
		GetPhysicalDeviceQueueFamilyProperties: func(_ vk.PhysicalDevice, n *uint32, p *vk.QueueFamilyProperties) {
			if p == nil {
				*n = uint32(len(f.families))
				return
			}
			copy(unsafe.Slice(p, *n), f.families)
		},
		CreateDevice: func(_ vk.PhysicalDevice, ci *vk.DeviceCreateInfo, _ vk.Alloc, out *vk.Device) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			q := (*vk.DeviceQueueCreateInfo)(ci.PQueueCreateInfos)
			if ci.QueueCreateInfoCount != 1 || q.QueueCount != 1 || *(*float32)(q.PQueuePriorities) != 1 {
				return vk.ErrorUnknown
			}
			f.enabled = nil
			if ci.PEnabledFeatures != nil {
				v := *(*vk.PhysicalDeviceFeatures)(ci.PEnabledFeatures)
				f.enabled = &v
			}
			*out = vk.Device(f.create("device"))
			return vk.Success
		},
	}, nil
}

func (f *fakeLoader) Device(vk.Instance, vk.Device) (*vk.DeviceFuncs, error) {
	if f.devErr != nil {
		return nil, f.devErr
	}
	mk := func(kind string, out *uint64) vk.Result {
		if r := f.step(); r != vk.Success {
			return r
		}
		*out = f.create(kind)
		return vk.Success
	}
	return &vk.DeviceFuncs{
		GetDeviceQueue: func(_ vk.Device, _, _ uint32, q *vk.Queue) { *q = 99 },
		CreateBuffer: func(_ vk.Device, ci *vk.BufferCreateInfo, _ vk.Alloc, out *vk.Buffer) vk.Result {
			var h uint64
			r := mk("buffer", &h)
			*out = vk.Buffer(h)
			return r
		},
		DestroyBuffer: func(vk.Device, vk.Buffer, vk.Alloc) { f.destroy("buffer") },
		GetBufferMemoryRequirements: func(_ vk.Device, _ vk.Buffer, r *vk.MemoryRequirements) {
			r.Size, r.Alignment, r.MemoryTypeBits = vk.DeviceSize(f.reqSize), 64, f.typeBits
		},
		AllocateMemory: func(_ vk.Device, ai *vk.MemoryAllocateInfo, _ vk.Alloc, out *vk.DeviceMemory) vk.Result {
			var h uint64
			r := mk("memory", &h)
			*out = vk.DeviceMemory(h)
			f.backing[*out] = make([]byte, ai.AllocationSize)
			return r
		},
		FreeMemory:       func(vk.Device, vk.DeviceMemory, vk.Alloc) { f.destroy("memory") },
		BindBufferMemory: func(vk.Device, vk.Buffer, vk.DeviceMemory, uint64) vk.Result { return f.step() },
		MapMemory: func(_ vk.Device, m vk.DeviceMemory, _, _ uint64, _ uint32, out *unsafe.Pointer) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			if !f.nilMap {
				*out = unsafe.Pointer(&f.backing[m][0])
			}
			return vk.Success
		},
		UnmapMemory: func(vk.Device, vk.DeviceMemory) {},
		CreateShaderModule: func(_ vk.Device, ci *vk.ShaderModuleCreateInfo, _ vk.Alloc, out *vk.ShaderModule) vk.Result {
			var h uint64
			r := mk("shader", &h)
			*out = vk.ShaderModule(h)
			return r
		},
		DestroyShaderModule: func(vk.Device, vk.ShaderModule, vk.Alloc) { f.destroy("shader") },
		CreateDescriptorSetLayout: func(_ vk.Device, _ *vk.DescriptorSetLayoutCreateInfo, _ vk.Alloc, out *vk.DescriptorSetLayout) vk.Result {
			var h uint64
			r := mk("setlayout", &h)
			*out = vk.DescriptorSetLayout(h)
			return r
		},
		DestroyDescriptorSetLayout: func(vk.Device, vk.DescriptorSetLayout, vk.Alloc) { f.destroy("setlayout") },
		CreatePipelineLayout: func(_ vk.Device, _ *vk.PipelineLayoutCreateInfo, _ vk.Alloc, out *vk.PipelineLayout) vk.Result {
			var h uint64
			r := mk("layout", &h)
			*out = vk.PipelineLayout(h)
			return r
		},
		DestroyPipelineLayout: func(vk.Device, vk.PipelineLayout, vk.Alloc) { f.destroy("layout") },
		CreateComputePipelines: func(_ vk.Device, _ uint64, n uint32, ci *vk.ComputePipelineCreateInfo, _ vk.Alloc, out *vk.Pipeline) vk.Result {
			var h uint64
			r := mk("pipeline", &h)
			*out = vk.Pipeline(h)
			if n != 1 || *(*byte)(ci.Stage.PName) == 0 {
				return vk.ErrorUnknown
			}
			return r
		},
		DestroyPipeline: func(vk.Device, vk.Pipeline, vk.Alloc) { f.destroy("pipeline") },
		CreateDescriptorPool: func(_ vk.Device, _ *vk.DescriptorPoolCreateInfo, _ vk.Alloc, out *vk.DescriptorPool) vk.Result {
			var h uint64
			r := mk("pool", &h)
			*out = vk.DescriptorPool(h)
			return r
		},
		DestroyDescriptorPool: func(vk.Device, vk.DescriptorPool, vk.Alloc) { f.destroy("pool") },
		AllocateDescriptorSets: func(_ vk.Device, _ *vk.DescriptorSetAllocateInfo, out *vk.DescriptorSet) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			*out = 5
			return vk.Success
		},
		UpdateDescriptorSets: func(_ vk.Device, n uint32, w *vk.WriteDescriptorSet, _ uint32, _ unsafe.Pointer) {
			f.writes = append(f.writes, unsafe.Slice(w, n)...)
		},
		CreateCommandPool: func(_ vk.Device, _ *vk.CommandPoolCreateInfo, _ vk.Alloc, out *vk.CommandPool) vk.Result {
			var h uint64
			r := mk("cmdpool", &h)
			*out = vk.CommandPool(h)
			return r
		},
		DestroyCommandPool: func(vk.Device, vk.CommandPool, vk.Alloc) { f.destroy("cmdpool") },
		AllocateCommandBuffers: func(_ vk.Device, _ *vk.CommandBufferAllocateInfo, out *vk.CommandBuffer) vk.Result {
			if r := f.step(); r != vk.Success {
				return r
			}
			*out = 6
			return vk.Success
		},
		BeginCommandBuffer: func(vk.CommandBuffer, *vk.CommandBufferBeginInfo) vk.Result { return f.step() },
		EndCommandBuffer:   func(vk.CommandBuffer) vk.Result { return f.step() },
		CmdBindPipeline:    func(vk.CommandBuffer, vk.PipelineBindPoint, vk.Pipeline) {},
		CmdBindDescriptorSets: func(vk.CommandBuffer, vk.PipelineBindPoint, vk.PipelineLayout, uint32, uint32, *vk.DescriptorSet, uint32, *uint32) {
		},
		CmdDispatch: func(_ vk.CommandBuffer, x, y, z uint32) {
			f.dispatches = append(f.dispatches, [3]uint32{x, y, z})
		},
		CmdCopyBuffer: func(_ vk.CommandBuffer, _, _ vk.Buffer, n uint32, r *vk.BufferCopy) {
			f.copies = append(f.copies, unsafe.Slice(r, n)...)
		},
		CmdPipelineBarrier: func(_ vk.CommandBuffer, src, dst, _ uint32, nm uint32, mb *vk.MemoryBarrier, _ uint32, _ unsafe.Pointer, _ uint32, _ unsafe.Pointer) {
			for _, m := range unsafe.Slice(mb, nm) {
				f.barriers = append(f.barriers, fakeBarrier{src, dst, m.SrcAccessMask, m.DstAccessMask})
			}
		},
		QueueSubmit:    func(vk.Queue, uint32, *vk.SubmitInfo, vk.Fence) vk.Result { return f.step() },
		QueueWaitIdle:  func(vk.Queue) vk.Result { return vk.Success },
		DeviceWaitIdle: func(vk.Device) vk.Result { return f.step() },
		CreateFence: func(_ vk.Device, _ *vk.FenceCreateInfo, _ vk.Alloc, out *vk.Fence) vk.Result {
			var h uint64
			r := mk("fence", &h)
			*out = vk.Fence(h)
			return r
		},
		DestroyFence:   func(vk.Device, vk.Fence, vk.Alloc) { f.destroy("fence") },
		WaitForFences:  func(vk.Device, uint32, *vk.Fence, uint32, uint64) vk.Result { return f.waitResult },
		GetFenceStatus: func(vk.Device, vk.Fence) vk.Result { return f.fenceResult },
		ResetFences:    func(vk.Device, uint32, *vk.Fence) vk.Result { return vk.Success },
	}, nil
}

var errBoom = errors.New("boom")

// must returns v, or panics (and so fails the test) on an error.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func noerr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
