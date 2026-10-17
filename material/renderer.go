package material

import "github.com/Uzer007-admin/gl/capabilities"

// Renderer joins the device, shared two-layer budget/cache, GPU capture and
// separable material passes for one live context generation. Source is an
// already-rendered lower-layer texture owned/accounted by the compositor; no
// CPU readback, full-size per-panel snapshot or implicit frame loop is used.
// One renderer should serve all material panels of a window. The caller owns
// layer ordering, fresh Content/Surface/Style keys and drawing the final texture.
// It must restore its program/texture/vertex/blend state after a changed render.
type Renderer struct {
	device *GLDevice
	pool   *Pool
	blur   *Blur
	closed bool
}

type Result struct {
	Target   Target
	Size     Size
	Rendered bool
}

func NewRenderer(context capabilities.Context, api DrawProcedures, budget uint64) (*Renderer, error) {
	device, err := NewGLDevice(context, api)
	if err != nil {
		return nil, err
	}
	pool, err := NewPool(device, budget)
	if err != nil {
		return nil, err
	}
	blur, err := NewBlur(context, device, api)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Renderer{device: device, pool: pool, blur: blur}, nil
}

// Render selects the best affordable 1/2/4/8 quality at or below downsample,
// then captures the crop and filters only when its full key changes. Zero
// downsample requests full quality. Region uses physical bottom-left pixels;
// ScaleX/Y remain independent conversions from logical blur/corner units.
// Crop movement, texture identity and source size always invalidate the cache.
// The result is borrowed until this slot changes/releases or the renderer closes.
// Errors retain their cause; ErrBudget permits an explicit opaque fallback.
func (r *Renderer) Render(slot int, source Target, region Region, downsample uint8, key Key) (Result, error) {
	if r.closed {
		return Result{}, ErrClosed
	}
	if slot < 0 || slot >= MaxLayers || source.Texture == 0 || source.Width <= 0 || source.Height <= 0 || source.Width > r.device.maximum || source.Height > r.device.maximum || region.X < 0 || region.Y < 0 || region.Width <= 0 || region.Height <= 0 || region.Width > source.Width || region.Height > source.Height || region.X > source.Width-region.Width || region.Y > source.Height-region.Height {
		return Result{}, ErrInvalid
	}
	// Reject the slot's borrowed old output before Update can resize/delete it.
	// Sampling another layer is allowed only when the compositor has established
	// that it belongs to the current lower-layer graph, within the depth limit.
	l := &r.pool.layers[slot]
	if l.bytes != 0 && (source.Texture == l.pair.Front.Texture || source.Texture == l.pair.Back.Texture) {
		return Result{}, ErrInvalid
	}
	key.Capture = CaptureKey{source.Texture, source.Width, source.Height, region}
	size, err := r.pool.adaptiveSize(slot, Size{region.Width, region.Height, downsample}, key)
	if err != nil {
		return Result{}, err
	}
	if l.valid && l.size == size && l.key == key {
		r.pool.stats.CacheHits++
		return Result{Target: l.pair.Front, Size: size}, nil
	}
	// Keep static lookup ahead of callback construction. Capture an immutable
	// render key only on the changed path so the small closure can stay on stack.
	renderKey := key
	pair, rendered, err := r.pool.Update(slot, size, key, func(pair Pair) error {
		if err := r.blur.Capture(pair.Front, source, region, renderKey.Linear); err != nil {
			return err
		}
		return r.blur.Apply(pair, size, renderKey)
	})
	if err != nil {
		return Result{Size: size, Rendered: rendered}, err
	}
	return Result{Target: pair.Front, Size: size, Rendered: rendered}, nil
}

func (r *Renderer) Stats() Stats { return r.pool.Stats() }
func (r *Renderer) Release(slot int) error {
	if r.closed {
		return ErrClosed
	}
	return r.pool.Release(slot)
}

// Close frees resources while the matching context is current, before Destroy.
func (r *Renderer) Close() {
	if r.closed {
		return
	}
	r.pool.Close()
	r.blur.Close()
	r.closed = true
}

// Abandon drops identities after context loss, without wrong-context deletes.
func (r *Renderer) Abandon() {
	if r.closed {
		return
	}
	r.pool.Abandon()
	r.blur.Abandon()
	r.closed = true
}
