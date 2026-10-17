// Package native adapts procedure addresses to portable capability queries.
// Query must run under a current GL/GLES context on its owning OS thread.
package native

/*
#include <stdint.h>
#if defined(_WIN32)
#define GOGL_ENTRY __stdcall
#else
#define GOGL_ENTRY
#endif
typedef const unsigned char* (GOGL_ENTRY *GoGetString)(uint32_t);
typedef const unsigned char* (GOGL_ENTRY *GoGetStringi)(uint32_t,uint32_t);
typedef void (GOGL_ENTRY *GoGetInteger)(uint32_t,int32_t*);
static const char* getString(void* address,uint32_t key) { return (const char*)((GoGetString)address)(key); }
static const char* getStringi(void* address,uint32_t key,uint32_t index) { return (const char*)((GoGetStringi)address)(key,index); }
static int32_t getInteger(void* address,uint32_t key) { int32_t value=0; ((GoGetInteger)address)(key,&value); return value; }
*/
import "C"

import (
	"unsafe"

	"github.com/Uzer007-admin/gl/capabilities"
)

// Query resolves only the three query procedures and creates an independent
// context snapshot. It does not replace any generated binding's global table.
func Query(resolver func(string) unsafe.Pointer) (capabilities.Context, error) {
	if resolver == nil {
		return capabilities.Context{}, capabilities.ErrMissingQuery
	}
	getString, getInteger := resolver("glGetString"), resolver("glGetIntegerv")
	if getString == nil || getInteger == nil {
		return capabilities.Context{}, capabilities.ErrMissingQuery
	}
	queries := capabilities.Queries{
		String:  func(key uint32) string { return C.GoString(C.getString(getString, C.uint32_t(key))) },
		Integer: func(key uint32) int32 { return int32(C.getInteger(getInteger, C.uint32_t(key))) },
	}
	if getStringi := resolver("glGetStringi"); getStringi != nil {
		queries.Stringi = func(key, index uint32) string {
			return C.GoString(C.getStringi(getStringi, C.uint32_t(key), C.uint32_t(index)))
		}
	}
	return capabilities.Query(queries)
}
