// Package vulkan is a pure-Go (no cgo) binding to the part of Vulkan a compute
// workload needs: the loader, an instance with the optional Khronos validation
// layer, a physical-device pick, one device and one compute queue,
// host-visible coherent buffers, shader modules from SPIR-V words, compute
// pipelines with their descriptor sets, command buffers, dispatch and fences.
//
// It calls the system loader libvulkan.so.1 through purego, so a binary built
// with CGO_ENABLED=0 can be cross-compiled and copied to a machine that has a
// Vulkan driver. It is Linux 64-bit only (amd64, arm64); elsewhere Open returns
// ErrUnsupported. There is no window-system integration and no graphics
// pipeline.
//
// Threading: a queue is externally synchronised, so a Device and everything
// made from it belong to one goroutine at a time.
//
// Close order: close buffers, pipelines, shaders, command buffers and fences
// before their Device, and the Device before the Instance.
package vulkan

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/go-gfx/vulkan/internal/native"
	"github.com/go-gfx/vulkan/internal/vk"
)

// defaultLoader opens the system loader; tests replace it with a fake.
var defaultLoader = native.Open

// apiVersion12 is VK_MAKE_API_VERSION(0, 1, 2, 0), the version the instance
// asks for: Vulkan 1.2 brings properties2 and the driver properties.
const apiVersion12 = 1<<22 | 2<<12

// validationLayer is the Khronos validation layer.
const validationLayer = "VK_LAYER_KHRONOS_validation"

// Version is a packed Vulkan version (VK_MAKE_API_VERSION).
type Version uint32

// Major, Minor and Patch are the fields of the packed version.
func (v Version) Major() uint32 { return uint32(v) >> 22 & 0x7f }

// Minor is the minor number.
func (v Version) Minor() uint32 { return uint32(v) >> 12 & 0x3ff }

// Patch is the patch number.
func (v Version) Patch() uint32 { return uint32(v) & 0xfff }

// String is "major.minor.patch".
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major(), v.Minor(), v.Patch()) }

// Options configures Open.
type Options struct {
	// ApplicationName is reported to the driver. It may be empty.
	ApplicationName string
	// Validation enables VK_LAYER_KHRONOS_validation. When the layer is not
	// installed Open fails with ErrLayerNotPresent: a validation run that
	// silently validated nothing would prove nothing.
	Validation bool
}

// Instance is a VkInstance.
type Instance struct {
	ldr    vk.Loader
	g      *vk.Global
	f      *vk.InstanceFuncs
	h      vk.Instance
	closed bool
}

// Open loads libvulkan.so.1 and creates an instance.
func Open(o Options) (*Instance, error) { return openWith(defaultLoader, o) }

func openWith(open func() (vk.Loader, error), o Options) (*Instance, error) {
	ldr, err := open()
	if err != nil {
		return nil, err
	}
	in, err := newInstance(ldr, o)
	if err != nil {
		ldr.Close()
		return nil, err
	}
	return in, nil
}

func newInstance(ldr vk.Loader, o Options) (*Instance, error) {
	g := ldr.Global()
	app := vk.ApplicationInfo{
		SType:      vk.StructureTypeApplicationInfo,
		ApiVersion: apiVersion12,
	}
	name := cstr(o.ApplicationName)
	if o.ApplicationName != "" {
		app.PApplicationName = unsafe.Pointer(&name[0])
	}
	ci := vk.InstanceCreateInfo{
		SType:            vk.StructureTypeInstanceCreateInfo,
		PApplicationInfo: unsafe.Pointer(&app),
	}
	var lname []byte
	var layers []*byte
	if o.Validation {
		ok, err := hasLayer(g, validationLayer)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &Error{Op: "vkCreateInstance", Result: ErrLayerNotPresent.Result}
		}
		lname = cstr(validationLayer)
		layers = []*byte{&lname[0]}
		ci.EnabledLayerCount = 1
		ci.PpEnabledLayerNames = unsafe.Pointer(&layers[0])
	}
	var h vk.Instance
	r := g.CreateInstance(&ci, nil, &h)
	runtime.KeepAlive(name)
	runtime.KeepAlive(layers)
	runtime.KeepAlive(lname)
	runtime.KeepAlive(&app)
	if err := check("vkCreateInstance", r); err != nil {
		return nil, err
	}
	f, err := ldr.Instance(h)
	if err != nil {
		g.DestroyInstance(h, nil)
		return nil, err
	}
	return &Instance{ldr: ldr, g: g, f: f, h: h}, nil
}

// hasLayer reports whether the loader lists the named instance layer. The
// two-call idiom can race with an install, so it retries on VK_INCOMPLETE.
func hasLayer(g *vk.Global, name string) (bool, error) {
	for range 3 {
		var n uint32
		if err := check("vkEnumerateInstanceLayerProperties", g.EnumerateInstanceLayerProperties(&n, nil)); err != nil {
			return false, err
		}
		if n == 0 {
			return false, nil
		}
		props := make([]vk.LayerProperties, n)
		r := g.EnumerateInstanceLayerProperties(&n, &props[0])
		if err := check("vkEnumerateInstanceLayerProperties", r); err != nil {
			return false, err
		}
		if r == vk.Incomplete {
			continue
		}
		for i := range props[:n] {
			if cString(props[i].LayerName[:]) == name {
				return true, nil
			}
		}
		return false, nil
	}
	return false, &Error{Op: "vkEnumerateInstanceLayerProperties", Result: Result(vk.Incomplete)}
}

// Close destroys the instance and closes the loader. It is safe to call twice.
func (in *Instance) Close() error {
	if in.closed {
		return nil
	}
	in.closed = true
	in.g.DestroyInstance(in.h, nil)
	return in.ldr.Close()
}

// cstr is s with a terminating NUL, for a const char* argument.
func cstr(s string) []byte { return append([]byte(s), 0) }

// cString is the bytes up to the first NUL, from a fixed char array.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
