package material

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/Uzer007-admin/gl/capabilities"
)

var ErrUnsupported = errors.New("material: texture framebuffers unsupported")

// Procedures is implemented by the generated material GL and GLES 2 Functions
// tables. It can also be adapted to WebGL with an object-name registry. Queries
// and resource creation must use the same live context as the capability snapshot.
type Procedures interface {
	IsFunctionAvailable(string) bool
	GetIntegerv(uint32, *int32)
	GetError() uint32
	GenTextures(int32, *uint32)
	DeleteTextures(int32, *uint32)
	BindTexture(uint32, uint32)
	TexParameteri(uint32, uint32, int32)
	TexImage2D(uint32, int32, int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	BindBuffer(uint32, uint32)
	Viewport(int32, int32, int32, int32)
	CopyTexSubImage2D(uint32, int32, int32, int32, int32, int32, int32, int32)
}

type coreFramebuffers interface {
	GenFramebuffers(int32, *uint32)
	DeleteFramebuffers(int32, *uint32)
	BindFramebuffer(uint32, uint32)
	FramebufferTexture2D(uint32, uint32, uint32, uint32, int32)
	CheckFramebufferStatus(uint32) uint32
}
type extFramebuffers interface {
	GenFramebuffersEXT(int32, *uint32)
	DeleteFramebuffersEXT(int32, *uint32)
	BindFramebufferEXT(uint32, uint32)
	FramebufferTexture2DEXT(uint32, uint32, uint32, uint32, int32)
	CheckFramebufferStatusEXT(uint32) uint32
}

// Optional batched bridges emitted by Glow. Adapters without these methods
// retain the existing GetIntegerv path and the same restoration contract.
type targetQueries interface {
	GetFramebufferViewport(bool) (int32, int32, [4]int32)
	GetTargetBindings(bool, bool) (int32, int32, int32, int32)
}

// GLDevice allocates RGBA8 texture targets with API/extension-checked core or
// EXT framebuffer procedures. It restores texture, pixel-unpack-buffer and both
// read/draw framebuffer bindings, without changing active texture or viewport.
// Error state must be clean on entry; errors encountered are returned with their
// GL code (GetError consumes them). It never uses ReadPixels or GL synchronization.
type GLDevice struct {
	api                      Procedures
	maximum                  int
	split, unpack, sizedRGBA bool
	gen, remove              func(int32, *uint32)
	bind                     func(uint32, uint32)
	attach                   func(uint32, uint32, uint32, uint32, int32)
	check                    func(uint32) uint32
	query                    [4]int32
	queries                  targetQueries
}

func NewGLDevice(context capabilities.Context, api Procedures) (*GLDevice, error) {
	if api == nil || context.MaxTextureSize <= 0 || context.MaxTextureSize > 0x7fffffff {
		return nil, ErrInvalid
	}
	for _, name := range []string{"glGetIntegerv", "glGetError", "glGenTextures", "glDeleteTextures", "glBindTexture", "glTexParameteri", "glTexImage2D", "glBindBuffer", "glCopyTexSubImage2D"} {
		if !api.IsFunctionAvailable(name) {
			return nil, fmt.Errorf("%w: %s", ErrUnsupported, name)
		}
	}
	d := &GLDevice{api: api, maximum: context.MaxTextureSize,
		split:     context.API == capabilities.OpenGL && (context.AtLeast(3, 0) || context.HasExtension("GL_ARB_framebuffer_object") || context.HasExtension("GL_EXT_framebuffer_blit")) || context.API == capabilities.OpenGLES && context.AtLeast(3, 0) || context.API == capabilities.WebGL && context.AtLeast(2, 0),
		unpack:    context.API == capabilities.OpenGL && (context.AtLeast(2, 1) || context.HasExtension("GL_ARB_pixel_buffer_object")) || context.API == capabilities.OpenGLES && context.AtLeast(3, 0) || context.API == capabilities.WebGL && context.AtLeast(2, 0),
		sizedRGBA: context.API == capabilities.OpenGL || context.API == capabilities.OpenGLES && context.AtLeast(3, 0) || context.API == capabilities.WebGL && context.AtLeast(2, 0)}
	switch context.Framebuffers(api.IsFunctionAvailable) {
	case capabilities.CoreFramebuffer:
		f, ok := api.(coreFramebuffers)
		if !ok {
			return nil, ErrUnsupported
		}
		d.gen, d.remove, d.bind, d.attach, d.check = f.GenFramebuffers, f.DeleteFramebuffers, f.BindFramebuffer, f.FramebufferTexture2D, f.CheckFramebufferStatus
	case capabilities.EXTFramebuffer:
		f, ok := api.(extFramebuffers)
		if !ok {
			return nil, ErrUnsupported
		}
		d.gen, d.remove, d.bind, d.attach, d.check = f.GenFramebuffersEXT, f.DeleteFramebuffersEXT, f.BindFramebufferEXT, f.FramebufferTexture2DEXT, f.CheckFramebufferStatusEXT
	default:
		return nil, ErrUnsupported
	}
	d.queries, _ = api.(targetQueries)
	return d, nil
}

func (d *GLDevice) MaxTextureSize() int { return d.maximum }

func (d *GLDevice) integer(name uint32) int32 {
	d.api.GetIntegerv(name, &d.query[0])
	return d.query[0]
}

func (d *GLDevice) restoreFramebuffers(draw, read int32) {
	if d.split {
		d.bind(0x8CA9, uint32(draw))
		d.bind(0x8CA8, uint32(read))
	} else {
		d.bind(0x8D40, uint32(draw))
	}
}

// RenderTarget scopes framebuffer and viewport state around a GPU pass. It
// restores separate read/draw bindings even on a returned draw error. The draw
// callback owns/restores program, texture, buffer, blend, scissor and other state.
// No per-pass query/output storage is allocated: the device reuses its scratch.
func (d *GLDevice) RenderTarget(target Target, draw func() error) error {
	if draw == nil || target.Texture == 0 || target.Framebuffer == 0 || target.Width <= 0 || target.Height <= 0 || target.Width > d.maximum || target.Height > d.maximum {
		return ErrInvalid
	}
	var oldDraw, oldRead int32
	var viewport [4]int32
	if d.queries != nil {
		oldDraw, oldRead, viewport = d.queries.GetFramebufferViewport(d.split)
	} else {
		oldDraw = d.integer(0x8CA6)
		if d.split {
			oldRead = d.integer(0x8CAA)
		}
		d.api.GetIntegerv(0x0BA2, &d.query[0]) // VIEWPORT
		viewport = d.query
	}
	defer func() {
		d.restoreFramebuffers(oldDraw, oldRead)
		d.api.Viewport(viewport[0], viewport[1], viewport[2], viewport[3])
	}()
	d.bind(0x8D40, target.Framebuffer)
	d.api.Viewport(0, 0, int32(target.Width), int32(target.Height))
	return draw()
}

// CopySnapshot copies a physical region from the caller's current read
// framebuffer into a target texture, entirely on the GPU. Source coordinates
// use GL's bottom-left origin. The caller selects its read buffer, ensures the
// region is inside its readable framebuffer, and captures only lower layers.
// It preserves texture/FBO/PBO/viewport state. Copying a target into itself is
// rejected. This operation does not rescale; downsampling needs a GPU draw pass.
func (d *GLDevice) CopySnapshot(target Target, x, y int) error {
	if target.Texture == 0 || target.Framebuffer == 0 || target.Width <= 0 || target.Height <= 0 || target.Width > d.maximum || target.Height > d.maximum || x < 0 || y < 0 || x > 0x7fffffff-target.Width || y > 0x7fffffff-target.Height {
		return ErrInvalid
	}
	readEnum := uint32(0x8CA6)
	if d.split {
		readEnum = 0x8CAA
	}
	if uint32(d.integer(readEnum)) == target.Framebuffer {
		return fmt.Errorf("%w: snapshot feedback", ErrInvalid)
	}
	if code := d.api.GetError(); code != 0 {
		return GLError(code)
	}
	texture := d.integer(0x8069)
	defer d.api.BindTexture(0x0DE1, uint32(texture))
	d.api.BindTexture(0x0DE1, target.Texture)
	d.api.CopyTexSubImage2D(0x0DE1, 0, 0, 0, int32(x), int32(y), int32(target.Width), int32(target.Height))
	if code := d.api.GetError(); code != 0 {
		return GLError(code)
	}
	return nil
}

type GLError uint32

func (e GLError) Error() string { return fmt.Sprintf("material: GL error 0x%04x", uint32(e)) }

func (d *GLDevice) CreateTarget(width, height int) (handle Handle, err error) {
	if width <= 0 || height <= 0 || width > d.maximum || height > d.maximum {
		return Handle{}, ErrInvalid
	}
	if code := d.api.GetError(); code != 0 {
		return Handle{}, GLError(code)
	}
	const texture2D, framebuffer = 0x0DE1, 0x8D40
	var texture, draw, read, unpack int32
	if d.queries != nil {
		texture, draw, read, unpack = d.queries.GetTargetBindings(d.split, d.unpack)
	} else {
		texture, draw = d.integer(0x8069), d.integer(0x8CA6)
		if d.split {
			read = d.integer(0x8CAA)
		}
		if d.unpack {
			unpack = d.integer(0x88EF)
		}
	}
	if d.unpack {
		d.api.BindBuffer(0x88EC, 0)
	}
	defer func() {
		if err != nil {
			d.DeleteTarget(handle)
			handle = Handle{}
		}
		d.api.BindTexture(texture2D, uint32(texture))
		d.restoreFramebuffers(draw, read)
		if d.unpack {
			d.api.BindBuffer(0x88EC, uint32(unpack))
		}
	}()
	d.api.GenTextures(1, &handle.Texture)
	d.gen(1, &handle.Framebuffer)
	if handle.Texture == 0 || handle.Framebuffer == 0 {
		return handle, fmt.Errorf("%w: zero resource handle", ErrInvalid)
	}
	d.api.BindTexture(texture2D, handle.Texture)
	d.api.TexParameteri(texture2D, 0x2801, 0x2601) // MIN_FILTER LINEAR, no mip levels
	d.api.TexParameteri(texture2D, 0x2800, 0x2601) // MAG_FILTER
	d.api.TexParameteri(texture2D, 0x2802, 0x812F) // WRAP_S/T CLAMP_TO_EDGE
	d.api.TexParameteri(texture2D, 0x2803, 0x812F)
	format := int32(0x1908) // ES 2 requires unsized RGBA + UNSIGNED_BYTE
	if d.sizedRGBA {
		format = 0x8058
	} // RGBA8
	d.api.TexImage2D(texture2D, 0, format, int32(width), int32(height), 0, 0x1908, 0x1401, nil)
	d.bind(framebuffer, handle.Framebuffer)
	d.attach(framebuffer, 0x8CE0, texture2D, handle.Texture, 0)
	status := d.check(framebuffer)
	if code := d.api.GetError(); code != 0 {
		return handle, GLError(code)
	}
	if status != 0x8CD5 {
		return handle, fmt.Errorf("material: incomplete framebuffer 0x%04x", status)
	}
	return handle, nil
}

func (d *GLDevice) DeleteTarget(handle Handle) {
	if handle.Framebuffer != 0 {
		d.remove(1, &handle.Framebuffer)
	}
	if handle.Texture != 0 {
		d.api.DeleteTextures(1, &handle.Texture)
	}
}
