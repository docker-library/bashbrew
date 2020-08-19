package templatelib_test

import (
	"strings"
	"testing"
	"text/template"
	"unsafe"

	"github.com/docker-library/bashbrew/pkg/templatelib"
)

func TestTernaryPanic(t *testing.T) {
	// one of the only places template.IsTrue will return "false" for the "ok" value is an UnsafePointer (hence this test)

	tmpl, err := template.New("unsafe-pointer").Funcs(templatelib.FuncMap).Parse(`{{ ternary "true" "false" . }}`)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	err = tmpl.Execute(nil, unsafe.Pointer(uintptr(0)))
	if err == nil {
		t.Errorf("Expected error, executed successfully instead")
	}
	if !strings.HasSuffix(err.Error(), `template.IsTrue(<nil>) says things are NOT OK`) {
		t.Errorf("Expected specific error, got: %v", err)
	}
}

func TestJoinPanic(t *testing.T) {
	tmpl, err := template.New("join-no-arg").Funcs(templatelib.FuncMap).Parse(`{{ join }}`)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	err = tmpl.Execute(nil, nil)
	if err == nil {
		t.Errorf("Expected error, executed successfully instead")
	}
	if !strings.HasSuffix(err.Error(), `"join" requires at least one argument`) {
		t.Errorf("Expected specific error, got: %v", err)
	}
}
