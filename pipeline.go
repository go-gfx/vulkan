package vulkan

import (
	"runtime"
	"slices"
	"strings"
	"unsafe"

	"github.com/go-gfx/vulkan/internal/vk"
)

const (
	// spirvMagic is the first word of a SPIR-V module (SPIR-V 2.3, Table 1).
	spirvMagic = 0x07230203
	// maxShaderWords bounds a module: 16 Mi words, 64 MiB. CodeSize is a
	// size_t, so the byte count can never wrap.
	maxShaderWords = 1 << 24
	// maxBindings bounds the descriptors of the one set a pipeline owns.
	maxBindings = 1 << 10
)

// Shader is a VkShaderModule.
type Shader struct {
	d      *Device
	h      vk.ShaderModule
	closed bool
}

// NewShader creates a shader module from SPIR-V words (for instance from
// go-gfx/wgsl's spirv.Module). The words are checked for the magic number and
// a sane length; the driver (and the validation layer) check the rest.
func (d *Device) NewShader(words []uint32) (*Shader, error) {
	if d.closed {
		return nil, ErrClosed
	}
	if len(words) < 5 || len(words) > maxShaderWords || words[0] != spirvMagic {
		return nil, ErrInvalid
	}
	ci := vk.ShaderModuleCreateInfo{
		SType:    vk.StructureTypeShaderModuleCreateInfo,
		CodeSize: uintptr(len(words)) * 4,
		PCode:    unsafe.Pointer(&words[0]),
	}
	s := &Shader{d: d}
	r := d.f.CreateShaderModule(d.h, &ci, nil, &s.h)
	runtime.KeepAlive(words)
	if err := check("vkCreateShaderModule", r); err != nil {
		return nil, err
	}
	return s, nil
}

// Close destroys the module; a pipeline made from it keeps working.
func (s *Shader) Close() {
	if s.closed || s.d.closed {
		return
	}
	s.closed = true
	s.d.f.DestroyShaderModule(s.d.h, s.h, nil)
}

// DescriptorKind is the kind of resource bound at a binding.
type DescriptorKind int32

// The kinds a compute shader here uses.
const (
	StorageBuffer = DescriptorKind(vk.DescriptorTypeStorageBuffer)
	UniformBuffer = DescriptorKind(vk.DescriptorTypeUniformBuffer)
)

func (k DescriptorKind) usage() BufferUsage {
	if k == UniformBuffer {
		return UsageUniform
	}
	return UsageStorage
}

// Binding declares one binding of descriptor set 0.
type Binding struct {
	Binding uint32
	Kind    DescriptorKind
}

func checkBindings(bs []Binding) error {
	if len(bs) == 0 || len(bs) > maxBindings {
		return ErrInvalid
	}
	seen := make(map[uint32]bool, len(bs))
	for _, b := range bs {
		if seen[b.Binding] || (b.Kind != StorageBuffer && b.Kind != UniformBuffer) {
			return ErrInvalid
		}
		seen[b.Binding] = true
	}
	return nil
}

// Pipeline is a compute pipeline with its layout, and one descriptor set
// (set 0) allocated from a pool of its own.
type Pipeline struct {
	d         *Device
	bindings  []Binding
	setLayout vk.DescriptorSetLayout
	layout    vk.PipelineLayout
	pipeline  vk.Pipeline
	pool      vk.DescriptorPool
	set       vk.DescriptorSet
	closed    bool
}

// NewComputePipeline builds the descriptor set layout for bindings, the
// pipeline layout, the pipeline for entry point entry of sh, and a descriptor
// set. On an error nothing is left alive.
func (d *Device) NewComputePipeline(sh *Shader, entry string, bindings []Binding) (*Pipeline, error) {
	if d.closed || sh.closed {
		return nil, ErrClosed
	}
	if sh.d != d || entry == "" || strings.IndexByte(entry, 0) >= 0 {
		return nil, ErrInvalid
	}
	if err := checkBindings(bindings); err != nil {
		return nil, err
	}
	p := &Pipeline{d: d, bindings: slices.Clone(bindings)}
	var undo []func()
	fail := func(err error) (*Pipeline, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}

	lb := make([]vk.DescriptorSetLayoutBinding, len(bindings))
	var nStorage, nUniform uint32
	for i, b := range bindings {
		lb[i] = vk.DescriptorSetLayoutBinding{
			Binding:         b.Binding,
			DescriptorType:  vk.DescriptorType(b.Kind),
			DescriptorCount: 1,
			StageFlags:      vk.ShaderStageFlags(vk.ShaderStageComputeBit),
		}
		if b.Kind == UniformBuffer {
			nUniform++
		} else {
			nStorage++
		}
	}
	slci := vk.DescriptorSetLayoutCreateInfo{
		SType:        vk.StructureTypeDescriptorSetLayoutCreateInfo,
		BindingCount: uint32(len(lb)),
		PBindings:    unsafe.Pointer(&lb[0]),
	}
	r := d.f.CreateDescriptorSetLayout(d.h, &slci, nil, &p.setLayout)
	runtime.KeepAlive(lb)
	if err := check("vkCreateDescriptorSetLayout", r); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { d.f.DestroyDescriptorSetLayout(d.h, p.setLayout, nil) })

	plci := vk.PipelineLayoutCreateInfo{
		SType:          vk.StructureTypePipelineLayoutCreateInfo,
		SetLayoutCount: 1,
		PSetLayouts:    unsafe.Pointer(&p.setLayout),
	}
	r = d.f.CreatePipelineLayout(d.h, &plci, nil, &p.layout)
	runtime.KeepAlive(&p.setLayout)
	if err := check("vkCreatePipelineLayout", r); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { d.f.DestroyPipelineLayout(d.h, p.layout, nil) })

	name := cstr(entry)
	pci := vk.ComputePipelineCreateInfo{
		SType: vk.StructureTypeComputePipelineCreateInfo,
		Stage: vk.PipelineShaderStageCreateInfo{
			SType:  vk.StructureTypePipelineShaderStageCreateInfo,
			Stage:  vk.ShaderStageComputeBit,
			Module: sh.h,
			PName:  unsafe.Pointer(&name[0]),
		},
		Layout:             p.layout,
		BasePipelineHandle: 0,
		BasePipelineIndex:  -1,
	}
	r = d.f.CreateComputePipelines(d.h, 0, 1, &pci, nil, &p.pipeline)
	runtime.KeepAlive(name)
	if err := check("vkCreateComputePipelines", r); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { d.f.DestroyPipeline(d.h, p.pipeline, nil) })

	var sizes []vk.DescriptorPoolSize
	if nStorage > 0 {
		sizes = append(sizes, vk.DescriptorPoolSize{Type: vk.DescriptorTypeStorageBuffer, DescriptorCount: nStorage})
	}
	if nUniform > 0 {
		sizes = append(sizes, vk.DescriptorPoolSize{Type: vk.DescriptorTypeUniformBuffer, DescriptorCount: nUniform})
	}
	dpci := vk.DescriptorPoolCreateInfo{
		SType:         vk.StructureTypeDescriptorPoolCreateInfo,
		MaxSets:       1,
		PoolSizeCount: uint32(len(sizes)),
		PPoolSizes:    unsafe.Pointer(&sizes[0]),
	}
	r = d.f.CreateDescriptorPool(d.h, &dpci, nil, &p.pool)
	runtime.KeepAlive(sizes)
	if err := check("vkCreateDescriptorPool", r); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { d.f.DestroyDescriptorPool(d.h, p.pool, nil) })

	dsai := vk.DescriptorSetAllocateInfo{
		SType:              vk.StructureTypeDescriptorSetAllocateInfo,
		DescriptorPool:     p.pool,
		DescriptorSetCount: 1,
		PSetLayouts:        unsafe.Pointer(&p.setLayout),
	}
	r = d.f.AllocateDescriptorSets(d.h, &dsai, &p.set)
	runtime.KeepAlive(&p.setLayout)
	if err := check("vkAllocateDescriptorSets", r); err != nil {
		return fail(err)
	}
	return p, nil
}

// Bind points a binding of the pipeline's descriptor set at a whole buffer.
// The binding must be declared by the pipeline, and the buffer must have the
// usage that kind of binding needs. Bind before recording the dispatch that
// uses it, and not while a submission that uses the set is in flight.
func (p *Pipeline) Bind(binding uint32, b *Buffer) error {
	if p.closed || p.d.closed || b.closed {
		return ErrClosed
	}
	if b.d != p.d {
		return ErrInvalid
	}
	i := slices.IndexFunc(p.bindings, func(x Binding) bool { return x.Binding == binding })
	if i < 0 || b.usage&p.bindings[i].Kind.usage() == 0 {
		return ErrInvalid
	}
	info := vk.DescriptorBufferInfo{Buffer: b.buf, Offset: 0, Range: vk.DeviceSize(vk.WholeSize)}
	w := vk.WriteDescriptorSet{
		SType:           vk.StructureTypeWriteDescriptorSet,
		DstSet:          p.set,
		DstBinding:      binding,
		DescriptorCount: 1,
		DescriptorType:  vk.DescriptorType(p.bindings[i].Kind),
		PBufferInfo:     unsafe.Pointer(&info),
	}
	p.d.f.UpdateDescriptorSets(p.d.h, 1, &w, 0, nil)
	runtime.KeepAlive(&info)
	return nil
}

// Close destroys the pipeline, its layouts and its pool (which frees the set).
func (p *Pipeline) Close() {
	if p.closed || p.d.closed {
		return
	}
	p.closed = true
	f, h := p.d.f, p.d.h
	f.DestroyDescriptorPool(h, p.pool, nil)
	f.DestroyPipeline(h, p.pipeline, nil)
	f.DestroyPipelineLayout(h, p.layout, nil)
	f.DestroyDescriptorSetLayout(h, p.setLayout, nil)
}
