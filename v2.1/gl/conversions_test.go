package gl

import (
	"bytes"
	"runtime"
	"testing"
	"unsafe"
)

func TestStrsTermination(t *testing.T) {
	values := []string{"", "A", "B\x00", "embedded\x00NUL", "é"}
	pointers, free := Strs(values...)
	defer free()
	runtime.GC()
	for i, pointer := range unsafe.Slice(pointers, len(values)) {
		value := values[i]
		expected := []byte(value)
		if len(value) == 0 || value[len(value)-1] != 0 {
			expected = append(expected, 0)
		}
		if data := unsafe.Slice(pointer, len(expected)); !bytes.Equal(data, expected) {
			t.Fatalf("%d: %x != %x", i, data, expected)
		}
	}
}

func TestStrsExactLengths(t *testing.T) {
	p, lengths, free := StrsWithLengths("A\x00B", "")
	defer free()
	if lengths[0] != 3 || lengths[1] != 0 || string(unsafe.Slice(*p, 3)) != "A\x00B" {
		t.Fatal("exact lengths lost")
	}
}

func TestNumericSlicePointer(t *testing.T) {
	var nilPointer *float32
	if Ptr(nilPointer) != nil || Ptr([]float32{}) != nil || SlicePtr([]uint32{}) != nil {
		t.Fatal("empty value must have nil address")
	}
	data := []uint16{1, 2, 3}
	if SliceBytes(data) != 6 || SlicePtr(data) != unsafe.Pointer(&data[0]) {
		t.Fatal("numeric size/address")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if SlicePtr(data) == nil {
			panic("nil")
		}
	}); allocations != 0 {
		t.Fatalf("%g allocations", allocations)
	}
}
