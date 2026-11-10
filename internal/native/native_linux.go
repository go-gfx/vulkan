//go:build linux && (amd64 || arm64)

// Package native opens the system Vulkan loader, libvulkan.so.1, through
// purego and resolves the entry points of internal/vk's function tables.
//
// With CGO_ENABLED=0 purego links a fake runtime/cgo, so the Go binary becomes
// a dynamically linked executable (it names /lib64/ld-linux-x86-64.so.2 or its
// arm64 counterpart as its interpreter) and dlopen works without a C compiler.
package native

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/ebitengine/purego"

	"github.com/go-gfx/vulkan/internal/vk"
)

// libName is the versioned name the Khronos loader documentation tells a
// program that dlopen()s the loader to use ("Linux Library Name").
const libName = "libvulkan.so.1"

type loader struct {
	lib  uintptr
	gipa func(vk.Instance, *byte) uintptr // vkGetInstanceProcAddr
	gdpa func(vk.Device, *byte) uintptr   // vkGetDeviceProcAddr
	g    vk.Global
}

// Open dlopens libvulkan.so.1. A missing library is reported as an error that
// wraps errors.ErrUnsupported.
func Open() (vk.Loader, error) {
	lib, err := purego.Dlopen(libName, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("vulkan: %s: %v: %w", libName, err, errors.ErrUnsupported)
	}
	l := &loader{lib: lib}
	fail := func(err error) (vk.Loader, error) {
		_ = purego.Dlclose(lib)
		return nil, err
	}
	// The one function resolved by name from the library.
	sym, err := purego.Dlsym(lib, "vkGetInstanceProcAddr")
	if err != nil {
		return fail(fmt.Errorf("vulkan: %w", err))
	}
	purego.RegisterFunc(&l.gipa, sym)
	// The two commands that need no instance come from vkGetInstanceProcAddr(NULL, ...).
	if err := l.fill(&l.g, func(name string) uintptr { return l.gipa(0, cname(name)) }, "EnumerateInstanceLayerProperties", "CreateInstance"); err != nil {
		return fail(err)
	}
	// The destructors are exported by the loader, so that an object can be
	// destroyed even if resolving the rest of its table failed.
	for name, dst := range map[string]any{"vkDestroyInstance": &l.g.DestroyInstance, "vkDestroyDevice": &l.g.DestroyDevice} {
		p, err := purego.Dlsym(lib, name)
		if err != nil {
			return fail(fmt.Errorf("vulkan: %w", err))
		}
		purego.RegisterFunc(dst, p)
	}
	return l, nil
}

func cname(name string) *byte {
	b := append([]byte(name), 0)
	return &b[0]
}

// fill resolves the fields of the struct pointed to by table, whose names are
// the Vulkan commands without the "vk" prefix. Only the named fields when only
// is given, else every field.
func (l *loader) fill(table any, lookup func(string) uintptr, only ...string) error {
	v := reflect.ValueOf(table).Elem()
	t := v.Type()
	for i := range t.NumField() {
		name := t.Field(i).Name
		if len(only) > 0 && !slices.Contains(only, name) {
			continue
		}
		p := lookup("vk" + name)
		if p == 0 {
			return fmt.Errorf("vulkan: the loader does not provide vk%s", name)
		}
		purego.RegisterFunc(v.Field(i).Addr().Interface(), p)
	}
	return nil
}

func (l *loader) Global() *vk.Global { return &l.g }

func (l *loader) Instance(h vk.Instance) (*vk.InstanceFuncs, error) {
	f := &vk.InstanceFuncs{}
	if err := l.fill(f, func(name string) uintptr { return l.gipa(h, cname(name)) }); err != nil {
		return nil, err
	}
	// vkGetDeviceProcAddr is an instance-level command of the loader.
	p := l.gipa(h, cname("vkGetDeviceProcAddr"))
	if p == 0 {
		return nil, errors.New("vulkan: the loader does not provide vkGetDeviceProcAddr")
	}
	purego.RegisterFunc(&l.gdpa, p)
	return f, nil
}

func (l *loader) Device(_ vk.Instance, d vk.Device) (*vk.DeviceFuncs, error) {
	f := &vk.DeviceFuncs{}
	if err := l.fill(f, func(name string) uintptr { return l.gdpa(d, cname(name)) }); err != nil {
		return nil, err
	}
	return f, nil
}

// Close keeps the library mapped: drivers loaded by the Vulkan loader may keep
// threads and state alive after their instance is destroyed, and unmapping the
// code under them is a crash waiting to happen. dlopen is reference counted,
// so opening again is free.
func (l *loader) Close() error { return nil }
