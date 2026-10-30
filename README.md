# gl: библиотека проекта Granio

**Исходный репозиторий:** [go-gl/gl](https://github.com/go-gl/gl).
**Цель форка:** Обёртки OpenGL/GLES, загрузка функций и диагностика графического контекста. Изменения генерации согласованы с Glow.

Репозиторий развивается самостоятельно. Исходная история, лицензия и авторство
сохранены. Минимальная версия Go — **1.27.1**. Собственный адрес модуля:
`github.com/Uzer007-admin/gl`.

Для безопасного чтения RGBA8 добавлен пакет [`readback`](readback/README.md).
Он проверяет размеры и объём буфера, временно сбрасывает pixel-pack layout/PBO
и восстанавливает состояние после чтения, ошибки или panic. Поддержка параметров
проверяется по фактической версии GL/GLES и расширениям. Интеграция в Granio,
причина пустого кадра 0×0 и результаты Windows/X11/Wayland описаны в
[отчёте](https://github.com/Uzer007-admin/granio/blob/dev/granio/docs/GL_CAPTURE_REPAIR.md).

Платформенные ограничения и результаты проверок:
[развитие графического стека](https://github.com/Uzer007-admin/granio/blob/dev/granio/docs/NATIVE_STACK_IMPLEMENTATION.md).
Длительные проверки производительности в этот этап не входят.

---

## Документация исходного проекта и прежних этапов

Адреса установки в сохранённых примерах ниже могут относиться к исходнику.

# gl [![Build Status](https://github.com/go-gl/gl/actions/workflows/main.yml/badge.svg)](https://github.com/go-gl/gl/actions/workflows/main.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/go-gl/gl.svg)](https://pkg.go.dev/github.com/go-gl/gl)

This repository holds Go bindings to various OpenGL versions. They are auto-generated using [Glow](https://github.com/go-gl/glow).

Features:
- Go functions that mirror the C specification using Go types.
- Support for multiple OpenGL APIs (GL/GLES/EGL/WGL/GLX/EGL), versions, and profiles.
- Support for extensions (including debug callbacks).
- `IsFunctionAvailable("glFunctionName")` reports a loaded procedure address.
  It does not establish API version, extension support, or OS compositor support.
  Query it on the context thread after `Init`; initialization and rendering must
  not race. Reinitialization resolves the entire table, clearing stale addresses
  even when a required function is missing and initialization returns an error.
- Each generated profile includes a deterministic `symbol_manifest.json` with
  its API/version/profile, enum names and required/optional procedure names.

The optional `v2.1/material` profile adds separate alpha blending, texture FBO
and renderbuffer entry points (core/ARB and EXT names): 79 procedures, including
pixel-store state for safe readback. FBO entry points remain optional, preserving
the GL 2.1 baseline. The existing 51-procedure
Fyne profile is unchanged and is still the renderer's default.

`capabilities` provides context/version/extension snapshots with no global
procedure state; `capabilities/native.Query(glfw.GetProcAddress)` resolves only
the three query functions on the current context thread. `Framebuffers` and
`SeparateBlend` require both advertised support and loaded procedures. Browser
adapters can use pure-Go `Queries.ExtensionNames`; this package is separate from
the cgo native query adapter. OS backdrop support is outside these GL capabilities.

Requirements:
- A cgo compiler (typically gcc).
- On Ubuntu/Debian-based systems, the `libgl1-mesa-dev` package.

## Usage

Use `go get -u` to download and install the prebuilt packages. The prebuilt packages support OpenGL versions 2.1, 3.1, 3.2, 3.3, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6 across both the core and compatibility profiles and include all extensions. Pick whichever one(s) you need:

    go get -u github.com/go-gl/gl/v{3.2,3.3,4.1,4.2,4.3,4.4,4.5,4.6}-{core,compatibility}/gl
    go get -u github.com/go-gl/gl/v3.1/gles2
    go get -u github.com/go-gl/gl/v2.1/gl

Once the bindings are installed you can use them with the appropriate import statements.

```Go
package main

import "github.com/go-gl/gl/v3.3-core/gl"

func main() {
	window := ... // Open a window.
	window.MakeContextCurrent()

	// Important! Call gl.Init only under the presence of an active OpenGL context,
	// i.e., after MakeContextCurrent.
	if err := gl.Init(); err != nil {
		log.Fatalln(err)
	}
}
```

The `gl` package contains the OpenGL functions and enumeration values for the imported version. It also contains helper functions for working with the API. Of note is `gl.Ptr` which takes a Go array or slice or pointer and returns a corresponding `uintptr` to use with functions expecting data pointers. Also of note is `gl.Str` which takes a null-terminated Go string and returns a corresponding `*int8` to use with functions expecting character pointers.

A note about threading and goroutines. The bindings do not expose a mechanism to make an OpenGL context current on a different thread so you must restrict your usage to the thread on which you called `gl.Init()`. To do so you should use [LockOSThread](https://code.google.com/p/go-wiki/wiki/LockOSThread).

## Examples

Examples illustrating how to use the bindings are available in the [example](https://github.com/go-gl/example) repo. There are examples for [OpenGL 4.1 core](https://github.com/go-gl/example/tree/master/gl41core-cube) and [OpenGL 2.1](https://github.com/go-gl/example/tree/master/gl21-cube).

## Function Loading

The `procaddr` package contains platform-specific functions for [loading OpenGL functions](https://www.opengl.org/wiki/Load_OpenGL_Functions). Calling `gl.Init()` uses the `auto` subpackage to automatically select an appropriate implementation based on the build environment. If you want to select a specific implementation you can use the `noauto` build tag and the `gl.InitWithProcAddrFunc` initialization function.

## Go >=1.14 and `checkptr`

In version 1.14 of Go, the race detector added `checkptr` instrumentation. This compilation option ensures that programs follow `unsafe.Pointer` safety rules. See here for details: https://golang.org/doc/go1.14#compiler.

If enabled, there is a high chance that it will cause program termination when calling specific OpenGL functions, with a message like this:

```
fatal error: checkptr: pointer arithmetic computed bad pointer value
```

The reported call stack will point to a function like `gl.VertexAttribPointer()` that receives an `unsafe.Pointer` as parameter.
In case such function requires an "offset" passed in as a pointer (in the low-level API), a different signature needs to be used in order to satisfy the detector.

For this purpose `glow` generates "override" functions which have a different signature, taking `uintptr` instead. These functions have the suffix `WithOffset` (or similar) in their name.
For the previous example, it would be `gl.VertexAttribPointerWithOffset()`.

Not all such functions have an appropriate override! In case you stumble over such an error, and the override is missing, you have the following options:
* Disable the detector by building your program with `-gcflags=all=-d=checkptr=0`
* Report the missing function(s) as [issue for `glow`](https://github.com/go-gl/glow/issues)
* Possibly even create a pull-request for `glow` with the missing override yourself, and re-generate the `gl` bindings.

## Generating

### Compact Fyne profile

`github.com/go-gl/gl/v2.1/fyne` (package `gl`) contains the symbols required by
Fyne's native renderer plus renderer/version and shader diagnostics. The complete
`v2.1/gl` package remains available. The compact profile currently loads 51
functions, compared with 2,848 in the complete profile.

In the enclosing Fyne workspace, run `python scripts/update-fyne-gl-profile.py`
after changing the renderer's GL calls, then regenerate. The checked-in restriction
is `profiles/fyne-gl21.json`; glow validates every requested symbol.

Native `Init` uses generated NUL-terminated names directly without per-symbol
C allocation. `InitWithProcAddrFunc` continues to pass ordinary Go names without
NUL terminators. Windows loaders reject WGL failure sentinels before falling back
to `opengl32.dll`. The context/thread requirements of `Init` still apply.

The compact, material and strict GLES 2 profiles also expose `Load()` and
`LoadWithProcAddrFunc()` returning immutable `*Functions` tables. They do not
modify legacy globals. Methods and offset overloads use that table; a failed
global reload or another window's table cannot replace its addresses. Load on
the matching current context and discard/reload on recreation. Tables do not
make GL calls thread-safe or take ownership of a context.

### Material resources and GPU passes

`github.com/go-gl/gl/material` is a portable resource/cache layer. `NewGLDevice`
accepts the matching `capabilities.Context` and a material/GLES `Functions` table,
selects supported core/ARB/EXT FBO entry points, creates complete RGBA8 texture
targets and preserves texture, pixel-unpack-buffer and separate read/draw FBO
bindings. `RenderTarget` restores FBO/viewport even on errors/panics and reuses
query storage. It does not preserve other compositor-owned draw state.
`CopySnapshot` copies a lower-layer region from the current read framebuffer to
a target texture on the GPU, restores texture binding and rejects self-feedback.
It uses bottom-left physical coordinates and does not rescale or capture DWM.

`NewPool` defaults to 64 MiB of requested RGBA8 texel storage and two layers,
each with two ping-pong targets. It checks old+new resize peak before allocation,
rolls back partial creation and caches by source/style/surface generations,
both scales and effect parameters. `Update` does no GL work on a cache hit.
Driver bookkeeping, programs, window framebuffers and other caches are outside
this texel budget. `Close` deletes while the matching context is current;
`Abandon` forgets handles after context loss without deleting into a new context.

`NewBlur` owns a shader/VBO and performs two GPU passes: five-tap separable
Gaussian, rounded mask, tint and deterministic noise. The source must be a fresh
snapshot of lower canvas layers in premultiplied RGBA; no desktop capture or
per-frame CPU readback is performed. Optional linear-light filtering decodes
before bilinear interpolation and encodes final sRGB output; the intermediate
RGBA8 format has finite precision. Radius/corners are logical units and both
axes are scaled independently. A compositor owns/rebinds its draw state after
`Apply`; GL 2.1 compatibility/GLES 2 are the tested baselines. WebGL requires an
adapter and runtime verification; desktop core profiles require appropriate
vertex-array/rasterization state from their compositor.

### All profiles

These gl bindings are generated using the [Glow](https://github.com/go-gl/glow) generator. Only developers of this repository need to do this step.

It is required to have `glow` source in a sibling directory to `go-gl/gl` since relative paths are used for generation (see `generate.go`).
For non-module-aware cases, this means `glow` needs to be in the same Go workspace as `go-gl/gl`.
For module-aware cases, `go-gl/glow` needs to be checked out parallel to `go-gl/gl`. 

Perform generation with the following:

```bash
cd path/to/go-gl/gl
go generate -tags=gen .
```

More information about these bindings can be found in the [Glow repository](https://github.com/go-gl/glow).
