package vk

import (
	"reflect"
	"testing"
	"unsafe"
)

// TestSpecificationValues pins constants against values stated by the Vulkan
// specification (the generator's source is the registry; these are the
// spec's own numbers).
func TestSpecificationValues(t *testing.T) {
	for name, c := range map[string][2]int64{
		"VK_SUCCESS":                           {int64(Success), 0},
		"VK_ERROR_DEVICE_LOST":                 {int64(ErrorDeviceLost), -4},
		"VK_ERROR_OUT_OF_DEVICE_MEMORY":        {int64(ErrorOutOfDeviceMemory), -2},
		"VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO": {int64(StructureTypeBufferCreateInfo), 12},
		"VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU": {int64(PhysicalDeviceTypeDiscreteGpu), 2},
		"VK_BUFFER_USAGE_STORAGE_BUFFER_BIT":   {int64(BufferUsageStorageBufferBit), 0x20},
		"VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT":  {int64(MemoryPropertyHostVisibleBit), 2},
		"VK_MEMORY_PROPERTY_HOST_COHERENT_BIT": {int64(MemoryPropertyHostCoherentBit), 4},
		"VK_QUEUE_COMPUTE_BIT":                 {int64(QueueComputeBit), 2},
		"VK_SHADER_STAGE_COMPUTE_BIT":          {int64(ShaderStageComputeBit), 0x20},
		"VK_DESCRIPTOR_TYPE_STORAGE_BUFFER":    {int64(DescriptorTypeStorageBuffer), 7},
		"VK_PIPELINE_BIND_POINT_COMPUTE":       {int64(PipelineBindPointCompute), 1},
		"VK_ACCESS_HOST_READ_BIT":              {int64(AccessHostReadBit), 0x2000},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %d, the specification says %d", name, c[0], c[1])
		}
	}
	if WholeSize != 1<<64-1 {
		t.Errorf("VK_WHOLE_SIZE = %#x", uint64(WholeSize))
	}
}

// TestHandlesAreOneWord: a handle is passed in one 64-bit register.
func TestHandlesAreOneWord(t *testing.T) {
	for _, v := range []any{Instance(0), PhysicalDevice(0), Device(0), Queue(0), CommandBuffer(0),
		Buffer(0), DeviceMemory(0), ShaderModule(0), Pipeline(0), PipelineLayout(0),
		DescriptorSetLayout(0), DescriptorPool(0), DescriptorSet(0), CommandPool(0), Fence(0)} {
		if s := reflect.TypeOf(v).Size(); s != 8 {
			t.Errorf("%T is %d bytes", v, s)
		}
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Fatal("the binding is 64-bit only")
	}
}

// TestFuncTablesHaveOnlyFuncs: the native loader fills every field by name, so
// a field that is not a function would panic there.
func TestFuncTablesHaveOnlyFuncs(t *testing.T) {
	for _, v := range []any{Global{}, InstanceFuncs{}, DeviceFuncs{}} {
		ty := reflect.TypeOf(v)
		for i := range ty.NumField() {
			if ty.Field(i).Type.Kind() != reflect.Func || !ty.Field(i).IsExported() {
				t.Errorf("%s.%s is not an exported func", ty, ty.Field(i).Name)
			}
		}
	}
}
