# go-gfx/vulkan

A pure-Go (no cgo) binding to the part of Vulkan a compute workload needs. It
calls the system loader `libvulkan.so.1` through
[purego](https://github.com/ebitengine/purego), so a binary built with
`CGO_ENABLED=0` can be cross-compiled on one machine and copied to another that
has a Vulkan driver.

```go
in, err := vulkan.Open(vulkan.Options{Validation: false})
if err != nil { /* errors.Is(err, vulkan.ErrUnsupported): no loader */ }
defer in.Close()

pd, err := in.Pick(vulkan.Filter{}) // discrete GPU first, then integrated, ..., CPU
dev, err := pd.Open()               // one device, one compute queue
defer dev.Close()

px, _ := dev.NewBuffer(1<<20, vulkan.UsageStorage) // host-visible, coherent, mapped
sh, _ := dev.NewShader(words)                      // SPIR-V words, e.g. go-gfx/wgsl's spirv.Module
pipe, _ := dev.NewComputePipeline(sh, "main", []vulkan.Binding{{0, vulkan.StorageBuffer}})
pipe.Bind(0, px)

cb, _ := dev.NewCommandBuffer()
cb.Begin(); cb.Dispatch(pipe, 64, 1, 1); cb.End()
fence, _ := dev.Submit(cb)
ok, err := fence.Wait(time.Second)   // px.Bytes() now holds the shader's writes
```

## Surface

| Piece | Entry points |
|---|---|
| Loader | `Open` (dlopen `libvulkan.so.1`, `vkGetInstanceProcAddr`; device functions through `vkGetDeviceProcAddr`) |
| Instance | `Options{Validation}` enables `VK_LAYER_KHRONOS_validation` (`ErrLayerNotPresent` if absent), `Instance.Close` |
| Physical device | `Instance.Devices`, `Instance.Pick(Filter)`, `Pick(infos, Filter)`; `DeviceInfo` carries name, type, vendor, API and driver version, driver name and the limits the binding checks |
| Device and queue | `PhysicalDevice.Open` (one compute queue), `Device.Close` |
| Buffers | `Device.NewBuffer(size, usage)`, `Buffer.Bytes`, `Buffer.Close` |
| Shader | `Device.NewShader(words)` |
| Pipeline | `Device.NewComputePipeline(shader, entry, bindings)`, `Pipeline.Bind`; descriptor set layout, pool and set are owned by the pipeline |
| Commands | `Device.NewCommandBuffer`, `Begin`, `Dispatch`, `End`, `Close` |
| Fences | `Device.Submit`, `Fence.Wait`, `Fence.Done`, `Fence.Close` |
| Errors | a negative `VkResult` is `*Error{Op, Result}`; `errors.Is(err, vulkan.ErrDeviceLost)` and the other sentinels |

## Safety

- A buffer size is checked **before** allocation against `maxStorageBufferRange`
  or `maxUniformBufferRange` for its usage, against `maxMemoryAllocationSize`
  (twice: for the request and for the driver's `memoryRequirements.size`), and
  against the largest Go slice, all in `uint64`, so 32-bit wrap-around cannot
  make a huge request look small. A refusal is `ErrTooLarge`.
- A shader is checked for the SPIR-V magic number and a bound of 2^24 words.
- Every handle made on the way is destroyed on an error path: a test fails each
  fallible driver call in turn, on a fake driver, and requires zero live handles
  afterwards.
- The layout of every struct passed to Vulkan is generated from the Khronos
  registry (below) and pinned by a generated test.

## Struct layouts: `vkgen`

`internal/vk/types_gen.go` is generated from the Khronos registry
(`registry/vk.xml.gz`, Vulkan-Headers `vulkan-sdk-1.4.363.0`, `Apache-2.0 OR MIT`,
kept gzipped: 0.3 MB instead of 3.3 MB; `gunzip -k` gives the original) by `cmd/vkgen`, with sizes and offsets computed by the C rules for a 64-bit
target. It also writes `layout_gen_test.go`, which pins `unsafe.Sizeof`,
`unsafe.Alignof` and `unsafe.Offsetof` of every Go struct against that table.

```
go generate ./internal/vk                       # rewrites both files
go run ./cmd/vkgen -r registry/vk.xml.gz -o internal/vk --check   # fails on drift
```

The registry reader is fuzzed (`FuzzParse`, corpus committed in
`internal/vkgen/testdata/fuzz`).

## Testing

The pure-Go parts run against a fake driver on every platform and are held to
100 % coverage. The real driver is exercised where one exists:

- CI, Ubuntu with lavapipe and the Khronos validation layer, with the test
  binary built `CGO_ENABLED=0`: a compute dispatch of a fill kernel (emitted by
  [go-gfx/wgsl](https://github.com/go-gfx/wgsl)'s `spirv` package) must write
  the expected bytes, and the run must produce **zero** validation messages
  (the test captures stdout and stderr). A second test proves that lane can
  fail: a pipeline layout that does not match the shader draws a `VUID-` message.
- A break check: binding the pixels' descriptor to the wrong buffer must not
  produce the expected bytes.
- `VULKAN_REQUIRE=1` turns "no Vulkan here" from a skip into a failure;
  `VULKAN_VALIDATION=1` enables the layer; `VULKAN_VENDOR=0x1002` restricts the
  pick to a vendor.

lavapipe, `spirv-val` and the validation layer are oracles in CI only: nothing
at build time or run time depends on them.

## Out of scope

- Window-system integration: no `VK_KHR_surface`, no swapchain, no WSI.
- Graphics pipelines, render passes, images, samplers.
- Callbacks (`VK_EXT_debug_utils`): the validation layer's output is read from
  stdout and stderr.
- 32-bit targets: non-dispatchable handles are `uint64_t` passed in two words
  there, and purego's 32-bit Linux targets are Tier 2. On other platforms
  `Open` returns `ErrUnsupported`. Supported: linux/amd64 and linux/arm64.
- Windows (`vulkan-1.dll`) and macOS (MoltenVK).

`Close` keeps the loader mapped for the life of the process: drivers may keep
threads alive after their instance is destroyed.

## Threading

A queue is externally synchronised: a `Device` and everything made from it
belong to one goroutine at a time.
