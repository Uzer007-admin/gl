package material

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/Uzer007-admin/gl/capabilities"
)

// DrawProcedures is supplied by a matching generated material/GLES Functions
// table, or a WebGL adapter. A compositor using Blur owns the draw state:
// program, vertex input, array buffer, texture unit 0, blending, scissor, culling
// and color mask are set by Apply. Framebuffer and viewport are scoped/restored.
// The caller rebinds its own draw state afterwards. Desktop fixed-function alpha
// test and rasterizer discard must be disabled by that compositor, where present.
type DrawProcedures interface {
	Procedures
	CreateShader(uint32) uint32
	ShaderSourceString(uint32, string) error
	CompileShader(uint32)
	GetShaderiv(uint32, uint32, *int32)
	GetShaderInfoLog(uint32, int32, *int32, *uint8)
	DeleteShader(uint32)
	CreateProgram() uint32
	AttachShader(uint32, uint32)
	LinkProgram(uint32)
	GetProgramiv(uint32, uint32, *int32)
	GetProgramInfoLog(uint32, int32, *int32, *uint8)
	DeleteProgram(uint32)
	UseProgram(uint32)
	GetAttribLocation(uint32, *uint8) int32
	GetUniformLocation(uint32, *uint8) int32
	GenBuffers(int32, *uint32)
	DeleteBuffers(int32, *uint32)
	BufferData(uint32, int, unsafe.Pointer, uint32)
	ActiveTexture(uint32)
	EnableVertexAttribArray(uint32)
	VertexAttribPointerWithOffset(uint32, int32, uint32, bool, int32, uintptr)
	Uniform1i(int32, int32)
	Uniform1f(int32, float32)
	Uniform2f(int32, float32, float32)
	Uniform4f(int32, float32, float32, float32, float32)
	Disable(uint32)
	ColorMask(bool, bool, bool, bool)
	DrawArrays(uint32, int32, int32)
}

// Blur owns one program/VBO for a context generation. It performs a separable
// five-tap Gaussian, rounded mask, tint and deterministic noise entirely on the
// GPU. Pool.Update avoids both passes on an unchanged source/key. Front must
// already contain premultiplied lower-layer pixels; it must not contain an old
// material result. Radius/Corners are logical units; ScaleX/Y convert to source
// pixels independently. Size is the physical source region before downsampling.
// Output is premultiplied RGBA with RGB=0 at alpha=0. Linear optionally decodes
// sRGB samples before filtering and encodes the final straight color afterwards;
// the intermediate target stores linear premultiplied values in RGBA8.
type Blur struct {
	device                                                                 *GLDevice
	api                                                                    DrawProcedures
	program, buffer                                                        uint32
	position                                                               uint32
	sampler, axis, final, tint, extent, corners, noise, linear, sampleSize int32
	captureBounds, captureStep                                             int32
}

func NewBlur(context capabilities.Context, device *GLDevice, api DrawProcedures) (*Blur, error) {
	if device == nil || api == nil {
		return nil, ErrInvalid
	}
	for _, name := range []string{"glCreateShader", "glShaderSource", "glCompileShader", "glGetShaderiv", "glGetShaderInfoLog", "glDeleteShader", "glCreateProgram", "glAttachShader", "glLinkProgram", "glGetProgramiv", "glGetProgramInfoLog", "glDeleteProgram", "glUseProgram", "glGetAttribLocation", "glGetUniformLocation", "glGenBuffers", "glDeleteBuffers", "glBufferData", "glActiveTexture", "glEnableVertexAttribArray", "glVertexAttribPointer", "glUniform1i", "glUniform1f", "glUniform2f", "glUniform4f", "glDisable", "glColorMask", "glDrawArrays"} {
		if !api.IsFunctionAvailable(name) {
			return nil, fmt.Errorf("%w: %s", ErrUnsupported, name)
		}
	}
	vertexPrefix, fragmentPrefix := "#version 120\n", "#version 120\n"
	if context.API == capabilities.OpenGLES || context.API == capabilities.WebGL {
		vertexPrefix = "#version 100\n"
		fragmentPrefix = "#version 100\n#ifdef GL_FRAGMENT_PRECISION_HIGH\nprecision highp float;\n#else\nprecision mediump float;\n#endif\n"
	}
	vertex, err := compile(api, 0x8B31, vertexPrefix+blurVertex)
	if err != nil {
		return nil, err
	}
	defer api.DeleteShader(vertex)
	fragment, err := compile(api, 0x8B30, fragmentPrefix+blurFragment)
	if err != nil {
		return nil, err
	}
	defer api.DeleteShader(fragment)
	b := &Blur{device: device, api: api, program: api.CreateProgram()}
	if b.program == 0 {
		return nil, fmt.Errorf("material: could not create program")
	}
	api.AttachShader(b.program, vertex)
	api.AttachShader(b.program, fragment)
	api.LinkProgram(b.program)
	var ok int32
	api.GetProgramiv(b.program, 0x8B82, &ok)
	if ok == 0 {
		log := shaderLog(api, b.program, false)
		b.Close()
		return nil, fmt.Errorf("material: link blur: %s", log)
	}
	position := api.GetAttribLocation(b.program, unsafe.StringData("position\x00"))
	if position < 0 {
		b.Close()
		return nil, fmt.Errorf("material: missing blur position")
	}
	b.position = uint32(position)
	locations := []*int32{&b.sampler, &b.axis, &b.final, &b.tint, &b.extent, &b.corners, &b.noise, &b.linear, &b.sampleSize, &b.captureBounds, &b.captureStep}
	for i, name := range []string{"source\x00", "axis\x00", "finalPass\x00", "tint\x00", "extent\x00", "corners\x00", "noise\x00", "linear\x00", "sampleSize\x00", "captureBounds\x00", "captureStep\x00"} {
		*locations[i] = api.GetUniformLocation(b.program, unsafe.StringData(name))
		if *locations[i] < 0 {
			b.Close()
			return nil, fmt.Errorf("material: missing blur uniform %q", name)
		}
	}
	api.GenBuffers(1, &b.buffer)
	if b.buffer == 0 {
		b.Close()
		return nil, fmt.Errorf("material: could not create blur buffer")
	}
	old := device.integer(0x8894) // ARRAY_BUFFER_BINDING
	api.BindBuffer(0x8892, b.buffer)
	points := [8]float32{-1, -1, 1, -1, -1, 1, 1, 1}
	api.BufferData(0x8892, len(points)*4, unsafe.Pointer(&points[0]), 0x88E4)
	api.BindBuffer(0x8892, uint32(old))
	if code := api.GetError(); code != 0 {
		b.Close()
		return nil, GLError(code)
	}
	return b, nil
}

func compile(api DrawProcedures, kind uint32, source string) (uint32, error) {
	id := api.CreateShader(kind)
	if id == 0 {
		return 0, fmt.Errorf("material: could not create shader")
	}
	if err := api.ShaderSourceString(id, source); err != nil {
		api.DeleteShader(id)
		return 0, err
	}
	api.CompileShader(id)
	var ok int32
	api.GetShaderiv(id, 0x8B81, &ok)
	if ok == 0 {
		log := shaderLog(api, id, true)
		api.DeleteShader(id)
		return 0, fmt.Errorf("material: compile blur: %s", log)
	}
	return id, nil
}

func shaderLog(api DrawProcedures, id uint32, shader bool) string {
	var length int32
	if shader {
		api.GetShaderiv(id, 0x8B84, &length)
	} else {
		api.GetProgramiv(id, 0x8B84, &length)
	}
	if length < 1 {
		return "no compiler diagnostic"
	}
	if length > 65536 {
		length = 65536
	}
	data := make([]byte, length)
	if shader {
		api.GetShaderInfoLog(id, length, nil, &data[0])
	} else {
		api.GetProgramInfoLog(id, length, nil, &data[0])
	}
	for i, c := range data {
		if c == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func (b *Blur) Apply(pair Pair, size Size, key Key) error {
	if b.program == 0 {
		return ErrClosed
	}
	_, w, h, _, err := dimensions(size, b.device.maximum)
	if err != nil || !validKey(key) {
		return ErrInvalid
	}
	if pair.Front.Texture == 0 || pair.Back.Texture == 0 || pair.Front.Framebuffer == 0 || pair.Back.Framebuffer == 0 || pair.Front.Width != w || pair.Front.Height != h || pair.Back.Width != w || pair.Back.Height != h || pair.Front.Texture == pair.Back.Texture || pair.Front.Framebuffer == pair.Back.Framebuffer {
		return ErrInvalid
	}
	stepX, stepY := key.Radius*key.ScaleX/(2*float32(size.Width)), key.Radius*key.ScaleY/(2*float32(size.Height))
	extentX, extentY := float32(size.Width)/key.ScaleX, float32(size.Height)/key.ScaleY
	for _, value := range []float32{stepX, stepY, extentX, extentY} {
		if value != value || value > math.MaxFloat32 || value < -math.MaxFloat32 {
			return ErrInvalid
		}
	}
	if extentX <= 0 || extentY <= 0 {
		return ErrInvalid
	}
	b.prepareDraw()
	a := b.api
	a.Uniform1i(b.sampler, 0)
	a.Uniform2f(b.sampleSize, float32(w), float32(h))
	a.Uniform4f(b.tint, key.Tint[0], key.Tint[1], key.Tint[2], key.Tint[3])
	a.Uniform2f(b.extent, extentX, extentY)
	a.Uniform2f(b.corners, key.Corners, min(key.ScaleX, key.ScaleY))
	a.Uniform1f(b.noise, key.Noise)
	linear := int32(0)
	if key.Linear {
		linear = 1
	}
	a.Uniform1i(b.linear, linear)
	err = b.device.RenderTarget(pair.Back, func() error {
		a.BindTexture(0x0DE1, pair.Front.Texture)
		a.Uniform2f(b.axis, stepX, 0)
		a.Uniform1i(b.final, 0)
		a.DrawArrays(0x0005, 0, 4)
		return nil
	})
	if err != nil {
		return err
	}
	err = b.device.RenderTarget(pair.Front, func() error {
		a.BindTexture(0x0DE1, pair.Back.Texture)
		a.Uniform2f(b.axis, 0, stepY)
		a.Uniform1i(b.final, 1)
		a.DrawArrays(0x0005, 0, 4)
		return nil
	})
	if err != nil {
		return err
	}
	if code := a.GetError(); code != 0 {
		return GLError(code)
	}
	return nil
}

// Region is a physical source-texture rectangle in GL bottom-left coordinates.
type Region struct{ X, Y, Width, Height int }

// Capture crops lower-layer pixels from an existing premultiplied RGBA8 texture
// into target on the GPU, reducing each axis by up to 8. No full-size per-panel
// snapshot or extra target is allocated. Four bilinear samples prefilter each
// output texel; at factor 2 this is a 2x2 box, while 4/8 are cheaper approximations
// rather than exact area filters. Edges clamp to the requested region. Linear
// decodes before interpolation/filtering and re-encodes the captured texture.
// Source lifetime/context and texel budget belong to the compositor. Capture
// owns the same draw state as Apply, and scopes/restores FBO/viewport. Use inside
// Pool.Update/UpdateAdaptive before Apply so a cache hit skips capture as well.
func (b *Blur) Capture(target, source Target, region Region, linear bool) error {
	if b.program == 0 {
		return ErrClosed
	}
	if source.Texture == 0 || source.Width <= 0 || source.Height <= 0 || source.Width > b.device.maximum || source.Height > b.device.maximum || target.Texture == 0 || target.Framebuffer == 0 || target.Texture == source.Texture || target.Width <= 0 || target.Height <= 0 || target.Width > b.device.maximum || target.Height > b.device.maximum || region.X < 0 || region.Y < 0 || region.Width <= 0 || region.Height <= 0 || region.Width > source.Width || region.Height > source.Height || region.X > source.Width-region.Width || region.Y > source.Height-region.Height || target.Width > region.Width || target.Height > region.Height || (region.Width-1)/8+1 > target.Width || (region.Height-1)/8+1 > target.Height {
		return ErrInvalid
	}
	b.prepareDraw()
	a := b.api
	a.BindTexture(0x0DE1, source.Texture)
	a.Uniform1i(b.sampler, 0)
	a.Uniform1i(b.final, -1)
	flag := int32(0)
	if linear {
		flag = 1
	}
	a.Uniform1i(b.linear, flag)
	a.Uniform2f(b.sampleSize, float32(source.Width), float32(source.Height))
	a.Uniform4f(b.captureBounds, float32(region.X)/float32(source.Width), float32(region.Y)/float32(source.Height), float32(region.Width)/float32(source.Width), float32(region.Height)/float32(source.Height))
	stepX, stepY := float32(region.Width)/float32(target.Width), float32(region.Height)/float32(target.Height)
	if stepX == 1 {
		stepX = 0
	}
	if stepY == 1 {
		stepY = 0
	}
	a.Uniform2f(b.captureStep, stepX/float32(source.Width), stepY/float32(source.Height))
	return b.device.RenderTarget(target, func() error {
		a.DrawArrays(0x0005, 0, 4)
		if code := a.GetError(); code != 0 {
			return GLError(code)
		}
		return nil
	})
}

func (b *Blur) prepareDraw() {
	a := b.api
	a.UseProgram(b.program)
	a.BindBuffer(0x8892, b.buffer)
	a.EnableVertexAttribArray(b.position)
	a.VertexAttribPointerWithOffset(b.position, 2, 0x1406, false, 0, 0)
	a.ActiveTexture(0x84C0)
	a.Disable(0x0BE2)
	a.Disable(0x0C11)
	a.Disable(0x0B44) // BLEND, SCISSOR, CULL
	a.ColorMask(true, true, true, true)
}

func (b *Blur) Close() {
	if b.program != 0 {
		b.api.DeleteProgram(b.program)
		b.program = 0
	}
	if b.buffer != 0 {
		b.api.DeleteBuffers(1, &b.buffer)
		b.buffer = 0
	}
}
func (b *Blur) Abandon() { b.program, b.buffer = 0, 0 }

const blurVertex = `attribute vec2 position;
varying vec2 uv;
void main() { uv = position * 0.5 + 0.5; gl_Position = vec4(position, 0.0, 1.0); }
`
const blurFragment = `varying vec2 uv;
uniform sampler2D source;
uniform vec2 axis, extent, corners, sampleSize;
uniform vec4 captureBounds;
uniform vec2 captureStep;
uniform int finalPass, linear;
uniform vec4 tint;
uniform float noise;
vec3 decode(vec3 c) { return mix(c/12.92, pow((c+0.055)/1.055, vec3(2.4)), step(vec3(0.04045),c)); }
vec3 encode(vec3 c) { c=max(c,vec3(0.0)); return mix(c*12.92, 1.055*pow(c,vec3(1.0/2.4))-0.055, step(vec3(0.0031308),c)); }
vec4 linearColor(vec2 p) {
    vec4 c=texture2D(source,p);
    c.rgb=decode(clamp(c.rgb/max(c.a,0.00001),0.0,1.0))*c.a;
    return c;
}
vec4 sampleColor(vec2 p) {
    if (linear!=0 && finalPass<=0) {
        vec2 grid=p*sampleSize-0.5;
        vec2 base=floor(grid), part=fract(grid);
        vec4 top=mix(linearColor((base+vec2(0.5,0.5))/sampleSize),linearColor((base+vec2(1.5,0.5))/sampleSize),part.x);
        vec4 bottom=mix(linearColor((base+vec2(0.5,1.5))/sampleSize),linearColor((base+vec2(1.5,1.5))/sampleSize),part.x);
        return mix(top,bottom,part.y);
    }
    return texture2D(source,p);
}
vec4 captureColor(vec2 p) {
    vec2 edge=0.5/sampleSize;
    return sampleColor(clamp(p,captureBounds.xy+edge,captureBounds.xy+captureBounds.zw-edge));
}
void main() {
    if (finalPass<0) {
        vec2 p=captureBounds.xy+uv*captureBounds.zw;
        vec2 d=captureStep*0.25;
        vec4 c=(captureColor(p+vec2(-d.x,-d.y))+captureColor(p+vec2(d.x,-d.y))+captureColor(p+vec2(-d.x,d.y))+captureColor(p+d))*0.25;
        if (linear!=0) c.rgb=encode(clamp(c.rgb/max(c.a,0.00001),0.0,1.0))*c.a;
        c.rgb=clamp(c.rgb,vec3(0.0),vec3(c.a));
        gl_FragColor=c;
        return;
    }
    vec4 c=(sampleColor(uv-2.0*axis)+4.0*sampleColor(uv-axis)+6.0*sampleColor(uv)+4.0*sampleColor(uv+axis)+sampleColor(uv+2.0*axis))/16.0;
    if (finalPass!=0) {
        vec3 tintRGB=tint.rgb; if (linear!=0) tintRGB=decode(tintRGB);
        c=vec4(tintRGB*tint.a,tint.a)+c*(1.0-tint.a);
        if (linear!=0) c.rgb=encode(clamp(c.rgb/max(c.a,0.00001),0.0,1.0))*c.a;
        float radius=min(corners.x,min(extent.x,extent.y)*0.5);
        vec2 q=abs((uv-0.5)*extent)-extent*0.5+radius;
        float distance=length(max(q,vec2(0.0)))+min(max(q.x,q.y),0.0)-radius;
        float mask=clamp(0.5-distance*corners.y,0.0,1.0);
        float grain=0.0;
        if (noise>0.0) { vec3 p=fract(vec3(gl_FragCoord.xyx)*0.1031); p+=dot(p,p.yzx+33.33); grain=fract((p.x+p.y)*p.z)-0.5; }
        c.rgb=clamp(c.rgb+grain*noise*c.a,vec3(0.0),vec3(c.a));
        c*=mask;
    }
    gl_FragColor=c;
}
`
