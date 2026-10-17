// Package capabilities describes a current GL/GLES/WebGL context from explicit
// query functions. It stores no global procedure table and applies no UI policy.
package capabilities

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type API uint8

const (
	OpenGL API = iota
	OpenGLES
	WebGL
)

var ErrNoContext = errors.New("gl: no current context or empty version")
var ErrMissingQuery = errors.New("gl: missing context query function")

// Queries must refer to one current context on its owner thread. Stringi is
// needed for native GL/GLES 3+; ExtensionNames lets browser adapters provide
// getSupportedExtensions without pretending GL_EXTENSIONS is a WebGL query.
type Queries struct {
	String         func(uint32) string
	Integer        func(uint32) int32
	Stringi        func(uint32, uint32) string
	ExtensionNames func() ([]string, error)
}

type Context struct {
	API                       API
	Major, Minor              int
	Version, Vendor, Renderer string
	MaxTextureSize            int
	extensions                map[string]struct{}
}

func (c Context) AtLeast(major, minor int) bool {
	return c.Major > major || c.Major == major && c.Minor >= minor
}
func (c Context) HasExtension(name string) bool { _, found := c.extensions[name]; return found }
func (c Context) Extensions() []string {
	result := make([]string, 0, len(c.extensions))
	for name := range c.extensions {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// ParseVersion accepts driver version prefixes, not arbitrary digits in an
// error string. WebGL version numbers remain WebGL numbers (1/2), not ES 2/3.
func ParseVersion(version string) (API, int, int, error) {
	value := strings.TrimSpace(version)
	if value == "" {
		return OpenGL, 0, 0, ErrNoContext
	}
	api := OpenGL
	for _, prefix := range []struct {
		text string
		api  API
	}{{"OpenGL ES-CM ", OpenGLES}, {"OpenGL ES-CL ", OpenGLES}, {"OpenGL ES ", OpenGLES}, {"WebGL ", WebGL}, {"OpenGL ", OpenGL}} {
		if strings.HasPrefix(value, prefix.text) {
			api, value = prefix.api, strings.TrimPrefix(value, prefix.text)
			break
		}
	}
	end := 0
	for end < len(value) && (value[end] >= '0' && value[end] <= '9' || value[end] == '.') {
		end++
	}
	parts := strings.Split(value[:end], ".")
	if len(parts) < 2 {
		return api, 0, 0, fmt.Errorf("gl: invalid version %q", version)
	}
	major, a := strconv.Atoi(parts[0])
	minor, b := strconv.Atoi(parts[1])
	if a != nil || b != nil || major < 1 || minor < 0 {
		return api, 0, 0, fmt.Errorf("gl: invalid version %q", version)
	}
	return api, major, minor, nil
}

func Query(queries Queries) (Context, error) {
	if queries.String == nil || queries.Integer == nil {
		return Context{}, ErrMissingQuery
	}
	version := queries.String(0x1F02) // GL_VERSION
	api, major, minor, err := ParseVersion(version)
	if err != nil {
		return Context{}, err
	}
	context := Context{API: api, Major: major, Minor: minor, Version: version, Vendor: queries.String(0x1F00), Renderer: queries.String(0x1F01), MaxTextureSize: int(queries.Integer(0x0D33)), extensions: make(map[string]struct{})}
	if context.MaxTextureSize <= 0 {
		return Context{}, errors.New("gl: invalid maximum texture size")
	}
	var names []string
	switch {
	case queries.ExtensionNames != nil:
		names, err = queries.ExtensionNames()
		if err != nil {
			return Context{}, err
		}
	case api != WebGL && major >= 3:
		if queries.Stringi == nil {
			return Context{}, fmt.Errorf("%w: glGetStringi", ErrMissingQuery)
		}
		count := queries.Integer(0x821D) // GL_NUM_EXTENSIONS
		if count < 0 || count > 65536 {
			return Context{}, errors.New("gl: invalid extension count")
		}
		for i := uint32(0); i < uint32(count); i++ {
			names = append(names, queries.Stringi(0x1F03, i))
		}
	case api == WebGL:
		return Context{}, fmt.Errorf("%w: browser extension names", ErrMissingQuery)
	default:
		names = strings.Fields(queries.String(0x1F03))
	}
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, " \t\r\n\x00") {
			return Context{}, errors.New("gl: invalid extension name")
		}
		context.extensions[name] = struct{}{}
	}
	return context, nil
}

type FramebufferAPI uint8

const (
	NoFramebuffer FramebufferAPI = iota
	CoreFramebuffer
	EXTFramebuffer
)

// Framebuffers requires both API/extension support and all six texture-FBO
// procedures. A resolver returning non-nil stubs alone never enables support.
func (c Context) Framebuffers(available func(string) bool) FramebufferAPI {
	if available == nil {
		return NoFramebuffer
	}
	core := c.API == OpenGL && (c.AtLeast(3, 0) || c.HasExtension("GL_ARB_framebuffer_object")) || c.API == OpenGLES && c.AtLeast(2, 0) || c.API == WebGL && c.AtLeast(1, 0)
	functions := []string{"glGenFramebuffers", "glDeleteFramebuffers", "glBindFramebuffer", "glFramebufferTexture2D", "glCheckFramebufferStatus", "glGetFramebufferAttachmentParameteriv"}
	all := func(suffix string) bool {
		for _, name := range functions {
			if !available(name + suffix) {
				return false
			}
		}
		return true
	}
	if core && all("") {
		return CoreFramebuffer
	}
	if c.API == OpenGL && c.HasExtension("GL_EXT_framebuffer_object") && all("EXT") {
		return EXTFramebuffer
	}
	return NoFramebuffer
}

func (c Context) SeparateBlend(available func(string) bool) bool {
	if available == nil {
		return false
	}
	if (c.API == OpenGL && c.AtLeast(1, 4) || c.API == OpenGLES && c.AtLeast(2, 0) || c.API == WebGL && c.AtLeast(1, 0)) && available("glBlendFuncSeparate") {
		return true
	}
	return c.API == OpenGL && c.HasExtension("GL_EXT_blend_func_separate") && available("glBlendFuncSeparateEXT")
}
