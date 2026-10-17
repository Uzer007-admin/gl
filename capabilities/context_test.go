package capabilities

import (
	"errors"
	"testing"
)

func fixture(version, extensions string) Queries {
	return Queries{String: func(key uint32) string {
		if key == 0x1F02 {
			return version
		}
		if key == 0x1F03 {
			return extensions
		}
		return "fixture"
	}, Integer: func(uint32) int32 { return 4096 }}
}

func TestCapabilitiesRequireVersionExtensionsAndProcedures(t *testing.T) {
	context, err := Query(fixture("2.1 Mesa", "GL_EXT_framebuffer_object GL_EXT_framebuffer_object"))
	if err != nil || len(context.Extensions()) != 1 {
		t.Fatal(context, err)
	}
	all := func(string) bool { return true }
	if context.Framebuffers(all) != EXTFramebuffer || !context.SeparateBlend(all) {
		t.Fatal("EXT path not selected")
	}
	if context.Framebuffers(func(name string) bool { return name != "glBindFramebufferEXT" }) != NoFramebuffer {
		t.Fatal("missing procedure accepted")
	}
	context, err = Query(fixture("2.1 Mesa", ""))
	if err != nil || context.Framebuffers(all) != NoFramebuffer {
		t.Fatal("addresses alone enabled FBO")
	}
	context, err = Query(fixture("OpenGL ES 2.0 Mesa", ""))
	if err != nil || context.API != OpenGLES || context.Framebuffers(all) != CoreFramebuffer {
		t.Fatal(context, err)
	}
}

func TestIndexedAndBrowserExtensionQueries(t *testing.T) {
	queries := fixture("4.6 NVIDIA", "legacy query must not be used")
	if _, err := Query(queries); !errors.Is(err, ErrMissingQuery) {
		t.Fatal(err)
	}
	queries.Integer = func(key uint32) int32 {
		if key == 0x821D {
			return 1
		}
		return 16384
	}
	queries.Stringi = func(key, index uint32) string {
		if key != 0x1F03 || index != 0 {
			t.Fatal("invalid indexed query")
		}
		return "GL_ARB_framebuffer_object"
	}
	context, err := Query(queries)
	if err != nil || !context.HasExtension("GL_ARB_framebuffer_object") {
		t.Fatal(context, err)
	}
	queries = fixture("WebGL 1.0 (OpenGL ES 2.0 Chromium)", "")
	queries.ExtensionNames = func() ([]string, error) { return []string{"EXT_sRGB"}, nil }
	context, err = Query(queries)
	if err != nil || context.API != WebGL || context.Major != 1 || !context.HasExtension("EXT_sRGB") {
		t.Fatal(context, err)
	}
}

func TestInvalidContextMetadata(t *testing.T) {
	for _, version := range []string{"", "driver failed 4.6", "OpenGL ES x.y", "0.0"} {
		if _, _, _, err := ParseVersion(version); err == nil {
			t.Fatalf("invalid version accepted: %q", version)
		}
	}
	queries := fixture("3.3 core", "")
	queries.Stringi = func(uint32, uint32) string { return "bad name" }
	queries.Integer = func(key uint32) int32 {
		if key == 0x821D {
			return 1
		}
		return 4096
	}
	if _, err := Query(queries); err == nil {
		t.Fatal("invalid extension accepted")
	}
	queries.Integer = func(key uint32) int32 {
		if key == 0x821D {
			return -1
		}
		return 4096
	}
	if _, err := Query(queries); err == nil {
		t.Fatal("negative extension count accepted")
	}
}
