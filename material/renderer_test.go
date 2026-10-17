package material

import "testing"

func cachedRenderer(t testing.TB) (*Renderer, Target, Region, Key) {
	t.Helper()
	p, _ := newTestPool(t, 1024)
	source := Target{Handle: Handle{77, 88}, Width: 32, Height: 24}
	region := Region{3, 2, 25, 19}
	key := baseKey()
	stored := key
	stored.Capture = CaptureKey{source.Texture, source.Width, source.Height, region}
	if _, _, err := p.Update(0, Size{25, 19, 4}, stored, func(Pair) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &Renderer{device: &GLDevice{maximum: 4096}, pool: p}, source, region, key
}

func TestRendererStaticDoesNotAllocateOrDraw(t *testing.T) {
	r, source, region, key := cachedRenderer(t)
	// blur is deliberately nil: a cache hit must reach no GPU draw method.
	if allocations := testing.AllocsPerRun(100, func() {
		result, err := r.Render(0, source, region, 1, key)
		if err != nil || result.Rendered || result.Size.Downsample != 4 {
			t.Fatal(result, err)
		}
	}); allocations != 0 {
		t.Fatalf("static cache allocations: %v", allocations)
	}
}

func BenchmarkRendererCachedMaterial(b *testing.B) {
	r, source, region, key := cachedRenderer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(0, source, region, 1, key); err != nil {
			b.Fatal(err)
		}
	}
}
