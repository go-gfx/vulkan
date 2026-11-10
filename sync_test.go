package vulkan

import (
	"errors"
	"slices"
	"testing"

	"github.com/go-gfx/vulkan/internal/vk"
)

func TestFeaturesAreReportedAndRobustAccessEnabled(t *testing.T) {
	f := newFake()
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	ds := must(in.Devices())
	cpu, gpu := ds[0].Info, ds[1].Info
	if cpu.RobustBufferAccess || cpu.RoundingModeRTEFloat32 {
		t.Fatalf("a device offering nothing reports %+v", cpu)
	}
	if !gpu.RobustBufferAccess || !gpu.RoundingModeRTEFloat32 {
		t.Fatalf("a device offering both reports %+v", gpu)
	}
	// Offered: enabled, and nothing else.
	d := must(ds[1].Open())
	if !d.RobustBufferAccess() || f.enabled == nil || f.enabled.RobustBufferAccess != 1 {
		t.Fatalf("robustBufferAccess offered but not enabled: %v %+v", d.RobustBufferAccess(), f.enabled)
	}
	if e := *f.enabled; e != (vk.PhysicalDeviceFeatures{RobustBufferAccess: 1}) {
		t.Fatalf("a feature beyond robustBufferAccess was enabled: %+v", e)
	}
	d.Close()
	// Not offered: no feature structure at all.
	d = must(ds[0].Open())
	if d.RobustBufferAccess() || f.enabled != nil {
		t.Fatalf("robustBufferAccess not offered yet enabled: %v %+v", d.RobustBufferAccess(), f.enabled)
	}
	d.Close()
}

func TestBarrierCopyAndDeviceBuffers(t *testing.T) {
	f := newFake()
	f.reqSize = 64
	in := must(openWith(f.open, Options{}))
	defer in.Close()
	pd := must(in.Pick(Filter{}))
	d := must(pd.Open())
	defer d.Close()
	cb := must(d.NewCommandBuffer())
	defer cb.Close()

	if err := cb.Barrier(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Barrier outside Begin: %v", err)
	}
	dev := must(d.NewDeviceBuffer(32, UsageStorage|UsageTransferDst|UsageTransferSrc))
	host := must(d.NewBuffer(32, UsageTransferSrc|UsageTransferDst))
	noTransfer := must(d.NewBuffer(32, UsageStorage))
	if dev.Bytes() != nil || dev.Size() != 32 || host.Size() != 32 {
		t.Fatalf("device buffer: mapped %v, size %d", dev.Bytes() != nil, dev.Size())
	}
	if err := cb.Copy(dev, host, 8); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Copy outside Begin: %v", err)
	}
	noerr(t, cb.Begin())
	noerr(t, cb.Barrier())
	other := must(pd.Open())
	defer other.Close()
	foreign := must(other.NewBuffer(32, UsageTransferSrc|UsageTransferDst))
	for name, c := range map[string]struct {
		dst, src *Buffer
		n        uint64
	}{
		"zero size":          {dev, host, 0},
		"past dst":           {host, dev, 33},
		"past src":           {dev, host, 33},
		"src not a source":   {dev, noTransfer, 8},
		"dst not a dest":     {noTransfer, host, 8},
		"buffer of a device": {foreign, host, 8},
		"src of a device":    {dev, foreign, 8},
	} {
		if err := cb.Copy(c.dst, c.src, c.n); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	noerr(t, cb.Copy(dev, host, 16))
	foreign.Close()
	if err := cb.Copy(dev, foreign, 8); !errors.Is(err, ErrClosed) {
		t.Fatalf("Copy from a closed buffer: %v", err)
	}
	noerr(t, cb.End())

	// What was recorded: the barrier orders compute and transfer writes before
	// compute and transfer reads and writes; the copy ends with a host barrier.
	const stages = uint32(vk.PipelineStageComputeShaderBit | vk.PipelineStageTransferBit)
	want := []fakeBarrier{
		{stages, stages,
			vk.AccessFlags(vk.AccessShaderWriteBit | vk.AccessTransferWriteBit),
			vk.AccessFlags(vk.AccessShaderReadBit | vk.AccessShaderWriteBit | vk.AccessTransferReadBit | vk.AccessTransferWriteBit)},
		{uint32(vk.PipelineStageTransferBit), uint32(vk.PipelineStageHostBit),
			vk.AccessFlags(vk.AccessTransferWriteBit), vk.AccessFlags(vk.AccessHostReadBit)},
	}
	if !slices.Equal(f.barriers, want) {
		t.Fatalf("barriers\n got %+v\nwant %+v", f.barriers, want)
	}
	if len(f.copies) != 1 || f.copies[0].Size != 16 {
		t.Fatalf("copies %+v", f.copies)
	}

	// A device buffer is never mapped, so closing it must not unmap.
	dev.Close()
	dev.Close()
	host.Close()
	noTransfer.Close()
	if f.live["buffer"] != 0 || f.live["memory"] != 0 {
		t.Fatal(f.leaks())
	}
	cb.Close()
	for _, err := range []error{cb.Barrier(), cb.Copy(dev, host, 1)} {
		if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
	// No device-local memory type allowed: refused, nothing leaked.
	f.typeBits = 0b10
	if _, err := d.NewDeviceBuffer(10, UsageStorage); !errors.Is(err, ErrNoMemoryType) || f.live["buffer"] != 0 || f.live["memory"] != 0 {
		t.Fatalf("%v %v", err, f.leaks())
	}
}

func TestMemoryTypeLocal(t *testing.T) {
	var m vk.PhysicalDeviceMemoryProperties
	dl := vk.MemoryPropertyFlags(vk.MemoryPropertyDeviceLocalBit)
	hv := vk.MemoryPropertyFlags(vk.MemoryPropertyHostVisibleBit)
	m.MemoryTypeCount = 4
	m.MemoryTypes[0].PropertyFlags = hv      // not device-local
	m.MemoryTypes[1].PropertyFlags = dl | hv // BAR memory: second choice
	m.MemoryTypes[2].PropertyFlags = dl      // VRAM: the choice
	m.MemoryTypes[3].PropertyFlags = dl | hv
	if i, ok := memoryTypeLocal(&m, 0b1111); !ok || i != 2 {
		t.Fatal(i, ok)
	}
	if i, ok := memoryTypeLocal(&m, 0b1011); !ok || i != 1 {
		t.Fatalf("with VRAM disallowed: %d %v", i, ok)
	}
	if _, ok := memoryTypeLocal(&m, 0b0001); ok {
		t.Fatal("a type that is not device-local was chosen")
	}
	m.MemoryTypeCount = 1000 // a lying driver cannot index past the array
	if _, ok := memoryTypeLocal(&m, 0); ok {
		t.Fatal("no bits allowed, yet a type was chosen")
	}
}
