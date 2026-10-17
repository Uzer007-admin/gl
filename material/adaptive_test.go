package material

import (
	"errors"
	"testing"
)

func TestAdaptiveBudgetResizeAndRecovery(t *testing.T) {
	p, d := newTestPool(t, 1024)
	calls := 0
	render := func(pair Pair, size Size) error {
		calls++
		if pair.Front.Width != (size.Width-1)/int(size.Downsample)+1 {
			t.Fatal(pair, size)
		}
		return nil
	}
	pair, chosen, rendered, err := p.UpdateAdaptive(0, Size{20, 20, 0}, baseKey(), render)
	if err != nil || !rendered || chosen.Downsample != 2 || d.calls != 2 {
		t.Fatal(chosen, rendered, err)
	}
	again, chosen, rendered, err := p.UpdateAdaptive(0, Size{20, 20, 0}, baseKey(), render)
	if err != nil || rendered || again != pair || calls != 1 || chosen.Downsample != 2 {
		t.Fatal("adaptive cache", chosen, rendered, err)
	}
	// Old 800 bytes + new 200 bytes fit; old + full-quality resize does not.
	_, chosen, _, err = p.UpdateAdaptive(0, Size{36, 36, 0}, baseKey(), render)
	if err != nil || chosen.Downsample != 8 || p.Stats().PeakBytes > 1024 {
		t.Fatal("resize peak", chosen, p.Stats(), err)
	}
	before := p.Stats()
	_, _, _, err = p.UpdateAdaptive(1, Size{100, 100, 1}, baseKey(), render)
	if !errors.Is(err, ErrBudget) || p.Stats() != before || calls != 2 {
		t.Fatal("budget failure changed live resources", err)
	}
	if err := p.Release(0); err != nil {
		t.Fatal(err)
	}
	_, chosen, _, err = p.UpdateAdaptive(0, Size{10, 10, 1}, baseKey(), render)
	if err != nil || chosen.Downsample != 1 {
		t.Fatal("quality recovery", chosen, err)
	}
}

type limitedDevice struct{ *fakeDevice }

func (*limitedDevice) MaxTextureSize() int { return 8 }

func TestAdaptiveTextureLimitAndRenderFailure(t *testing.T) {
	d := &limitedDevice{&fakeDevice{live: make(map[Handle]bool)}}
	p, err := NewPool(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	failure := errors.New("render failed")
	calls := 0
	_, size, rendered, err := p.UpdateAdaptive(0, Size{33, 19, 1}, baseKey(), func(Pair, Size) error { calls++; return failure })
	if err != failure || !rendered || size.Downsample != 8 || calls != 1 || d.calls != 2 {
		t.Fatal("render error must not trigger quality retries", size, rendered, err)
	}
	before := d.calls
	if _, _, _, err := p.UpdateAdaptive(0, Size{0, 19, 1}, baseKey(), func(Pair, Size) error { return nil }); err != ErrInvalid || d.calls != before {
		t.Fatal("invalid size allocated", err)
	}
}

func BenchmarkAdaptiveCachedMaterial(b *testing.B) {
	p, _ := newTestPool(b, 1024)
	render := func(Pair, Size) error { return nil }
	size, key := Size{20, 20, 1}, baseKey()
	if _, _, _, err := p.UpdateAdaptive(0, size, key, render); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := p.UpdateAdaptive(0, size, key, render); err != nil {
			b.Fatal(err)
		}
	}
}
