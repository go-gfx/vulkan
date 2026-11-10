//go:build linux && (amd64 || arm64)

package vulkan

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-gfx/wgsl"
	"github.com/go-gfx/wgsl/spirv"
)

// Synchronization, optional features and device-local memory, on a real
// implementation (lavapipe in CI).
//
//	VULKAN_SYNC_VALIDATION=1  the process runs with the layer's synchronization
//	                          validation on: VK_LAYER_ENABLES must hold
//	                          VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT
//	                          (the layer reads it when the instance is created; a Go
//	                          test cannot set it for itself with CGO_ENABLED=0).

const syncValidationEnable = "VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT"

// needSync skips unless the lane runs with synchronization validation, and
// fails when it says it does but the layer was not told to.
func needSync(t *testing.T) {
	t.Helper()
	if os.Getenv("VULKAN_SYNC_VALIDATION") != "1" {
		t.Skip("VULKAN_SYNC_VALIDATION is not set")
	}
	if os.Getenv("VULKAN_VALIDATION") != "1" || !strings.Contains(os.Getenv("VK_LAYER_ENABLES"), syncValidationEnable) {
		t.Fatalf("VULKAN_SYNC_VALIDATION=1 needs VULKAN_VALIDATION=1 and VK_LAYER_ENABLES=%s", syncValidationEnable)
	}
}

func fillModule(t *testing.T, o spirv.Options) []uint32 {
	t.Helper()
	m, err := wgsl.Parse(fillSrc)
	if err != nil {
		t.Fatal(err)
	}
	info, errs := wgsl.Check(m)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	words, err := spirv.Module(m, info, o, "fill")
	if err != nil {
		t.Fatal(err)
	}
	return words
}

// twoSubmissions records two copies into the same device buffer, each in its
// own command buffer, and submits both before waiting for either. With barrier
// the second begins with Barrier. It returns what the validation layer said.
//
// The hazard is shown with copies, not with the fill kernel: the layer's
// synchronization validation (1.4.309, on lavapipe) reports hazards between
// transfer commands but none between compute dispatches, whose descriptor
// accesses it did not track for this kernel (an unordered pair of dispatches
// that write one buffer, and a dispatch followed by a copy of what it wrote,
// both drew nothing). The barrier and its masks cover both stages.
func twoSubmissions(t *testing.T, barrier bool) string {
	t.Helper()
	stop := capture(t)
	r := openRig(t)
	d := r.dev
	const n = 4096
	up := must(d.NewBuffer(n, UsageTransferSrc))
	defer up.Close()
	dev := must(d.NewDeviceBuffer(n, UsageTransferDst))
	defer dev.Close()
	var fences []*Fence
	for i := range 2 {
		cb := must(d.NewCommandBuffer())
		defer cb.Close()
		noerr(t, cb.Begin())
		if i == 1 && barrier {
			noerr(t, cb.Barrier())
		}
		noerr(t, cb.Copy(dev, up, n))
		noerr(t, cb.End())
		fences = append(fences, must(d.Submit(cb)))
	}
	for _, f := range fences {
		ok, err := f.Wait(30 * time.Second)
		if err != nil || !ok {
			t.Fatalf("fence: %v %v", ok, err)
		}
		f.Close()
	}
	return stop()
}

// TestBarrierOrdersSubmissions: two submissions that write the same buffer,
// the second after a Barrier, draw no message from synchronization validation.
func TestBarrierOrdersSubmissions(t *testing.T) {
	needSync(t)
	if out := twoSubmissions(t, true); strings.Contains(out, "SYNC-HAZARD") || strings.Contains(out, validationMarker) || strings.Contains(out, "Validation") {
		t.Fatalf("a barrier between the submissions, yet the layer reported:\n%s", out)
	}
}

// TestBreakMissingBarrier is the break check: the same two submissions with
// the Barrier left out MUST draw a synchronization hazard, or the lane cannot
// see a missing barrier.
func TestBreakMissingBarrier(t *testing.T) {
	needSync(t)
	out := twoSubmissions(t, false)
	if !strings.Contains(out, "SYNC-HAZARD") {
		t.Fatalf("two unordered submissions writing one buffer drew no SYNC-HAZARD; the layer said: %q", out)
	}
}

// TestDeviceBufferStagingRoundTrip fills a device buffer from a host buffer,
// runs the fill kernel into it, and reads it back through a second host buffer:
// the staging path, barriers included.
func TestDeviceBufferStagingRoundTrip(t *testing.T) {
	validate := os.Getenv("VULKAN_VALIDATION") == "1"
	var stop func() string
	if validate {
		stop = capture(t)
	}
	r := openRig(t)
	d := r.dev
	const w, h, color, rounds = 37, 21, 0xDEADBEEF, 9
	n := uint64(w * h * 4)
	sh := must(d.NewShader(fillModule(t, spirv.Options{})))
	defer sh.Close()
	pipe := must(d.NewComputePipeline(sh, "fill", []Binding{{0, StorageBuffer}, {1, UniformBuffer}}))
	defer pipe.Close()
	dev := must(d.NewDeviceBuffer(n, UsageStorage|UsageTransferSrc|UsageTransferDst))
	defer dev.Close()
	if dev.Bytes() != nil || dev.Size() != n {
		t.Fatalf("a device buffer is mapped or has the wrong size: %v %d", dev.Bytes() != nil, dev.Size())
	}
	up := must(d.NewBuffer(n, UsageTransferSrc))
	defer up.Close()
	down := must(d.NewBuffer(n, UsageTransferDst))
	defer down.Close()
	params := must(d.NewBuffer(16, UsageUniform))
	defer params.Close()
	for i := range up.Bytes() {
		up.Bytes()[i], down.Bytes()[i] = 0xAA, 0x55 // the kernel must overwrite the 0xAA it is given
	}
	for i, v := range []uint32{w, h, color, rounds} {
		binary.LittleEndian.PutUint32(params.Bytes()[4*i:], v)
	}
	noerr(t, pipe.Bind(0, dev))
	noerr(t, pipe.Bind(1, params))
	cb := must(d.NewCommandBuffer())
	defer cb.Close()
	noerr(t, cb.Begin())
	noerr(t, cb.Copy(dev, up, n)) // upload
	noerr(t, cb.Barrier())
	noerr(t, cb.Dispatch(pipe, (w+15)/16, (h+7)/8, 1))
	noerr(t, cb.Barrier())
	noerr(t, cb.Copy(down, dev, n)) // read back
	noerr(t, cb.End())
	fence := must(d.Submit(cb))
	defer fence.Close()
	if ok, err := fence.Wait(30 * time.Second); err != nil || !ok {
		t.Fatalf("fence: %v %v", ok, err)
	}
	if want := wantFill(w, h, color, rounds); !bytes.Equal(down.Bytes(), want) {
		t.Fatalf("the staged round trip wrote the wrong bytes: got % x, want % x", down.Bytes()[:16], want[:16])
	}
	if validate {
		if out := stop(); strings.Contains(out, "Validation") || strings.Contains(out, validationMarker) {
			t.Fatalf("the validation layer reported:\n%s", out)
		}
	}
}

// TestFeaturesOnDevice: the optional features are reported, robustBufferAccess
// is enabled where offered, and a module declaring RoundingModeRTE is accepted
// (and computes the same bytes) where the device reports it.
func TestFeaturesOnDevice(t *testing.T) {
	validate := os.Getenv("VULKAN_VALIDATION") == "1"
	var stop func() string
	if validate {
		stop = capture(t)
	}
	r := openRig(t)
	info := r.dev.Info()
	t.Logf("robustBufferAccess offered %v, enabled %v; shaderRoundingModeRTEFloat32 %v",
		info.RobustBufferAccess, r.dev.RobustBufferAccess(), info.RoundingModeRTEFloat32)
	if r.dev.RobustBufferAccess() != info.RobustBufferAccess {
		t.Fatal("robustBufferAccess is not enabled exactly when offered")
	}
	if !info.RoundingModeRTEFloat32 {
		t.Skip("no RoundingModeRTE on this device")
	}
	d := r.dev
	const w, h, color, rounds = 37, 21, 0xDEADBEEF, 9
	sh := must(d.NewShader(fillModule(t, spirv.Options{RoundingModeRTE: true})))
	defer sh.Close()
	pipe := must(d.NewComputePipeline(sh, "fill", []Binding{{0, StorageBuffer}, {1, UniformBuffer}}))
	defer pipe.Close()
	px := must(d.NewBuffer(w*h*4, UsageStorage))
	defer px.Close()
	params := must(d.NewBuffer(16, UsageUniform))
	defer params.Close()
	for i, v := range []uint32{w, h, color, rounds} {
		binary.LittleEndian.PutUint32(params.Bytes()[4*i:], v)
	}
	noerr(t, pipe.Bind(0, px))
	noerr(t, pipe.Bind(1, params))
	cb := must(d.NewCommandBuffer())
	defer cb.Close()
	noerr(t, cb.Begin())
	noerr(t, cb.Dispatch(pipe, (w+15)/16, (h+7)/8, 1))
	noerr(t, cb.End())
	fence := must(d.Submit(cb))
	defer fence.Close()
	if ok, err := fence.Wait(30 * time.Second); err != nil || !ok {
		t.Fatalf("fence: %v %v", ok, err)
	}
	if !bytes.Equal(px.Bytes(), wantFill(w, h, color, rounds)) {
		t.Fatal("the RoundingModeRTE module wrote the wrong bytes")
	}
	if validate {
		if out := stop(); strings.Contains(out, "Validation") || strings.Contains(out, validationMarker) {
			t.Fatalf("the validation layer reported:\n%s", out)
		}
	}
}
