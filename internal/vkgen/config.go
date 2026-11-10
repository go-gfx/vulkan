package vkgen

// DefaultConfig is what go-gfx/vulkan calls: exactly the structures, handles
// and enumerations of the compute path, and nothing for presentation.
func DefaultConfig() Config {
	return Config{
		Package: "vk",
		Structs: []string{
			"VkApplicationInfo", "VkInstanceCreateInfo", "VkLayerProperties",
			"VkPhysicalDeviceProperties", "VkPhysicalDeviceProperties2",
			"VkPhysicalDeviceMaintenance3Properties", "VkPhysicalDeviceDriverProperties",
			"VkPhysicalDeviceMemoryProperties", "VkQueueFamilyProperties",
			"VkDeviceQueueCreateInfo", "VkDeviceCreateInfo", "VkPhysicalDeviceFeatures",
			"VkBufferCreateInfo", "VkMemoryRequirements", "VkMemoryAllocateInfo",
			"VkShaderModuleCreateInfo",
			"VkDescriptorSetLayoutBinding", "VkDescriptorSetLayoutCreateInfo",
			"VkDescriptorPoolSize", "VkDescriptorPoolCreateInfo", "VkDescriptorSetAllocateInfo",
			"VkDescriptorBufferInfo", "VkWriteDescriptorSet",
			"VkPipelineLayoutCreateInfo", "VkPipelineShaderStageCreateInfo", "VkComputePipelineCreateInfo",
			"VkCommandPoolCreateInfo", "VkCommandBufferAllocateInfo", "VkCommandBufferBeginInfo",
			"VkSubmitInfo", "VkFenceCreateInfo", "VkMemoryBarrier",
			"VkBufferCopy", "VkPhysicalDeviceFloatControlsProperties",
		},
		Handles: []string{
			"VkInstance", "VkPhysicalDevice", "VkDevice", "VkQueue", "VkCommandBuffer",
			"VkBuffer", "VkDeviceMemory", "VkShaderModule", "VkPipeline", "VkPipelineLayout",
			"VkDescriptorSetLayout", "VkDescriptorPool", "VkDescriptorSet", "VkCommandPool", "VkFence",
		},
		Enums: []string{
			"VkResult", "VkPhysicalDeviceType", "VkQueueFlagBits", "VkBufferUsageFlagBits",
			"VkMemoryPropertyFlagBits", "VkMemoryHeapFlagBits", "VkDescriptorType",
			"VkShaderStageFlagBits", "VkPipelineBindPoint", "VkSharingMode",
			"VkPipelineStageFlagBits", "VkAccessFlagBits", "VkCommandBufferLevel",
			"VkCommandBufferUsageFlagBits", "VkCommandPoolCreateFlagBits",
			"VkFenceCreateFlagBits", "VkDescriptorPoolCreateFlagBits",
		},
		Constants: []string{
			"VK_WHOLE_SIZE", "VK_QUEUE_FAMILY_IGNORED", "VK_MAX_MEMORY_TYPES", "VK_MAX_MEMORY_HEAPS",
			"VK_MAX_PHYSICAL_DEVICE_NAME_SIZE", "VK_MAX_EXTENSION_NAME_SIZE", "VK_MAX_DESCRIPTION_SIZE",
			"VK_UUID_SIZE", "VK_MAX_DRIVER_NAME_SIZE", "VK_MAX_DRIVER_INFO_SIZE",
		},
	}
}
