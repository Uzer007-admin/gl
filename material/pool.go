// Package material manages per-context RGBA8 targets and cached material passes.
// It does not read pixels, schedule frames, or sample the desktop compositor.
package material

import (
	"errors"
	"fmt"
	"math"
)

const DefaultBudget uint64 = 64 << 20
const MaxLayers = 2

var (
	ErrBudget  = errors.New("material: target memory budget exceeded")
	ErrClosed  = errors.New("material: pool closed")
	ErrInvalid = errors.New("material: invalid request")
)

type Handle struct{ Texture, Framebuffer uint32 }

// Device creates complete single-sample RGBA8 texture framebuffers. CreateTarget
// must release partial allocations on failure; DeleteTarget must accept a valid
// handle previously returned by this device. All calls run on the context owner
// thread, with that context current. No pool method is safe for concurrent use.
type Device interface {
	MaxTextureSize() int
	CreateTarget(width, height int) (Handle, error)
	DeleteTarget(Handle)
}

type Target struct {
	Handle
	Width, Height int
}

// Pair is for source -> horizontal -> vertical ping-pong passes. A pass must
// never sample the target it is writing. Handles are borrowed until this layer
// is resized/released or the pool closes; callers must not delete them.
type Pair struct{ Front, Back Target }

// Size is in physical pixels. Downsample defaults to 1 and accepts 1, 2, 4, 8;
// target dimensions round up so the final logical edge is never clipped.
type Size struct {
	Width, Height int
	Downsample    uint8
}

// Key describes the already-rendered lower canvas layers and material policy.
// Surface changes, lower-layer content, theme/style, both DPI axes and shader
// parameters must participate in the key. Tint is straight RGBA; a renderer
// converts it to its chosen alpha/color-space contract. Linear describes the
// shader's working color space; it does not change the RGBA8 target format.
type Key struct {
	Surface, Content, Style uint64
	ScaleX, ScaleY          float32
	Radius, Corners, Noise  float32
	Tint                    [4]float32
	Linear                  bool
	// Capture identifies a sampled texture region. Renderer fills this field;
	// direct pool users set it when movement/crop/source identity can change
	// independently of their lower-layer content generation.
	Capture CaptureKey
}

type CaptureKey struct {
	Texture       uint32
	Width, Height int
	Region        Region
}

type layer struct {
	pair  Pair
	size  Size
	key   Key
	bytes uint64
	valid bool
}

type Stats struct{ UsedBytes, PeakBytes, BudgetBytes, TargetAllocations, CacheHits uint64 }

// Pool belongs to a single GL context generation. Its budget covers requested
// RGBA8 texel storage, including old+new targets during a transactional resize.
// Driver bookkeeping, shaders, the window framebuffer and other caches require
// their own accounting. At most two layers own two targets each.
type Pool struct {
	device Device
	layers [MaxLayers]layer
	stats  Stats
	closed bool
}

func NewPool(device Device, budget uint64) (*Pool, error) {
	if device == nil || device.MaxTextureSize() <= 0 {
		return nil, ErrInvalid
	}
	if budget == 0 {
		budget = DefaultBudget
	}
	return &Pool{device: device, stats: Stats{BudgetBytes: budget}}, nil
}

func (p *Pool) Stats() Stats { return p.stats }

func dimensions(size Size, maximum int) (Size, int, int, uint64, error) {
	if size.Downsample == 0 {
		size.Downsample = 1
	}
	if size.Width <= 0 || size.Height <= 0 || size.Width > math.MaxInt32 || size.Height > math.MaxInt32 {
		return size, 0, 0, 0, ErrInvalid
	}
	if size.Downsample != 1 && size.Downsample != 2 && size.Downsample != 4 && size.Downsample != 8 {
		return size, 0, 0, 0, ErrInvalid
	}
	d := int(size.Downsample)
	w, h := (size.Width-1)/d+1, (size.Height-1)/d+1
	if w > maximum || h > maximum {
		return size, 0, 0, 0, fmt.Errorf("%w: texture dimensions exceed %d", ErrInvalid, maximum)
	}
	// int32 dimensions fit into uint64 individually but 2 * w * h * 4 may not.
	if uint64(w) > math.MaxUint64/8/uint64(h) {
		return size, 0, 0, 0, ErrInvalid
	}
	return size, w, h, uint64(w) * uint64(h) * 8, nil
}

func validKey(key Key) bool {
	// Keep validation in float32; avoid IsInf's widened absolute-value bit
	// conversion, which can allocate temporary floats with checkptr enabled.
	finite := func(v float32) bool { return v == v && v <= math.MaxFloat32 && v >= -math.MaxFloat32 }
	if !finite(key.ScaleX) || !finite(key.ScaleY) || key.ScaleX <= 0 || key.ScaleY <= 0 || !finite(key.Radius) || key.Radius < 0 || !finite(key.Corners) || key.Corners < 0 || !finite(key.Noise) || key.Noise < 0 || key.Noise > 1 {
		return false
	}
	for _, v := range key.Tint {
		if !finite(v) || v < 0 || v > 1 {
			return false
		}
	}
	return true
}

// Update renders only on a changed key/size or invalid result. render snapshots
// lower layers into Front and uses Back for intermediate work; successful output
// belongs in Front. It must restore the caller's GL draw state. A failed render
// invalidates the cached result. No hidden rendering happens on a cache hit.
// A zero/minimized framebuffer is ErrInvalid: skip rendering until it recovers.
func (p *Pool) Update(slot int, size Size, key Key, render func(Pair) error) (Pair, bool, error) {
	if p.closed {
		return Pair{}, false, ErrClosed
	}
	if slot < 0 || slot >= MaxLayers || render == nil || !validKey(key) {
		return Pair{}, false, ErrInvalid
	}
	size, w, h, bytes, err := dimensions(size, p.device.MaxTextureSize())
	if err != nil {
		return Pair{}, false, err
	}
	l := &p.layers[slot]
	if l.bytes != 0 && l.size == size && l.key == key && l.valid {
		p.stats.CacheHits++
		return l.pair, false, nil
	}
	if l.bytes == 0 || l.pair.Front.Width != w || l.pair.Front.Height != h {
		// Check before touching the old result or asking the driver to allocate.
		if bytes > p.stats.BudgetBytes || p.stats.UsedBytes > p.stats.BudgetBytes-bytes {
			return Pair{}, false, ErrBudget
		}
		created := Pair{}
		for i, out := range []*Target{&created.Front, &created.Back} {
			handle, createErr := p.device.CreateTarget(w, h)
			if createErr == nil && (handle.Texture == 0 || handle.Framebuffer == 0) {
				p.device.DeleteTarget(handle)
				createErr = fmt.Errorf("%w: zero resource handle", ErrInvalid)
			}
			if createErr != nil {
				if i == 1 {
					p.device.DeleteTarget(created.Front.Handle)
					p.stats.UsedBytes -= bytes / 2
				}
				return Pair{}, false, createErr
			}
			*out = Target{Handle: handle, Width: w, Height: h}
			p.stats.TargetAllocations++
			p.stats.UsedBytes += bytes / 2
			if p.stats.UsedBytes > p.stats.PeakBytes {
				p.stats.PeakBytes = p.stats.UsedBytes
			}
		}
		p.release(l)
		*l = layer{pair: created, bytes: bytes}
	}
	l.valid = false
	if err := render(l.pair); err != nil {
		return Pair{}, true, err
	}
	l.size, l.key, l.valid = size, key, true
	return l.pair, true, nil
}

// UpdateAdaptive chooses the highest affordable resolution at or below the
// requested quality (Downsample 1/2/4/8). It checks old+new resize texels before
// allocating, and also degrades to fit the device texture limit. render receives
// the chosen size; returned errors are preserved without trying another render.
// No existing layer is evicted to make room. If even 1/8 cannot fit, ErrBudget
// leaves the old layer intact and the compositor can choose an opaque fallback.
// The returned pair remains borrowed under Update's lifetime contract.
func (p *Pool) UpdateAdaptive(slot int, size Size, key Key, render func(Pair, Size) error) (Pair, Size, bool, error) {
	if render == nil && !p.closed {
		return Pair{}, Size{}, false, ErrInvalid
	}
	chosen, err := p.adaptiveSize(slot, size, key)
	if err != nil {
		return Pair{}, Size{}, false, err
	}
	pair, rendered, err := p.Update(slot, chosen, key, func(pair Pair) error { return render(pair, chosen) })
	return pair, chosen, rendered, err
}

func (p *Pool) adaptiveSize(slot int, size Size, key Key) (Size, error) {
	if p.closed {
		return Size{}, ErrClosed
	}
	if slot < 0 || slot >= MaxLayers || !validKey(key) {
		return Size{}, ErrInvalid
	}
	requested, _, _, _, err := dimensions(size, math.MaxInt32)
	if err != nil {
		return Size{}, err
	}
	l := &p.layers[slot]
	for chosen := requested; chosen.Downsample <= 8; chosen.Downsample *= 2 {
		_, w, h, bytes, dimErr := dimensions(chosen, p.device.MaxTextureSize())
		if dimErr != nil {
			continue
		}
		reuse := l.bytes != 0 && l.pair.Front.Width == w && l.pair.Front.Height == h
		if !reuse && (bytes > p.stats.BudgetBytes || p.stats.UsedBytes > p.stats.BudgetBytes-bytes) {
			continue
		}
		return chosen, nil
	}
	return Size{}, ErrBudget
}

func (p *Pool) release(l *layer) {
	if l.bytes == 0 {
		return
	}
	p.device.DeleteTarget(l.pair.Front.Handle)
	p.device.DeleteTarget(l.pair.Back.Handle)
	p.stats.UsedBytes -= l.bytes
	*l = layer{}
}

func (p *Pool) Release(slot int) error {
	if p.closed {
		return ErrClosed
	}
	if slot < 0 || slot >= MaxLayers {
		return ErrInvalid
	}
	p.release(&p.layers[slot])
	return nil
}

// Close deletes all live resources while the matching context is current.
// Call before destroying the context. Repeated Close is harmless.
func (p *Pool) Close() {
	if p.closed {
		return
	}
	for i := range p.layers {
		p.release(&p.layers[i])
	}
	p.closed = true
}

// Abandon invalidates handles after the context is lost/destroyed, without
// submitting deletes into a different context. The driver owns destruction of
// the lost context's resources. Create a new pool and table for its replacement.
func (p *Pool) Abandon() {
	p.layers = [MaxLayers]layer{}
	p.stats.UsedBytes = 0
	p.closed = true
}
