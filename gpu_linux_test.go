//go:build linux && !android && (amd64 || arm64)

package vulkan

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-gfx/wgsl"
	"github.com/go-gfx/wgsl/spirv"
)

// The tests below drive a real Vulkan implementation: lavapipe in CI, RADV on
// fractal. Where there is none they skip, unless VULKAN_REQUIRE=1 says the
// machine must have one (the CI lane that installs lavapipe sets it).
//
//	VULKAN_REQUIRE=1     a missing loader or device is a failure, not a skip
//	VULKAN_VALIDATION=1  enable the Khronos validation layer and fail on ANY message
//	VULKAN_VENDOR=0x1002 only a device of this PCI vendor

// fillSrc is the kernel of go-gfx/wgsl's spirv tests: one invocation per pixel,
// a guard, a loop with break and continue, and a call of another function.
const fillSrc = `
struct Params { w: u32, h: u32, color: u32, rounds: u32 }

@group(0) @binding(0) var<storage, read_write> px: array<u32>;
@group(0) @binding(1) var<uniform> p: Params;

fn mixbits(c: u32, k: u32) -> u32 {
  return ((c << 1u) | (c >> 31u)) ^ (k * 2654435761u);
}

@compute @workgroup_size(16, 8)
fn fill(@builtin(global_invocation_id) gid: vec3<u32>) {
  if (gid.x >= p.w || gid.y >= p.h) { return; }
  var c = p.color;
  for (var k = 0u; k < p.rounds; k++) {
    if (k == 3u) { continue; }
    if (k > 40u) { break; }
    c = mixbits(c, k);
  }
  px[gid.y * p.w + gid.x] = c;
}
`

// fillExpected is the kernel in Go, the oracle of the bytes.
func fillExpected(color, rounds uint32) uint32 {
	c := color
	for k := uint32(0); k < rounds; k++ {
		if k == 3 {
			continue
		}
		if k > 40 {
			break
		}
		c = (c<<1 | c>>31) ^ (k * 2654435761)
	}
	return c
}

func fillKernel(t *testing.T) []uint32 {
	t.Helper()
	m, err := wgsl.Parse(fillSrc)
	if err != nil {
		t.Fatal(err)
	}
	info, errs := wgsl.Check(m)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	words, err := spirv.Module(m, info, spirv.Options{}, "fill")
	if err != nil {
		t.Fatal(err)
	}
	return words
}

// capture redirects stdout and stderr (fds 1 and 2) to a file for the length
// of the test and returns a function that restores them and gives the text.
// The validation layer writes its messages to stdout, with no callback.
func capture(t *testing.T) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	o1, err1 := syscall.Dup(1)
	o2, err2 := syscall.Dup(2)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	_ = syscall.Dup3(int(f.Fd()), 1, 0)
	_ = syscall.Dup3(int(f.Fd()), 2, 0)
	done := false
	stop := func() string {
		if done {
			return ""
		}
		done = true
		_ = syscall.Dup3(o1, 1, 0)
		_ = syscall.Dup3(o2, 2, 0)
		syscall.Close(o1)
		syscall.Close(o2)
		b, _ := os.ReadFile(f.Name())
		f.Close()
		return string(b)
	}
	t.Cleanup(func() { stop() })
	return stop
}

type rig struct {
	in  *Instance
	dev *Device
}

// openRig opens an instance, picks the device and opens it, or skips.
func openRig(t *testing.T) *rig {
	t.Helper()
	require := os.Getenv("VULKAN_REQUIRE") == "1"
	skip := func(format string, args ...any) {
		t.Helper()
		if require {
			t.Fatalf(format, args...)
		}
		t.Skipf(format, args...)
	}
	in, err := Open(Options{ApplicationName: "go-gfx/vulkan tests", Validation: os.Getenv("VULKAN_VALIDATION") == "1"})
	if err != nil {
		skip("no usable Vulkan loader: %v", err)
	}
	t.Cleanup(func() { in.Close() })
	var f Filter
	if v := os.Getenv("VULKAN_VENDOR"); v != "" {
		id, err := strconv.ParseUint(v, 0, 32)
		if err != nil {
			t.Fatalf("VULKAN_VENDOR=%q: %v", v, err)
		}
		f.VendorID = uint32(id)
	}
	pd, err := in.Pick(f)
	if err != nil {
		skip("no Vulkan device: %v", err)
	}
	i := pd.Info
	t.Logf("device %q type=%v vendor=%#x api=%v driverVersion=%#x (%v) driver=%q info=%q",
		i.Name, i.Type, i.VendorID, i.APIVersion, i.DriverVersion, Version(i.DriverVersion), i.DriverName, i.DriverInfo)
	dev, err := pd.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dev.Close() })
	return &rig{in, dev}
}

// runFill dispatches the fill kernel with the pixels bound to pxBinding of the
// pipeline's set (0 is right; the break check passes a decoy buffer instead).
func runFill(t *testing.T, r *rig, w, h, color, rounds uint32, decoy bool) []byte {
	t.Helper()
	d := r.dev
	sh, err := d.NewShader(fillKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	pipe, err := d.NewComputePipeline(sh, "fill", []Binding{{0, StorageBuffer}, {1, UniformBuffer}})
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	px, err := d.NewBuffer(uint64(w*h*4), UsageStorage)
	if err != nil {
		t.Fatal(err)
	}
	defer px.Close()
	other, err := d.NewBuffer(uint64(w*h*4), UsageStorage)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	params, err := d.NewBuffer(16, UsageUniform)
	if err != nil {
		t.Fatal(err)
	}
	defer params.Close()
	for i := range px.Bytes() {
		px.Bytes()[i], other.Bytes()[i] = 0xAA, 0xAA // a sentinel the kernel must overwrite
	}
	for i, v := range []uint32{w, h, color, rounds} {
		binary.LittleEndian.PutUint32(params.Bytes()[4*i:], v)
	}
	target := px
	if decoy {
		target = other // the wrong binding: the kernel writes the decoy, not px
	}
	if err := pipe.Bind(0, target); err != nil {
		t.Fatal(err)
	}
	if err := pipe.Bind(1, params); err != nil {
		t.Fatal(err)
	}
	cb, err := d.NewCommandBuffer()
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Close()
	if err := cb.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := cb.Dispatch(pipe, (w+15)/16, (h+7)/8, 1); err != nil {
		t.Fatal(err)
	}
	if err := cb.End(); err != nil {
		t.Fatal(err)
	}
	fence, err := d.Submit(cb)
	if err != nil {
		t.Fatal(err)
	}
	defer fence.Close()
	ok, err := fence.Wait(30 * time.Second)
	if err != nil || !ok {
		t.Fatalf("fence: signalled=%v err=%v", ok, err)
	}
	if done, err := fence.Done(); err != nil || !done {
		t.Fatalf("Done after Wait = %v, %v", done, err)
	}
	return bytes.Clone(px.Bytes())
}

func wantFill(w, h, color, rounds uint32) []byte {
	want := make([]byte, w*h*4)
	v := fillExpected(color, rounds)
	for i := range w * h {
		binary.LittleEndian.PutUint32(want[4*i:], v)
	}
	return want
}

const validationMarker = "VUID-"

func TestFillKernelOnDevice(t *testing.T) {
	validate := os.Getenv("VULKAN_VALIDATION") == "1"
	var stop func() string
	if validate {
		stop = capture(t)
	}
	r := openRig(t)
	const w, h, color, rounds = 37, 21, 0xDEADBEEF, 9 // not multiples of the workgroup: the guard matters
	got := runFill(t, r, w, h, color, rounds, false)
	if want := wantFill(w, h, color, rounds); !bytes.Equal(got, want) {
		t.Fatalf("fill kernel wrote the wrong bytes: first got % x, want % x", got[:16], want[:16])
	}
	if validate {
		if out := stop(); strings.Contains(out, "Validation") || strings.Contains(out, validationMarker) {
			t.Fatalf("the validation layer reported:\n%s", out)
		}
	}
}

// TestBreakWrongBinding is the break check: with the pixels' descriptor
// pointed at the wrong buffer the expected bytes must NOT appear in the buffer
// the test reads.
func TestBreakWrongBinding(t *testing.T) {
	r := openRig(t)
	const w, h, color, rounds = 37, 21, 0xDEADBEEF, 9
	got := runFill(t, r, w, h, color, rounds, true)
	if bytes.Equal(got, wantFill(w, h, color, rounds)) {
		t.Fatal("a wrong descriptor binding still produced the expected bytes: the check proves nothing")
	}
	for _, b := range got {
		if b != 0xAA {
			t.Fatalf("the unbound buffer was written (%#x): the kernel reached a buffer it was not bound to", b)
		}
	}
}

// TestValidationLayerSeesBadLayout proves the validation lane can fail: a
// pipeline whose layout declares binding 1 for a shader that uses binding 0
// and 1 draws a validation error, which is read back from the captured output.
func TestValidationLayerSeesBadLayout(t *testing.T) {
	if os.Getenv("VULKAN_VALIDATION") != "1" {
		t.Skip("VULKAN_VALIDATION is not set")
	}
	stop := capture(t)
	r := openRig(t)
	sh, err := r.dev.NewShader(fillKernel(t))
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	// Binding 0 is missing from the layout: the shader uses it.
	if pipe, err := r.dev.NewComputePipeline(sh, "fill", []Binding{{1, UniformBuffer}}); err == nil {
		pipe.Close()
	}
	out := stop()
	if !strings.Contains(out, validationMarker) {
		t.Fatalf("a layout that does not match the shader drew no validation message; output: %q", out)
	}
}

func TestOpenAndEnumerate(t *testing.T) {
	r := openRig(t)
	infos := r.dev.Info()
	if infos.Name == "" || infos.APIVersion < minAPI || infos.MaxStorageBufferRange == 0 || infos.MaxMemoryAllocationSize == 0 {
		t.Fatalf("implausible device info: %+v", infos)
	}
	ds, err := r.in.Devices()
	if err != nil || len(ds) == 0 {
		t.Fatalf("Devices: %v, %v", ds, err)
	}
	// Refusals before allocation, on the real limits.
	if _, err := r.dev.NewBuffer(uint64(infos.MaxStorageBufferRange)+1, UsageStorage); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a storage buffer past maxStorageBufferRange: %v", err)
	}
}

func TestOpenTwiceClosesCleanly(t *testing.T) {
	in, err := Open(Options{})
	if err != nil {
		if os.Getenv("VULKAN_REQUIRE") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
}
