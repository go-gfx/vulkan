package vulkan

import (
	"errors"
	"fmt"

	"github.com/go-gfx/vulkan/internal/vk"
)

// Result is a VkResult. Negative values are errors, zero is success, positive
// values are successful statuses (VK_NOT_READY, VK_TIMEOUT, VK_INCOMPLETE).
type Result int32

// Error is a Vulkan call that returned a negative VkResult. Use errors.Is with
// the sentinels below to test for one code, or errors.As to read the call.
type Error struct {
	Op     string // the Vulkan command, "vkCreateBuffer"
	Result Result
}

// The VkResult errors a compute client can meet. errors.Is(err, ErrDeviceLost)
// is true for any *Error that carries that code.
var (
	ErrOutOfHostMemory      = &Error{Result: Result(vk.ErrorOutOfHostMemory)}
	ErrOutOfDeviceMemory    = &Error{Result: Result(vk.ErrorOutOfDeviceMemory)}
	ErrInitializationFailed = &Error{Result: Result(vk.ErrorInitializationFailed)}
	ErrDeviceLost           = &Error{Result: Result(vk.ErrorDeviceLost)}
	ErrMemoryMapFailed      = &Error{Result: Result(vk.ErrorMemoryMapFailed)}
	ErrLayerNotPresent      = &Error{Result: Result(vk.ErrorLayerNotPresent)}
	ErrExtensionNotPresent  = &Error{Result: Result(vk.ErrorExtensionNotPresent)}
	ErrFeatureNotPresent    = &Error{Result: Result(vk.ErrorFeatureNotPresent)}
	ErrIncompatibleDriver   = &Error{Result: Result(vk.ErrorIncompatibleDriver)}
	ErrTooManyObjects       = &Error{Result: Result(vk.ErrorTooManyObjects)}
	ErrFragmentedPool       = &Error{Result: Result(vk.ErrorFragmentedPool)}
	ErrOutOfPoolMemory      = &Error{Result: Result(vk.ErrorOutOfPoolMemory)}
	ErrUnknown              = &Error{Result: Result(vk.ErrorUnknown)}
)

// Errors of this package that are not a VkResult.
var (
	// ErrUnsupported means this platform has no binding (it is Linux, 64-bit
	// only) or the Vulkan loader library is not installed.
	ErrUnsupported = errors.ErrUnsupported
	// ErrNoDevice means no physical device matches the filter.
	ErrNoDevice = errors.New("vulkan: no matching physical device")
	// ErrTooLarge means a size exceeds a limit of the device or of the process.
	ErrTooLarge = errors.New("vulkan: size exceeds a device limit")
	// ErrInvalid means an argument the binding refuses before calling Vulkan.
	ErrInvalid = errors.New("vulkan: invalid argument")
	// ErrNoMemoryType means the device offers no host-visible coherent memory
	// for the buffer.
	ErrNoMemoryType = errors.New("vulkan: no host-visible coherent memory type")
	// ErrClosed means the object, or the device it belongs to, was closed.
	ErrClosed = errors.New("vulkan: object is closed")
)

var resultNames = map[Result]string{
	Result(vk.Success):                   "VK_SUCCESS",
	Result(vk.NotReady):                  "VK_NOT_READY",
	Result(vk.Timeout):                   "VK_TIMEOUT",
	Result(vk.Incomplete):                "VK_INCOMPLETE",
	Result(vk.ErrorOutOfHostMemory):      "VK_ERROR_OUT_OF_HOST_MEMORY",
	Result(vk.ErrorOutOfDeviceMemory):    "VK_ERROR_OUT_OF_DEVICE_MEMORY",
	Result(vk.ErrorInitializationFailed): "VK_ERROR_INITIALIZATION_FAILED",
	Result(vk.ErrorDeviceLost):           "VK_ERROR_DEVICE_LOST",
	Result(vk.ErrorMemoryMapFailed):      "VK_ERROR_MEMORY_MAP_FAILED",
	Result(vk.ErrorLayerNotPresent):      "VK_ERROR_LAYER_NOT_PRESENT",
	Result(vk.ErrorExtensionNotPresent):  "VK_ERROR_EXTENSION_NOT_PRESENT",
	Result(vk.ErrorFeatureNotPresent):    "VK_ERROR_FEATURE_NOT_PRESENT",
	Result(vk.ErrorIncompatibleDriver):   "VK_ERROR_INCOMPATIBLE_DRIVER",
	Result(vk.ErrorTooManyObjects):       "VK_ERROR_TOO_MANY_OBJECTS",
	Result(vk.ErrorFragmentedPool):       "VK_ERROR_FRAGMENTED_POOL",
	Result(vk.ErrorOutOfPoolMemory):      "VK_ERROR_OUT_OF_POOL_MEMORY",
	Result(vk.ErrorUnknown):              "VK_ERROR_UNKNOWN",
}

// String is the registry's name of the code, or "VkResult(n)".
func (r Result) String() string {
	if s, ok := resultNames[r]; ok {
		return s
	}
	return fmt.Sprintf("VkResult(%d)", int32(r))
}

// Error implements error.
func (e *Error) Error() string {
	if e.Op == "" {
		return fmt.Sprintf("vulkan: %s (%d)", e.Result, int32(e.Result))
	}
	return fmt.Sprintf("vulkan: %s: %s (%d)", e.Op, e.Result, int32(e.Result))
}

// Is matches a sentinel (an *Error with no Op) that carries the same code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Op == "" && t.Result == e.Result
}

// check maps a VkResult of command op to an error: nil for zero and positive
// statuses, an *Error for a negative code.
func check(op string, r vk.Result) error {
	if r >= 0 {
		return nil
	}
	return &Error{Op: op, Result: Result(r)}
}
