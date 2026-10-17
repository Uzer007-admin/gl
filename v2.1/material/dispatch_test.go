package gl

import (
	"sync"
	"testing"
	"unsafe"
)

func TestIndependentFunctionTables(t *testing.T) {
	var mock byte
	resolve := func(string) unsafe.Pointer { return unsafe.Pointer(&mock) }
	a, err := LoadWithProcAddrFunc(resolve)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadWithProcAddrFunc(func(name string) unsafe.Pointer {
		if name == "glBindFramebufferEXT" {
			return nil
		}
		return resolve(name)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsFunctionAvailable("glBindFramebufferEXT") || b.IsFunctionAvailable("glBindFramebufferEXT") {
		t.Fatal("table changed while loading another context")
	}
	if _, err := LoadWithProcAddrFunc(func(string) unsafe.Pointer { return nil }); err == nil {
		t.Fatal("required procedure missing")
	}
	if !a.IsFunctionAvailable("glClear") || !b.IsFunctionAvailable("glClear") {
		t.Fatal("failed load changed old tables")
	}
	if f, err := LoadWithProcAddrFunc(nil); err == nil || f != nil {
		t.Fatal("nil resolver accepted")
	}
	var nilTable *Functions
	if nilTable.IsFunctionAvailable("glClear") || a.IsFunctionAvailable("glTypo") {
		t.Fatal("invalid availability")
	}
	// Availability is read-only. This test never calls the mock addresses.
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 1000 {
				if !a.IsFunctionAvailable("glClear") || b.IsFunctionAvailable("glBindFramebufferEXT") {
					t.Error("mutated table")
				}
			}
		})
	}
	group.Wait()
}
