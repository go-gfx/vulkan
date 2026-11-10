package vulkan

import (
	"math"
	"runtime"
	"unsafe"

	"github.com/go-gfx/vulkan/internal/vk"
)

// DeviceType is VkPhysicalDeviceType.
type DeviceType int32

// The device types, in the registry's numbering.
const (
	DeviceTypeOther      = DeviceType(vk.PhysicalDeviceTypeOther)
	DeviceTypeIntegrated = DeviceType(vk.PhysicalDeviceTypeIntegratedGpu)
	DeviceTypeDiscrete   = DeviceType(vk.PhysicalDeviceTypeDiscreteGpu)
	DeviceTypeVirtual    = DeviceType(vk.PhysicalDeviceTypeVirtualGpu)
	DeviceTypeCPU        = DeviceType(vk.PhysicalDeviceTypeCpu)
)

// String names the type.
func (t DeviceType) String() string {
	switch t {
	case DeviceTypeOther:
		return "other"
	case DeviceTypeIntegrated:
		return "integrated"
	case DeviceTypeDiscrete:
		return "discrete"
	case DeviceTypeVirtual:
		return "virtual"
	case DeviceTypeCPU:
		return "cpu"
	}
	return "unknown"
}

// Well-known PCI vendor identifiers.
const (
	VendorAMD    uint32 = 0x1002
	VendorNVIDIA uint32 = 0x10de
	VendorIntel  uint32 = 0x8086
)

// DeviceInfo is what a physical device reports.
type DeviceInfo struct {
	Index         int // position in the enumeration
	Name          string
	Type          DeviceType
	VendorID      uint32
	DeviceID      uint32
	APIVersion    Version
	DriverVersion uint32 // vendor-specific encoding; Mesa drivers use Version's
	DriverName    string // from VkPhysicalDeviceDriverProperties
	DriverInfo    string

	// Limits the binding checks before it allocates or dispatches.
	MaxStorageBufferRange    uint32
	MaxUniformBufferRange    uint32
	MaxMemoryAllocationSize  uint64
	MaxComputeWorkGroupCount [3]uint32
}

// PhysicalDevice is a VkPhysicalDevice. It needs no Close.
type PhysicalDevice struct {
	in   *Instance
	h    vk.PhysicalDevice
	Info DeviceInfo
}

// Devices lists the physical devices.
func (in *Instance) Devices() ([]*PhysicalDevice, error) {
	if in.closed {
		return nil, ErrClosed
	}
	var hs []vk.PhysicalDevice
	for range 3 {
		var n uint32
		if err := check("vkEnumeratePhysicalDevices", in.f.EnumeratePhysicalDevices(in.h, &n, nil)); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, nil
		}
		hs = make([]vk.PhysicalDevice, n)
		r := in.f.EnumeratePhysicalDevices(in.h, &n, &hs[0])
		if err := check("vkEnumeratePhysicalDevices", r); err != nil {
			return nil, err
		}
		if r != vk.Incomplete {
			hs = hs[:n]
			break
		}
		hs = nil
	}
	if hs == nil {
		return nil, &Error{Op: "vkEnumeratePhysicalDevices", Result: Result(vk.Incomplete)}
	}
	out := make([]*PhysicalDevice, len(hs))
	for i, h := range hs {
		out[i] = &PhysicalDevice{in: in, h: h, Info: in.info(i, h)}
	}
	return out, nil
}

func (in *Instance) info(i int, h vk.PhysicalDevice) DeviceInfo {
	var maint vk.PhysicalDeviceMaintenance3Properties
	maint.SType = vk.StructureTypePhysicalDeviceMaintenance3Properties
	var drv vk.PhysicalDeviceDriverProperties
	drv.SType = vk.StructureTypePhysicalDeviceDriverProperties
	maint.PNext = unsafe.Pointer(&drv)
	var p2 vk.PhysicalDeviceProperties2
	p2.SType = vk.StructureTypePhysicalDeviceProperties2
	p2.PNext = unsafe.Pointer(&maint)
	in.f.GetPhysicalDeviceProperties2(h, &p2)
	runtime.KeepAlive(&maint)
	runtime.KeepAlive(&drv)
	p := &p2.Properties
	return DeviceInfo{
		Index:         i,
		Name:          cString(p.DeviceName[:]),
		Type:          DeviceType(p.DeviceType),
		VendorID:      p.VendorID,
		DeviceID:      p.DeviceID,
		APIVersion:    Version(p.ApiVersion),
		DriverVersion: p.DriverVersion,
		DriverName:    cString(drv.DriverName[:]),
		DriverInfo:    cString(drv.DriverInfo[:]),

		MaxStorageBufferRange:    p.Limits.MaxStorageBufferRange,
		MaxUniformBufferRange:    p.Limits.MaxUniformBufferRange,
		MaxMemoryAllocationSize:  uint64(maint.MaxMemoryAllocationSize),
		MaxComputeWorkGroupCount: p.Limits.MaxComputeWorkGroupCount,
	}
}

// Filter narrows Pick. The zero Filter accepts every device.
type Filter struct {
	VendorID uint32       // 0 for any vendor
	Types    []DeviceType // empty for any type
}

// rank orders the device types: a discrete GPU first, then an integrated one,
// a virtual one, a CPU implementation (lavapipe), anything else.
func rank(t DeviceType) int {
	switch t {
	case DeviceTypeDiscrete:
		return 0
	case DeviceTypeIntegrated:
		return 1
	case DeviceTypeVirtual:
		return 2
	case DeviceTypeCPU:
		return 3
	}
	return 4
}

// minAPI is the oldest device version the binding drives: properties2 is 1.1.
const minAPI = 1<<22 | 1<<12

// Pick chooses among infos: the best-ranked device that passes the filter and
// reports Vulkan 1.1 or later, the first of equals. It returns an index into
// infos, or ErrNoDevice.
func Pick(infos []DeviceInfo, f Filter) (int, error) {
	best, bestRank := -1, 0
	for i, d := range infos {
		if d.APIVersion < minAPI || (f.VendorID != 0 && d.VendorID != f.VendorID) {
			continue
		}
		if len(f.Types) > 0 && !containsType(f.Types, d.Type) {
			continue
		}
		if r := rank(d.Type); best < 0 || r < bestRank {
			best, bestRank = i, r
		}
	}
	if best < 0 {
		return 0, ErrNoDevice
	}
	return best, nil
}

func containsType(ts []DeviceType, t DeviceType) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// Pick returns the device chosen by the package-level Pick.
func (in *Instance) Pick(f Filter) (*PhysicalDevice, error) {
	ds, err := in.Devices()
	if err != nil {
		return nil, err
	}
	infos := make([]DeviceInfo, len(ds))
	for i, d := range ds {
		infos[i] = d.Info
	}
	i, err := Pick(infos, f)
	if err != nil {
		return nil, err
	}
	return ds[i], nil
}

// computeFamily returns the queue family to use: one with compute and no
// graphics when there is one (a dedicated compute queue), else the first with
// compute.
func computeFamily(fams []vk.QueueFamilyProperties) (uint32, bool) {
	first, found := uint32(0), false
	for i, q := range fams {
		flags := vk.QueueFlagBits(q.QueueFlags)
		if flags&vk.QueueComputeBit == 0 || q.QueueCount == 0 {
			continue
		}
		if flags&vk.QueueGraphicsBit == 0 {
			return uint32(i), true
		}
		if !found {
			first, found = uint32(i), true
		}
	}
	return first, found
}

// Device is a VkDevice with one compute queue.
type Device struct {
	pd     *PhysicalDevice
	f      *vk.DeviceFuncs
	h      vk.Device
	queue  vk.Queue
	family uint32
	mem    vk.PhysicalDeviceMemoryProperties
	closed bool
}

// Open creates a device with one compute queue on this physical device.
func (pd *PhysicalDevice) Open() (*Device, error) {
	in := pd.in
	if in.closed {
		return nil, ErrClosed
	}
	var n uint32
	in.f.GetPhysicalDeviceQueueFamilyProperties(pd.h, &n, nil)
	if n == 0 {
		return nil, ErrNoDevice
	}
	fams := make([]vk.QueueFamilyProperties, n)
	in.f.GetPhysicalDeviceQueueFamilyProperties(pd.h, &n, &fams[0])
	family, ok := computeFamily(fams[:n])
	if !ok {
		return nil, ErrNoDevice
	}
	prio := float32(1)
	qci := vk.DeviceQueueCreateInfo{
		SType:            vk.StructureTypeDeviceQueueCreateInfo,
		QueueFamilyIndex: family,
		QueueCount:       1,
		PQueuePriorities: unsafe.Pointer(&prio),
	}
	ci := vk.DeviceCreateInfo{
		SType:                vk.StructureTypeDeviceCreateInfo,
		QueueCreateInfoCount: 1,
		PQueueCreateInfos:    unsafe.Pointer(&qci),
	}
	var h vk.Device
	r := in.f.CreateDevice(pd.h, &ci, nil, &h)
	runtime.KeepAlive(&prio)
	runtime.KeepAlive(&qci)
	if err := check("vkCreateDevice", r); err != nil {
		return nil, err
	}
	f, err := in.ldr.Device(in.h, h)
	if err != nil {
		in.g.DestroyDevice(h, nil)
		return nil, err
	}
	d := &Device{pd: pd, f: f, h: h, family: family}
	f.GetDeviceQueue(h, family, 0, &d.queue)
	in.f.GetPhysicalDeviceMemoryProperties(pd.h, &d.mem)
	return d, nil
}

// Info is the physical device's description.
func (d *Device) Info() DeviceInfo { return d.pd.Info }

// Close waits for the queue to go idle and destroys the device. The caller
// has closed everything made from it. It is safe to call twice.
func (d *Device) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	err := check("vkDeviceWaitIdle", d.f.DeviceWaitIdle(d.h))
	d.pd.in.g.DestroyDevice(d.h, nil)
	return err
}

// maxInt is the largest slice length: buffer sizes are checked against it
// before any conversion to int.
const maxInt = uint64(math.MaxInt)
