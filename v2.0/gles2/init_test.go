package gles2

import (
	"strings"
	"testing"
	"unsafe"
)

func TestCustomResolverReceivesUnterminatedNames(t *testing.T) {
	count := 0
	pointer := unsafe.Pointer(unsafe.StringData("unused-procedure-address"))
	if err := InitWithProcAddrFunc(func(name string) unsafe.Pointer {
		count++
		if strings.ContainsRune(name, 0) || !strings.HasPrefix(name, "gl") {
			t.Fatalf("invalid custom resolver name %q", name)
		}
		return pointer
	}); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no functions resolved")
	}
}

func TestNativeResolverNamesHaveExactlyOneTerminator(t *testing.T) {
	count := 0
	pointer := unsafe.Pointer(unsafe.StringData("unused-procedure-address"))
	if err := initWithProcAddrFunc(func(name string) unsafe.Pointer {
		count++
		if !strings.HasSuffix(name, "\x00") || strings.ContainsRune(name[:len(name)-1], 0) {
			t.Fatalf("invalid native resolver name %q", name)
		}
		return pointer
	}, true); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no functions resolved")
	}
}

func TestFailedReloadClearsPreviouslyLoadedFunctions(t *testing.T) {
	pointer := unsafe.Pointer(unsafe.StringData("unused-procedure-address"))
	if err := InitWithProcAddrFunc(func(string) unsafe.Pointer { return pointer }); err != nil {
		t.Fatal(err)
	}
	if !IsFunctionAvailable("glViewport") || IsFunctionAvailable("Viewport") || IsFunctionAvailable("glUnknownProcedure") {
		t.Fatal("availability query does not match raw GL procedure names")
	}
	lookups := 0
	err := InitWithProcAddrFunc(func(string) unsafe.Pointer { lookups++; return nil })
	if err == nil || IsFunctionAvailable("glViewport") {
		t.Fatal("failed reload retained a stale procedure address")
	}
	if lookups < 2 {
		t.Fatal("reload stopped before resetting the procedure table")
	}
}
