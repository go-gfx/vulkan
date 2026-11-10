package vulkan

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/go-gfx/vulkan/internal/vk"
)

var goodWords = []uint32{spirvMagic, 0x00010500, 0, 8, 0}

// scenario is one whole use of the binding on the fake driver: every object is
// created, used and closed, and the first error of any step (a Close included)
// is returned.
func scenario(f *fakeLoader, o Options) (err error) {
	keep := func(e error) {
		if err == nil {
			err = e
		}
	}
	in, err := openWith(f.open, o)
	if err != nil {
		return err
	}
	defer func() { keep(in.Close()) }()
	pd, err := in.Pick(Filter{VendorID: VendorAMD, Types: []DeviceType{DeviceTypeDiscrete}})
	if err != nil {
		return err
	}
	dev, err := pd.Open()
	if err != nil {
		return err
	}
	defer func() { keep(dev.Close()) }()
	px, err := dev.NewBuffer(64, UsageStorage)
	if err != nil {
		return err
	}
	defer px.Close()
	params, err := dev.NewBuffer(16, UsageUniform)
	if err != nil {
		return err
	}
	defer params.Close()
	sh, err := dev.NewShader(goodWords)
	if err != nil {
		return err
	}
	defer sh.Close()
	pipe, err := dev.NewComputePipeline(sh, "main", []Binding{{0, StorageBuffer}, {1, UniformBuffer}})
	if err != nil {
		return err
	}
	defer pipe.Close()
	if err := pipe.Bind(0, px); err != nil {
		return err
	}
	if err := pipe.Bind(1, params); err != nil {
		return err
	}
	cb, err := dev.NewCommandBuffer()
	if err != nil {
		return err
	}
	defer cb.Close()
	if err := cb.Begin(); err != nil {
		return err
	}
	if err := cb.Dispatch(pipe, 1, 2, 3); err != nil {
		return err
	}
	if err := cb.End(); err != nil {
		return err
	}
	fence, err := dev.Submit(cb)
	if err != nil {
		return err
	}
	defer fence.Close()
	if ok, err := fence.Wait(time.Second); err != nil || !ok {
		return fmt.Errorf("wait: %v %v", ok, err)
	}
	if ok, err := fence.Done(); err != nil || !ok {
		return fmt.Errorf("done: %v %v", ok, err)
	}
	return nil
}

func TestScenarioSucceeds(t *testing.T) {
	f := newFake()
	if err := scenario(f, Options{ApplicationName: "t", Validation: true}); err != nil {
		t.Fatal(err)
	}
	if n := f.leaks(); len(n) != 0 {
		t.Fatalf("leaked: %v", n)
	}
	if len(f.writes) != 2 || len(f.dispatches) != 1 || f.dispatches[0] != [3]uint32{1, 2, 3} {
		t.Fatalf("writes=%d dispatches=%v", len(f.writes), f.dispatches)
	}
	if f.closed != 1 {
		t.Fatalf("loader closed %d times", f.closed)
	}
}

// TestEveryFailureReleasesEverything fails each fallible driver call in turn,
// on the whole scenario, and requires the error to surface and no handle to
// stay alive: the error paths destroy what they made.
func TestEveryFailureReleasesEverything(t *testing.T) {
	for _, o := range []Options{{}, {Validation: true, ApplicationName: "x"}} {
		base := newFake()
		if err := scenario(base, o); err != nil {
			t.Fatal(err)
		}
		n := base.calls
		if n < 20 {
			t.Fatalf("only %d fallible calls: the scenario no longer exercises the binding", n)
		}
		for k := 1; k <= n; k++ {
			f := newFake()
			f.failAt = k
			err := scenario(f, o)
			var e *Error
			if !errors.As(err, &e) || !errors.Is(err, ErrOutOfHostMemory) {
				t.Fatalf("failing call %d of %d: error %v", k, n, err)
			}
			if l := f.leaks(); len(l) != 0 {
				t.Fatalf("failing call %d of %d (%v) leaked %v", k, n, err, l)
			}
			if f.closed != 1 {
				t.Fatalf("failing call %d: loader closed %d times", k, f.closed)
			}
		}
	}
}

func TestOpenAndLoaderErrors(t *testing.T) {
	if _, err := openWith(func() (vk.Loader, error) { return nil, ErrUnsupported }, Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	f := newFake()
	f.instErr = errBoom
	if _, err := openWith(f.open, Options{}); !errors.Is(err, errBoom) || f.leaks() != nil && len(f.leaks()) != 0 || f.closed != 1 {
		t.Fatalf("instance table error: %v leaks=%v closed=%d", err, f.leaks(), f.closed)
	}
	f = newFake()
	f.layers = nil
	if _, err := openWith(f.open, Options{Validation: true}); !errors.Is(err, ErrLayerNotPresent) {
		t.Fatalf("no layers: %v", err)
	}
	f = newFake()
	f.layers = []string{"VK_LAYER_other"}
	if _, err := openWith(f.open, Options{Validation: true}); !errors.Is(err, ErrLayerNotPresent) {
		t.Fatalf("other layer only: %v", err)
	}
	// VK_INCOMPLETE is retried; always incomplete gives up.
	f = newFake()
	f.incomplete = 1
	in, err := openWith(f.open, Options{Validation: true})
	if err != nil {
		t.Fatal(err)
	}
	in.Close()
	f = newFake()
	f.incomplete = 100
	if _, err := openWith(f.open, Options{Validation: true}); err == nil {
		t.Fatal("endless VK_INCOMPLETE was accepted")
	}
	// Open itself: any outcome is fine where there is no Vulkan; a success closes.
	if in, err := Open(Options{}); err == nil {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstanceClosedAndTwice(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil || f.closed != 1 {
		t.Fatalf("second Close: %v, loader closed %d", err, f.closed)
	}
	if _, err := in.Devices(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := in.Pick(Filter{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	f = newFake()
	f.closeErr = errBoom
	in = must(openWith(f.open, Options{}))
	if err := in.Close(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestDevicesAndPick(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	ds := must(in.Devices())
	if len(ds) != 2 || ds[1].Info.Name != "gpu" || ds[1].Info.Index != 1 || ds[1].Info.DriverName != "fakedrv" ||
		ds[1].Info.DriverInfo != "fake 1.0" || ds[1].Info.VendorID != VendorAMD || ds[1].Info.APIVersion.String() != "1.3.0" ||
		ds[1].Info.MaxMemoryAllocationSize != 1<<30 || ds[1].Info.DriverVersion != 1<<22|2<<12|3 {
		t.Fatalf("%+v", ds)
	}
	if p := must(in.Pick(Filter{})); p.Info.Name != "gpu" {
		t.Fatalf("default pick = %q, want the discrete one", p.Info.Name)
	}
	if p := must(in.Pick(Filter{Types: []DeviceType{DeviceTypeCPU}})); p.Info.Name != "cpu" {
		t.Fatalf("cpu pick = %q", p.Info.Name)
	}
	if _, err := in.Pick(Filter{VendorID: VendorNVIDIA}); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	// Enumeration: none, retried, endless.
	f.devs = nil
	if ds, err := in.Devices(); ds != nil || err != nil {
		t.Fatalf("no devices: %v %v", ds, err)
	}
	if _, err := in.Pick(Filter{}); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	f.devs = newFake().devs
	f.incomplete = 1
	if ds := must(in.Devices()); len(ds) != 2 {
		t.Fatal(ds)
	}
	f.incomplete = 100
	if _, err := in.Devices(); err == nil {
		t.Fatal("endless VK_INCOMPLETE was accepted")
	}
}

func TestPickRanking(t *testing.T) {
	v13 := Version(1<<22 | 3<<12)
	mk := func(t DeviceType, vendor uint32, api Version) DeviceInfo {
		return DeviceInfo{Type: t, VendorID: vendor, APIVersion: api}
	}
	for _, c := range []struct {
		name  string
		infos []DeviceInfo
		f     Filter
		want  int
		err   error
	}{
		{"empty", nil, Filter{}, 0, ErrNoDevice},
		{"discrete beats integrated", []DeviceInfo{mk(DeviceTypeIntegrated, 1, v13), mk(DeviceTypeDiscrete, 2, v13)}, Filter{}, 1, nil},
		{"integrated beats virtual", []DeviceInfo{mk(DeviceTypeVirtual, 1, v13), mk(DeviceTypeIntegrated, 2, v13)}, Filter{}, 1, nil},
		{"virtual beats cpu", []DeviceInfo{mk(DeviceTypeCPU, 1, v13), mk(DeviceTypeVirtual, 2, v13)}, Filter{}, 1, nil},
		{"cpu beats other", []DeviceInfo{mk(DeviceTypeOther, 1, v13), mk(DeviceTypeCPU, 2, v13)}, Filter{}, 1, nil},
		{"first of equals", []DeviceInfo{mk(DeviceTypeDiscrete, 1, v13), mk(DeviceTypeDiscrete, 2, v13)}, Filter{}, 0, nil},
		{"vendor", []DeviceInfo{mk(DeviceTypeDiscrete, 1, v13), mk(DeviceTypeIntegrated, 2, v13)}, Filter{VendorID: 2}, 1, nil},
		{"type filter", []DeviceInfo{mk(DeviceTypeDiscrete, 1, v13), mk(DeviceTypeIntegrated, 2, v13)}, Filter{Types: []DeviceType{DeviceTypeIntegrated}}, 1, nil},
		{"vulkan 1.0 refused", []DeviceInfo{mk(DeviceTypeDiscrete, 1, 1<<22)}, Filter{}, 0, ErrNoDevice},
		{"unknown type ranks last", []DeviceInfo{mk(DeviceType(77), 1, v13), mk(DeviceTypeCPU, 1, v13)}, Filter{}, 1, nil},
	} {
		got, err := Pick(c.infos, c.f)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%s: Pick = %d, %v; want %d, %v", c.name, got, err, c.want, c.err)
		}
	}
	for ty, s := range map[DeviceType]string{DeviceTypeOther: "other", DeviceTypeIntegrated: "integrated", DeviceTypeDiscrete: "discrete",
		DeviceTypeVirtual: "virtual", DeviceTypeCPU: "cpu", 42: "unknown"} {
		if ty.String() != s {
			t.Errorf("%d.String() = %q", ty, ty.String())
		}
	}
}

func TestVersion(t *testing.T) {
	v := Version(1<<22 | 3<<12 | 296)
	if v.Major() != 1 || v.Minor() != 3 || v.Patch() != 296 || v.String() != "1.3.296" {
		t.Fatal(v.String())
	}
}

func TestComputeFamily(t *testing.T) {
	g, c := vk.QueueFlags(vk.QueueGraphicsBit), vk.QueueFlags(vk.QueueComputeBit)
	fam := func(fl vk.QueueFlags, n uint32) vk.QueueFamilyProperties {
		return vk.QueueFamilyProperties{QueueFlags: fl, QueueCount: n}
	}
	for _, tc := range []struct {
		name string
		in   []vk.QueueFamilyProperties
		want uint32
		ok   bool
	}{
		{"none", nil, 0, false},
		{"graphics only", []vk.QueueFamilyProperties{fam(g, 1)}, 0, false},
		{"empty queue count", []vk.QueueFamilyProperties{fam(c, 0)}, 0, false},
		{"first compute", []vk.QueueFamilyProperties{fam(g, 1), fam(g|c, 1), fam(g|c, 1)}, 1, true},
		{"dedicated preferred", []vk.QueueFamilyProperties{fam(g|c, 1), fam(c, 1)}, 1, true},
	} {
		got, ok := computeFamily(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %d %v", tc.name, got, ok)
		}
	}
}

func TestDeviceOpenErrors(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	pd := must(in.Pick(Filter{}))
	f.families = nil
	if _, err := pd.Open(); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	f.families = []vk.QueueFamilyProperties{{QueueFlags: vk.QueueFlags(vk.QueueGraphicsBit), QueueCount: 1}}
	if _, err := pd.Open(); !errors.Is(err, ErrNoDevice) {
		t.Fatal(err)
	}
	f.families = newFake().families
	f.devErr = errBoom
	if _, err := pd.Open(); !errors.Is(err, errBoom) || f.live["device"] != 0 {
		t.Fatalf("%v live=%d", err, f.live["device"])
	}
	f.devErr = nil
	d := must(pd.Open())
	if d.Info().Name != pd.Info.Name {
		t.Fatal(d.Info())
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	in.Close()
	if _, err := pd.Open(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestCheckBufferSize(t *testing.T) {
	lim := DeviceInfo{MaxStorageBufferRange: 1 << 27, MaxUniformBufferRange: 1 << 16, MaxMemoryAllocationSize: 1 << 30}
	for _, c := range []struct {
		name  string
		size  uint64
		usage BufferUsage
		want  error
	}{
		{"zero", 0, UsageStorage, ErrInvalid},
		{"no usage", 4, 0, ErrInvalid},
		{"fits", 1 << 27, UsageStorage, nil},
		{"storage over range", 1<<27 + 1, UsageStorage, ErrTooLarge},
		{"uniform over range", 1<<16 + 1, UsageUniform, ErrTooLarge},
		{"transfer over allocation limit", 1<<30 + 1, UsageTransferDst, ErrTooLarge},
		{"transfer at allocation limit", 1 << 30, UsageTransferDst, nil},
		{"past int", maxInt + 1, UsageTransferDst, ErrTooLarge},
		{"2^32 does not wrap to 0", 1 << 32, UsageStorage, ErrTooLarge},
		{"2^32+16 does not wrap to 16", 1<<32 + 16, UsageUniform, ErrTooLarge},
		{"max uint64", math.MaxUint64, UsageStorage, ErrTooLarge},
	} {
		if err := checkBufferSize(c.size, c.usage, lim); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

func TestMemoryType(t *testing.T) {
	var m vk.PhysicalDeviceMemoryProperties
	hv := vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit)
	hvc := hv | vk.MemoryPropertyFlags(vk.MemoryPropertyHostCoherentBit)
	m.MemoryTypeCount = 3
	m.MemoryTypes[0].PropertyFlags = vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit)
	m.MemoryTypes[1].PropertyFlags = hv // visible but not coherent: refused
	m.MemoryTypes[2].PropertyFlags = hvc
	if i, ok := memoryType(&m, 0b111); !ok || i != 2 {
		t.Fatal(i, ok)
	}
	if _, ok := memoryType(&m, 0b011); ok {
		t.Fatal("a type the buffer does not allow was chosen")
	}
	m.MemoryTypeCount = 1000 // a lying driver cannot index past the array
	if _, ok := memoryType(&m, 0); ok {
		t.Fatal("no bits allowed, yet a type was chosen")
	}
}

func TestBufferErrorsAndAccessors(t *testing.T) {
	f := newFake()
	f.reqSize = 64
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	d := must(must(in.Pick(Filter{})).Open())
	defer d.Close()

	b := must(d.NewBuffer(10, UsageStorage))
	if b.Size() != 10 || len(b.Bytes()) != 10 {
		t.Fatal(b.Size())
	}
	b.Close()
	b.Close()
	if b.Bytes() != nil {
		t.Fatal("Bytes after Close")
	}
	if f.leaks()["buffer"] != 0 {
		t.Fatal(f.leaks())
	}

	f.reqSize = 1<<30 + 1 // the allocation is larger than the buffer
	if _, err := d.NewBuffer(10, UsageStorage); !errors.Is(err, ErrTooLarge) || f.live["buffer"] != 0 || f.live["memory"] != 0 {
		t.Fatalf("%v %v", err, f.leaks())
	}
	f.reqSize = 64
	f.typeBits = 0b01 // only the device-local type
	if _, err := d.NewBuffer(10, UsageStorage); !errors.Is(err, ErrNoMemoryType) || f.live["buffer"] != 0 || f.live["memory"] != 0 {
		t.Fatalf("%v %v", err, f.leaks())
	}
	f.typeBits = 0b11
	f.nilMap = true
	if _, err := d.NewBuffer(10, UsageStorage); !errors.Is(err, ErrMemoryMapFailed) || f.live["buffer"] != 0 || f.live["memory"] != 0 {
		t.Fatalf("%v %v", err, f.leaks())
	}
	f.nilMap = false
	if _, err := d.NewBuffer(0, UsageStorage); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	d.Close()
	if _, err := d.NewBuffer(10, UsageStorage); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestShaderPipelineErrors(t *testing.T) {
	f := newFake()
	f.reqSize = 64
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	pd := must(in.Pick(Filter{}))
	d := must(pd.Open())
	defer d.Close()

	for name, w := range map[string][]uint32{
		"empty": nil, "short": goodWords[:4], "bad magic": {1, 2, 3, 4, 5}, "too long": make([]uint32, maxShaderWords+1),
	} {
		if _, err := d.NewShader(w); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	sh := must(d.NewShader(goodWords))
	two := []Binding{{0, StorageBuffer}, {1, UniformBuffer}}
	for name, c := range map[string]struct {
		entry string
		b     []Binding
	}{
		"no entry":     {"", two},
		"NUL in entry": {"a\x00b", two},
		"no bindings":  {"main", nil},
		"too many":     {"main", make([]Binding, maxBindings+1)},
		"duplicate":    {"main", []Binding{{0, StorageBuffer}, {0, UniformBuffer}}},
		"unknown kind": {"main", []Binding{{0, DescriptorKind(77)}}},
	} {
		if _, err := d.NewComputePipeline(sh, c.entry, c.b); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	d2 := must(pd.Open())
	defer d2.Close()
	if _, err := d2.NewComputePipeline(sh, "main", two); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a shader of another device: %v", err)
	}
	// An only-storage and an only-uniform pipeline (one pool size each).
	for _, b := range [][]Binding{{{0, StorageBuffer}}, {{3, UniformBuffer}}} {
		p := must(d.NewComputePipeline(sh, "main", b))
		p.Close()
		p.Close()
	}

	p := must(d.NewComputePipeline(sh, "main", two))
	st := must(d.NewBuffer(16, UsageStorage))
	un := must(d.NewBuffer(16, UsageUniform))
	other := must(d2.NewBuffer(16, UsageStorage))
	for name, c := range map[string]struct {
		binding uint32
		b       *Buffer
	}{
		"unknown binding":        {9, st},
		"storage into uniform":   {1, st},
		"uniform into storage":   {0, un},
		"buffer of other device": {0, other},
	} {
		if err := p.Bind(c.binding, c.b); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	un.Close()
	if err := p.Bind(1, un); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	sh.Close()
	sh.Close()
	if _, err := d.NewComputePipeline(sh, "main", two); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	p.Close()
	if err := p.Bind(0, st); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	st.Close()
	other.Close()
	d2.Close()
	d.Close()
	if _, err := d.NewShader(goodWords); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	// Closing children after their device is a no-op, not a crash.
	sh.Close()
	p.Close()
	st.Close()
}

func TestCommandBufferStates(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	pd := must(in.Pick(Filter{}))
	d := must(pd.Open())
	sh := must(d.NewShader(goodWords))
	p := must(d.NewComputePipeline(sh, "main", []Binding{{0, StorageBuffer}}))
	cb := must(d.NewCommandBuffer())

	if err := cb.Dispatch(p, 1, 1, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Dispatch outside Begin: %v", err)
	}
	if err := cb.End(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("End outside Begin: %v", err)
	}
	if _, err := d.Submit(cb); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Submit of a buffer never recorded: %v", err)
	}
	noerr(t, cb.Begin())
	if err := cb.Begin(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second Begin: %v", err)
	}
	if _, err := d.Submit(cb); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Submit while recording: %v", err)
	}
	for name, c := range map[string][3]uint32{"zero x": {0, 1, 1}, "zero y": {1, 0, 1}, "zero z": {1, 1, 0}} {
		if err := cb.Dispatch(p, c[0], c[1], c[2]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, c := range map[string][3]uint32{"x": {65536, 1, 1}, "y": {1, 65536, 1}, "z": {1, 1, 65536}} {
		if err := cb.Dispatch(p, c[0], c[1], c[2]); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s over maxComputeWorkGroupCount: %v", name, err)
		}
	}
	d2 := must(pd.Open())
	sh2 := must(d2.NewShader(goodWords))
	p2 := must(d2.NewComputePipeline(sh2, "main", []Binding{{0, StorageBuffer}}))
	if err := cb.Dispatch(p2, 1, 1, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pipeline of another device: %v", err)
	}
	cb2 := must(d2.NewCommandBuffer())
	noerr(t, cb2.Begin())
	noerr(t, cb2.End())
	if _, err := d.Submit(cb2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("command buffer of another device: %v", err)
	}
	noerr(t, cb.End())
	// Begin after End starts again; the first submission is of the old state.
	noerr(t, cb.Begin())
	noerr(t, cb.End())
	fence := must(d.Submit(cb))

	// Fence results.
	f.waitResult = vk.Timeout
	if ok, err := fence.Wait(-time.Second); ok || err != nil {
		t.Fatalf("timeout: %v %v", ok, err)
	}
	f.waitResult = vk.ErrorDeviceLost
	if ok, err := fence.Wait(0); ok || !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("lost: %v %v", ok, err)
	}
	f.fenceResult = vk.NotReady
	if ok, err := fence.Done(); ok || err != nil {
		t.Fatalf("not ready: %v %v", ok, err)
	}
	f.fenceResult = vk.ErrorDeviceLost
	if ok, err := fence.Done(); ok || !errors.Is(err, ErrDeviceLost) {
		t.Fatalf("lost: %v %v", ok, err)
	}

	// Everything closed, twice, then used.
	fence.Close()
	fence.Close()
	if _, err := fence.Wait(0); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := fence.Done(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	cb.Close()
	cb.Close()
	for _, err := range []error{cb.Begin(), cb.End(), cb.Dispatch(p, 1, 1, 1)} {
		if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
	if _, err := d.Submit(cb); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	p.Close()
	sh.Close()
	cb2.Close()
	p2.Close()
	sh2.Close()
	d2.Close()
	d.Close()
	if _, err := d.NewCommandBuffer(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.Submit(cb2); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := cb2.Begin(); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := fence.Wait(0); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	// Dispatch against a closed pipeline.
	if f.leaks()["device"] != 0 {
		t.Fatal(f.leaks())
	}
}

func TestDispatchOnClosedPipeline(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	d := must(must(in.Pick(Filter{})).Open())
	defer d.Close()
	sh := must(d.NewShader(goodWords))
	defer sh.Close()
	p := must(d.NewComputePipeline(sh, "main", []Binding{{0, StorageBuffer}}))
	cb := must(d.NewCommandBuffer())
	defer cb.Close()
	noerr(t, cb.Begin())
	p.Close()
	if err := cb.Dispatch(p, 1, 1, 1); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	e := &Error{Op: "vkCreateBuffer", Result: Result(vk.ErrorDeviceLost)}
	if got := e.Error(); got != "vulkan: vkCreateBuffer: VK_ERROR_DEVICE_LOST (-4)" {
		t.Fatal(got)
	}
	if got := (&Error{Result: Result(vk.ErrorDeviceLost)}).Error(); got != "vulkan: VK_ERROR_DEVICE_LOST (-4)" {
		t.Fatal(got)
	}
	if got := Result(-12345).String(); got != "VkResult(-12345)" {
		t.Fatal(got)
	}
	wrapped := fmt.Errorf("context: %w", e)
	var as *Error
	if !errors.Is(wrapped, ErrDeviceLost) || errors.Is(wrapped, ErrOutOfHostMemory) || !errors.As(wrapped, &as) || as.Op != "vkCreateBuffer" {
		t.Fatal("errors.Is / errors.As do not see the code through a wrapper")
	}
	if errors.Is(e, errBoom) || errors.Is(e, &Error{Op: "other", Result: e.Result}) {
		t.Fatal("Is matched something that is not a sentinel of this code")
	}
	if check("x", vk.Success) != nil || check("x", vk.Incomplete) != nil || check("x", vk.Timeout) != nil {
		t.Fatal("a success status was an error")
	}
	// Every named code maps to its registry name and back to a sentinel.
	for _, s := range []*Error{ErrOutOfHostMemory, ErrOutOfDeviceMemory, ErrInitializationFailed, ErrDeviceLost, ErrMemoryMapFailed,
		ErrLayerNotPresent, ErrExtensionNotPresent, ErrFeatureNotPresent, ErrIncompatibleDriver, ErrTooManyObjects,
		ErrFragmentedPool, ErrOutOfPoolMemory, ErrUnknown} {
		err := check("vkX", vk.Result(s.Result))
		if !errors.Is(err, s) || s.Result.String()[:9] != "VK_ERROR_" {
			t.Errorf("%v: %v", s, err)
		}
	}
	for r, n := range map[Result]string{Result(vk.Success): "VK_SUCCESS", Result(vk.NotReady): "VK_NOT_READY", Result(vk.Timeout): "VK_TIMEOUT", Result(vk.Incomplete): "VK_INCOMPLETE"} {
		if r.String() != n {
			t.Errorf("%d = %s", r, r)
		}
	}
}

func TestCStrings(t *testing.T) {
	if got := cString([]byte{'a', 'b', 0, 'c'}); got != "ab" {
		t.Fatal(got)
	}
	if got := cString([]byte("full")); got != "full" {
		t.Fatal(got)
	}
	if b := cstr("x"); len(b) != 2 || b[1] != 0 {
		t.Fatal(b)
	}
}
