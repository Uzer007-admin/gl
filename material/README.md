# Per-context GPU material renderer

`Renderer` combines crop/downsampling, two-target blur and a bounded cache for
one context generation. Use one instance for a window's material panels.
GL 2.1 compatibility with core/ARB/EXT texture FBOs and GLES 2 are supported by
the generated tables. A WebGL object adapter can implement the interfaces; this
package's WASM compilation alone does not verify browser rendering.

```go
// The matching context is current on its owner thread. procs is an immutable
// material/GLES Functions table; context is the queried capability snapshot.
renderer, err := material.NewRenderer(context, procs, material.DefaultBudget)
// Handle err, then close before destroying this context.
defer renderer.Close()

// lowerLayers is a compositor-owned RGBA8 texture. region is a physical crop
// with GL bottom-left origin. key contains current source/style/surface
// generations and physical pixels per logical unit in ScaleX and ScaleY.
result, err := renderer.Render(0, lowerLayers, region, 1, key)
// On success composite result.Target using premultiplied alpha. Rebind your
// program, vertex inputs, textures, blend/scissor/cull/color mask first.
// result.Size.Downsample reports selected quality; result.Rendered is false
// when the cached target was reused without GL calls or Go allocations.
```

`Render` tries 1/2/4/8 downsampling, starting at the requested factor, before
allocation. It includes old+new resize texels and both layers in the budget.
At most two layers own two RGBA8 targets each. No layer is evicted implicitly;
`ErrBudget` permits the application to choose an explicit opaque fallback.
`Release(slot)` frees the layer; `Close` is idempotent. After context loss use
`Abandon`, then create new tables/device/renderer for the replacement context.

Crop coordinates, source texture identity/dimensions and the supplied generations
participate in cache invalidation. Updating text drawn above a panel need not
change its lower-layer `Content`. Never use a slot's old material output as that
slot's new lower-layer source; this is rejected before resize can delete it.
Compositor ordering and limiting material nesting remain the caller's job.

`Blur.Capture` can also be used with `Pool.Update`/`UpdateAdaptive`. It performs
one GPU pass with four bilinear samples, clamps to the crop, and allocates no
full-size per-panel source texture. Factor 1 preserves texel sampling; factor 2
is a 2x2 box. Factors 4/8 approximate the footprint with four samples and can
lose fine detail; they are budget/quality fallback modes, not exact area filters.
For `Linear=true`, each source texel is decoded from straight sRGB before
interpolation/filtering, then re-encoded to the premultiplied RGBA8 capture.

Framebuffer/viewport state is restored on errors and panic. With current Glow
tables these snapshots use one cgo transition and C stack storage. Other adapters
retain the individual query path. Source textures, shaders, the window framebuffer
and driver metadata are outside the requested pool texel budget and require
separate compositor accounting. All passes require premultiplied input, a matching
current context and the documented draw-state ownership; no CPU readback or
implicit frame scheduling occurs.
