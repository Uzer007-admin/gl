// Package readback reads tightly packed RGBA8 pixels without borrowing the
// caller's pixel-pack layout or treating a Go pointer as a PBO offset.
package readback

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/Uzer007-admin/gl/capabilities"
)

var (
	ErrDimensions = errors.New("gl readback: invalid or overflowing dimensions")
	ErrBuffer     = errors.New("gl readback: destination buffer is too small")
	ErrProcedures = errors.New("gl readback: required procedures unavailable")
	ErrBusy       = errors.New("gl readback: reader is already in use")
)

const (
	packAlignment     = 0x0D05
	packRowLength     = 0x0D02
	packSkipRows      = 0x0D03
	packSkipPixels    = 0x0D04
	packBufferBinding = 0x88ED
	packBuffer        = 0x88EB
)

// RGBA8Size validates GL's signed 32-bit dimensions and Go allocation arithmetic.
// Zero-sized native surfaces are suspended surfaces, not successful captures.
func RGBA8Size(width, height int) (int, error) {
	const limit = 0x7fffffff
	maxInt := int(^uint(0) >> 1)
	if width <= 0 || height <= 0 || width > limit || height > limit || width > maxInt/4 || height > maxInt/(width*4) {
		return 0, ErrDimensions
	}
	return width * height * 4, nil
}

// Procedures is implemented by immutable material GL and GLES Functions tables.
// A Reader belongs to one current context on its owner thread. It does not bind
// framebuffers or choose read buffers: those are owned by the caller's GPU pass.
type Procedures interface {
	IsFunctionAvailable(string) bool
	GetIntegerv(uint32, *int32)
	GetError() uint32
	PixelStorei(uint32, int32)
	BindBuffer(uint32, uint32)
	ReadPixels(int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
}

type Reader struct {
	api       Procedures
	rows, pbo bool
	busy      bool
	saved     [5]int32
}

func New(context capabilities.Context, api Procedures) (*Reader, error) {
	if api == nil || context.Major < 1 || context.API > capabilities.WebGL {
		return nil, ErrProcedures
	}
	for _, name := range []string{"glGetIntegerv", "glGetError", "glPixelStorei", "glReadPixels"} {
		if !api.IsFunctionAvailable(name) {
			return nil, fmt.Errorf("%w: %s", ErrProcedures, name)
		}
	}
	r := &Reader{api: api,
		rows: context.API == capabilities.OpenGL || context.API == capabilities.OpenGLES && (context.AtLeast(3, 0) || context.HasExtension("GL_NV_pack_subimage")) || context.API == capabilities.WebGL && context.AtLeast(2, 0),
		pbo:  context.API == capabilities.OpenGL && (context.AtLeast(2, 1) || context.HasExtension("GL_ARB_pixel_buffer_object") || context.HasExtension("GL_EXT_pixel_buffer_object")) || context.API == capabilities.OpenGLES && (context.AtLeast(3, 0) || context.HasExtension("GL_NV_pixel_buffer_object")) || context.API == capabilities.WebGL && context.AtLeast(2, 0)}
	if r.pbo && !api.IsFunctionAvailable("glBindBuffer") {
		return nil, ErrProcedures
	}
	return r, nil
}

type GLError uint32

func (e GLError) Error() string { return fmt.Sprintf("gl readback: GL error 0x%04x", uint32(e)) }

// ReadRGBA8 writes bottom-up, tightly packed RGBA8 into dst. The caller must
// provide a complete readable framebuffer and a clean GL error state. Pack
// alignment, row length, skips and PBO binding are restored, including on panic.
// ES 2/WebGL 1 never query unsupported row-length or pixel-buffer state.
// Once initialized, successful reads allocate no Go memory besides caller dst.
func (r *Reader) ReadRGBA8(x, y, width, height int, dst []byte) error {
	size, err := RGBA8Size(width, height)
	if err != nil || x < 0 || y < 0 || x > 0x7fffffff-width || y > 0x7fffffff-height {
		return ErrDimensions
	}
	if len(dst) < size {
		return ErrBuffer
	}
	if r.busy {
		return ErrBusy
	}
	if code := r.api.GetError(); code != 0 {
		return GLError(code)
	}
	r.busy = true
	defer func() { r.busy = false }()
	r.api.GetIntegerv(packAlignment, &r.saved[0])
	if r.rows {
		r.api.GetIntegerv(packRowLength, &r.saved[1])
		r.api.GetIntegerv(packSkipRows, &r.saved[2])
		r.api.GetIntegerv(packSkipPixels, &r.saved[3])
	}
	if r.pbo {
		r.api.GetIntegerv(packBufferBinding, &r.saved[4])
	}
	defer func() {
		r.api.PixelStorei(packAlignment, r.saved[0])
		if r.rows {
			r.api.PixelStorei(packRowLength, r.saved[1])
			r.api.PixelStorei(packSkipRows, r.saved[2])
			r.api.PixelStorei(packSkipPixels, r.saved[3])
		}
		if r.pbo {
			r.api.BindBuffer(packBuffer, uint32(r.saved[4]))
		}
	}()
	r.api.PixelStorei(packAlignment, 1)
	if r.rows {
		r.api.PixelStorei(packRowLength, 0)
		r.api.PixelStorei(packSkipRows, 0)
		r.api.PixelStorei(packSkipPixels, 0)
	}
	if r.pbo {
		r.api.BindBuffer(packBuffer, 0)
	}
	r.api.ReadPixels(int32(x), int32(y), int32(width), int32(height), 0x1908, 0x1401, unsafe.Pointer(unsafe.SliceData(dst[:size])))
	runtime.KeepAlive(dst)
	if code := r.api.GetError(); code != 0 {
		return GLError(code)
	}
	return nil
}
