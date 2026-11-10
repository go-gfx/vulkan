package vk

import "unsafe"

//go:generate go run ../../cmd/vkgen --registry ../../registry/vk.xml.gz --out .

// Alloc is the VkAllocationCallbacks pointer every create and destroy call
// takes. The binding always passes nil: the driver allocates.
type Alloc = unsafe.Pointer

// Global holds the entry points the loader library exports: the two that need
// no instance, and the two destructors, which are exported too so that an
// object can always be destroyed, even when resolving the rest failed.
type Global struct {
	EnumerateInstanceLayerProperties func(count *uint32, props *LayerProperties) Result
	CreateInstance                   func(ci *InstanceCreateInfo, alloc Alloc, out *Instance) Result
	DestroyInstance                  func(Instance, Alloc)
	DestroyDevice                    func(Device, Alloc)
}

// InstanceFuncs holds the instance-level entry points.
type InstanceFuncs struct {
	EnumeratePhysicalDevices               func(Instance, *uint32, *PhysicalDevice) Result
	GetPhysicalDeviceProperties2           func(PhysicalDevice, *PhysicalDeviceProperties2)
	GetPhysicalDeviceMemoryProperties      func(PhysicalDevice, *PhysicalDeviceMemoryProperties)
	GetPhysicalDeviceQueueFamilyProperties func(PhysicalDevice, *uint32, *QueueFamilyProperties)
	CreateDevice                           func(PhysicalDevice, *DeviceCreateInfo, Alloc, *Device) Result
}

// DeviceFuncs holds the device-level entry points, from vkGetDeviceProcAddr:
// with no layer enabled they point straight at the driver.
type DeviceFuncs struct {
	GetDeviceQueue              func(Device, uint32, uint32, *Queue)
	CreateBuffer                func(Device, *BufferCreateInfo, Alloc, *Buffer) Result
	DestroyBuffer               func(Device, Buffer, Alloc)
	GetBufferMemoryRequirements func(Device, Buffer, *MemoryRequirements)
	AllocateMemory              func(Device, *MemoryAllocateInfo, Alloc, *DeviceMemory) Result
	FreeMemory                  func(Device, DeviceMemory, Alloc)
	BindBufferMemory            func(Device, Buffer, DeviceMemory, uint64) Result
	MapMemory                   func(Device, DeviceMemory, uint64, uint64, uint32, *unsafe.Pointer) Result
	UnmapMemory                 func(Device, DeviceMemory)
	CreateShaderModule          func(Device, *ShaderModuleCreateInfo, Alloc, *ShaderModule) Result
	DestroyShaderModule         func(Device, ShaderModule, Alloc)
	CreateDescriptorSetLayout   func(Device, *DescriptorSetLayoutCreateInfo, Alloc, *DescriptorSetLayout) Result
	DestroyDescriptorSetLayout  func(Device, DescriptorSetLayout, Alloc)
	CreatePipelineLayout        func(Device, *PipelineLayoutCreateInfo, Alloc, *PipelineLayout) Result
	DestroyPipelineLayout       func(Device, PipelineLayout, Alloc)
	CreateComputePipelines      func(Device, uint64, uint32, *ComputePipelineCreateInfo, Alloc, *Pipeline) Result
	DestroyPipeline             func(Device, Pipeline, Alloc)
	CreateDescriptorPool        func(Device, *DescriptorPoolCreateInfo, Alloc, *DescriptorPool) Result
	DestroyDescriptorPool       func(Device, DescriptorPool, Alloc)
	AllocateDescriptorSets      func(Device, *DescriptorSetAllocateInfo, *DescriptorSet) Result
	UpdateDescriptorSets        func(Device, uint32, *WriteDescriptorSet, uint32, unsafe.Pointer)
	CreateCommandPool           func(Device, *CommandPoolCreateInfo, Alloc, *CommandPool) Result
	DestroyCommandPool          func(Device, CommandPool, Alloc)
	AllocateCommandBuffers      func(Device, *CommandBufferAllocateInfo, *CommandBuffer) Result
	BeginCommandBuffer          func(CommandBuffer, *CommandBufferBeginInfo) Result
	EndCommandBuffer            func(CommandBuffer) Result
	CmdBindPipeline             func(CommandBuffer, PipelineBindPoint, Pipeline)
	CmdBindDescriptorSets       func(CommandBuffer, PipelineBindPoint, PipelineLayout, uint32, uint32, *DescriptorSet, uint32, *uint32)
	CmdDispatch                 func(CommandBuffer, uint32, uint32, uint32)
	CmdPipelineBarrier          func(CommandBuffer, uint32, uint32, uint32, uint32, *MemoryBarrier, uint32, unsafe.Pointer, uint32, unsafe.Pointer)
	QueueSubmit                 func(Queue, uint32, *SubmitInfo, Fence) Result
	QueueWaitIdle               func(Queue) Result
	DeviceWaitIdle              func(Device) Result
	CreateFence                 func(Device, *FenceCreateInfo, Alloc, *Fence) Result
	DestroyFence                func(Device, Fence, Alloc)
	WaitForFences               func(Device, uint32, *Fence, uint32, uint64) Result
	GetFenceStatus              func(Device, Fence) Result
	ResetFences                 func(Device, uint32, *Fence) Result
}

// Loader gives the binding its entry points. The native loader (package
// native) opens libvulkan.so.1; the tests of the pure-Go parts pass a fake.
type Loader interface {
	Global() *Global
	Instance(Instance) (*InstanceFuncs, error)
	Device(Instance, Device) (*DeviceFuncs, error)
	// Close releases the library. The caller has destroyed every object.
	Close() error
}
