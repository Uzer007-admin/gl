package material

import (
	"errors"
	"math"
	"testing"
)

type fakeDevice struct {
	next, calls, fail int
	live              map[Handle]bool
}

func (d *fakeDevice) MaxTextureSize() int { return 4096 }
func (d *fakeDevice) CreateTarget(int, int) (Handle, error) {
	d.calls++
	if d.calls == d.fail {
		return Handle{}, errors.New("allocation failed")
	}
	d.next++
	h := Handle{uint32(d.next), uint32(d.next + 100)}
	d.live[h] = true
	return h, nil
}
func (d *fakeDevice) DeleteTarget(h Handle) { delete(d.live, h) }
func newTestPool(t testing.TB, budget uint64) (*Pool, *fakeDevice) {
	t.Helper()
	d := &fakeDevice{live: make(map[Handle]bool)}
	p, err := NewPool(d, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, d
}
func baseKey() Key {
	return Key{Surface: 1, Content: 1, Style: 1, ScaleX: 1.25, ScaleY: 1.5, Radius: 8}
}
func TestCacheAndDownsample(t *testing.T) {
	p, d := newTestPool(t, 0)
	calls := 0
	render := func(pair Pair) error {
		calls++
		if pair.Front.Width != 51 || pair.Front.Height != 26 || pair.Front.Handle == pair.Back.Handle {
			t.Fatal(pair)
		}
		return nil
	}
	size := Size{101, 51, 2}
	a, changed, err := p.Update(0, size, baseKey(), render)
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	b, changed, err := p.Update(0, size, baseKey(), render)
	if err != nil || changed || b != a || calls != 1 || d.calls != 2 {
		t.Fatalf("cache miss: %v %v %+v", changed, err, p.Stats())
	}
	k := baseKey()
	k.ScaleY = 1.75
	if _, changed, err := p.Update(0, size, k, render); err != nil || !changed || d.calls != 2 || calls != 2 {
		t.Fatalf("DPI: %v %v", changed, err)
	}
	p.Close()
	p.Close()
	if len(d.live) != 0 || p.Stats().UsedBytes != 0 {
		t.Fatal("resources leaked")
	}
}
func TestResizePeakAndRollback(t *testing.T) {
	p, d := newTestPool(t, 1000)
	render := func(Pair) error { return nil }
	old, _, err := p.Update(0, Size{10, 10, 1}, baseKey(), render)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Update(0, Size{12, 12, 1}, baseKey(), render); !errors.Is(err, ErrBudget) || d.calls != 2 {
		t.Fatal("budget checked after allocation", err)
	}
	// A failed resize does not destroy or invalidate the old cached result.
	if got, updated, err := p.Update(0, Size{10, 10, 1}, baseKey(), render); err != nil || updated || got != old {
		t.Fatal(err)
	}
	p.stats.BudgetBytes = 3000
	d.fail = 4 // second allocation of replacement
	if _, _, err := p.Update(0, Size{12, 12, 1}, baseKey(), render); err == nil || len(d.live) != 2 || p.Stats().UsedBytes != 800 || p.Stats().PeakBytes != 1376 {
		t.Fatalf("rollback: %+v %v", p.Stats(), err)
	}
	d.fail = 0
	if _, _, err := p.Update(0, Size{12, 12, 1}, baseKey(), render); err != nil || len(d.live) != 2 || p.Stats().UsedBytes != 1152 || p.Stats().PeakBytes != 1952 {
		t.Fatalf("resize: %+v %v", p.Stats(), err)
	}
}
func TestFailedRenderAndContextLoss(t *testing.T) {
	p, d := newTestPool(t, 0)
	if _, _, err := p.Update(0, Size{8, 8, 1}, baseKey(), func(Pair) error { return errors.New("shader failed") }); err == nil {
		t.Fatal("missing error")
	}
	if _, changed, err := p.Update(0, Size{8, 8, 1}, baseKey(), func(Pair) error { return nil }); err != nil || !changed || d.calls != 2 {
		t.Fatal(err)
	}
	p.Abandon()
	p.Close()
	if len(d.live) != 2 || p.Stats().UsedBytes != 0 {
		t.Fatal("submitted deletes after context loss")
	}
	if _, _, err := p.Update(0, Size{8, 8, 1}, baseKey(), func(Pair) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
func TestInvalidRequestsAndTwoLayers(t *testing.T) {
	p, d := newTestPool(t, 0)
	for _, size := range []Size{{0, 10, 1}, {-1, 10, 1}, {10, 10, 3}, {8193, 10, 2}, {math.MaxInt32, math.MaxInt32, 1}} {
		if _, _, err := p.Update(0, size, baseKey(), func(Pair) error { return nil }); !errors.Is(err, ErrInvalid) {
			t.Fatal(size, err)
		}
	}
	k := baseKey()
	k.ScaleX = float32(math.NaN())
	if _, _, err := p.Update(0, Size{8, 8, 1}, k, func(Pair) error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if d.calls != 0 {
		t.Fatal("invalid request allocated")
	}
	for i := 0; i < MaxLayers; i++ {
		if _, _, err := p.Update(i, Size{8, 8, 1}, baseKey(), func(Pair) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.Update(2, Size{8, 8, 1}, baseKey(), func(Pair) error { return nil }); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := p.Release(0); err != nil || len(d.live) != 2 {
		t.Fatal(err)
	}
}
func BenchmarkCachedMaterial(b *testing.B) {
	p, _ := newTestPool(b, 0)
	size, key := Size{640, 720, 2}, baseKey()
	render := func(Pair) error { return nil }
	if _, _, err := p.Update(0, size, key, render); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, changed, err := p.Update(0, size, key, render); changed || err != nil {
			b.Fatal(changed, err)
		}
	}
}
