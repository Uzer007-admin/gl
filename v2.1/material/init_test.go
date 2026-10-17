package gl

import (
	"encoding/json"
	"os"
	"testing"
	"unsafe"
)

func TestMaterialExtensionsRemainOptional(t *testing.T) {
	data, err := os.ReadFile("symbol_manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Functions []struct {
			Name     string
			Required bool
		}
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	required := make(map[string]bool)
	for _, function := range manifest.Functions {
		required[function.Name] = function.Required
	}
	if required["glGenFramebuffers"] || required["glGenFramebuffersEXT"] || !required["glBlendFuncSeparate"] {
		t.Fatal("material profile changed GL 2.1 baseline")
	}
	address := unsafe.Pointer(unsafe.StringData("unused-procedure-address"))
	if err := InitWithProcAddrFunc(func(name string) unsafe.Pointer {
		if required[name] {
			return address
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if IsFunctionAvailable("glGenFramebuffers") || !IsFunctionAvailable("glBlendFuncSeparate") {
		t.Fatal("optional procedure availability incorrect")
	}
}
