package readback

import (
	"errors"
	"math"
	"testing"
	"unsafe"

	"github.com/Uzer007-admin/gl/capabilities"
)

type fakeGL struct {
	state           map[uint32]int32
	rows, pbo       bool
	reads, queries  int
	fail, panicRead bool
	err             uint32
}

func (f *fakeGL) IsFunctionAvailable(string) bool { return true }
func (f *fakeGL) GetIntegerv(name uint32, value *int32) {
	if name != packAlignment && (!f.rows && name != packBufferBinding || name == packBufferBinding && !f.pbo) {
		panic("unsupported ES 2 query")
	}
	f.queries++
	*value = f.state[name]
}
func (f *fakeGL) GetError() uint32                     { result := f.err; f.err = 0; return result }
func (f *fakeGL) PixelStorei(name uint32, value int32) { f.state[name] = value }
func (f *fakeGL) BindBuffer(_ uint32, value uint32)    { f.state[packBufferBinding] = int32(value) }
func (f *fakeGL) ReadPixels(_, _, w, h int32, format, typ uint32, p unsafe.Pointer) {
	if f.state[packAlignment] != 1 || f.rows && (f.state[packRowLength] != 0 || f.state[packSkipRows] != 0 || f.state[packSkipPixels] != 0) || f.pbo && f.state[packBufferBinding] != 0 {
		panic("unsafe pack state")
	}
	f.reads++
	if f.panicRead {
		panic("read failed")
	}
	if f.fail {
		f.err = 0x0502
		return
	}
	if format != 0x1908 || typ != 0x1401 {
		panic("unexpected format")
	}
	for i := range unsafe.Slice((*byte)(p), int(w*h*4)) {
		unsafe.Slice((*byte)(p), int(w*h*4))[i] = byte(i)
	}
}
func TestRGBA8ReadRestoresPackState(t *testing.T) {
	for _, ctx := range []capabilities.Context{{API: capabilities.OpenGL, Major: 2, Minor: 1}, {API: capabilities.OpenGLES, Major: 2}, {API: capabilities.OpenGLES, Major: 3}, {API: capabilities.WebGL, Major: 1}, {API: capabilities.WebGL, Major: 2}} {
		rows := ctx.API == capabilities.OpenGL || ctx.Major >= 3 || ctx.API == capabilities.WebGL && ctx.Major == 2
		pbo := ctx.API == capabilities.OpenGL || ctx.Major >= 3 || ctx.API == capabilities.WebGL && ctx.Major == 2
		f := &fakeGL{state: map[uint32]int32{packAlignment: 8, packRowLength: 9, packSkipRows: 3, packSkipPixels: 2, packBufferBinding: 17}, rows: rows, pbo: pbo}
		r, err := New(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		dst := make([]byte, 3*2*4+4)
		for i := range dst {
			dst[i] = 0xff
		}
		if err = r.ReadRGBA8(0, 0, 3, 2, dst[:24]); err != nil {
			t.Fatal(err)
		}
		for i := range 24 {
			if dst[i] != byte(i) {
				t.Fatal("wrong pixels")
			}
		}
		for _, v := range dst[24:] {
			if v != 0xff {
				t.Fatal("canary overwritten")
			}
		}
		if f.state[packAlignment] != 8 || f.state[packRowLength] != 9 || f.state[packSkipRows] != 3 || f.state[packSkipPixels] != 2 || f.state[packBufferBinding] != 17 {
			t.Fatal("state not restored", f.state)
		}
		if allocs := testing.AllocsPerRun(100, func() {
			if e := r.ReadRGBA8(0, 0, 3, 2, dst); e != nil {
				panic(e)
			}
		}); allocs != 0 {
			t.Fatalf("hot read allocated: %g", allocs)
		}
		for _, mode := range []string{"error", "panic"} {
			f.fail = mode == "error"
			f.panicRead = mode == "panic"
			func() {
				if f.panicRead {
					defer func() {
						if recover() == nil {
							t.Fatal("missing panic")
						}
					}()
				}
				e := r.ReadRGBA8(0, 0, 3, 2, dst)
				if f.fail && !errors.Is(e, GLError(0x0502)) {
					t.Fatal(e)
				}
			}()
			if r.busy || f.state[packAlignment] != 8 || f.pbo && f.state[packBufferBinding] != 17 {
				t.Fatal("failed read retained state")
			}
		}
	}
}
func TestRGBA8DimensionsAndShortBuffers(t *testing.T) {
	for _, size := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {1, -1}, {math.MaxInt, 1}, {math.MaxInt32, math.MaxInt32}} {
		if _, err := RGBA8Size(size[0], size[1]); !errors.Is(err, ErrDimensions) {
			t.Fatal(size, err)
		}
	}
	f := &fakeGL{state: map[uint32]int32{packAlignment: 4}}
	r, _ := New(capabilities.Context{API: capabilities.OpenGLES, Major: 2}, f)
	if err := r.ReadRGBA8(0, 0, 3, 2, make([]byte, 23)); !errors.Is(err, ErrBuffer) {
		t.Fatal(err)
	}
	if err := r.ReadRGBA8(-1, 0, 1, 1, make([]byte, 4)); !errors.Is(err, ErrDimensions) {
		t.Fatal(err)
	}
	if f.reads != 0 || f.queries != 0 {
		t.Fatal("invalid request reached GL")
	}
	f.err = 0x0501
	if err := r.ReadRGBA8(0, 0, 1, 1, make([]byte, 4)); !errors.Is(err, GLError(0x0501)) {
		t.Fatal(err)
	}
	if f.reads != 0 || f.queries != 0 {
		t.Fatal("preexisting error reached GL")
	}
}

func TestGLES2NVPackExtensions(t *testing.T) {
	ctx, err := capabilities.Query(capabilities.Queries{
		String: func(uint32) string { return "OpenGL ES 2.0" }, Integer: func(uint32) int32 { return 4096 },
		ExtensionNames: func() ([]string, error) { return []string{"GL_NV_pack_subimage", "GL_NV_pixel_buffer_object"}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGL{state: map[uint32]int32{packAlignment: 8, packRowLength: 9, packSkipRows: 3, packSkipPixels: 2, packBufferBinding: 17}, rows: true, pbo: true}
	r, err := New(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ReadRGBA8(0, 0, 1, 1, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if f.state[packBufferBinding] != 17 || f.state[packRowLength] != 9 {
		t.Fatal("NV state lost")
	}
}
