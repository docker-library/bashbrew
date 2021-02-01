package manifest_test

import (
	"strings"
	"testing"

	"github.com/docker-library/bashbrew/manifest"
)

func TestParseError(t *testing.T) {
	invalidManifest := `this is just completely bogus and invalid no matter how you slice it`

	man, err := manifest.Parse(strings.NewReader(invalidManifest))
	if err == nil {
		t.Errorf("Expected error, got valid manifest instead:\n%s", man)
		return
	}
	if !strings.HasPrefix(err.Error(), "Bad line:") {
		t.Errorf("Unexpected error: %v", err)
		return
	}
}

func TestInvalidMaintainer(t *testing.T) {
	testManifest := `Maintainers: Valid Name (@valid-handle), Valid Name <valid-email> (@valid-handle), Invalid Maintainer (invalid-handle)`

	man, err := manifest.Parse(strings.NewReader(testManifest))
	if err == nil {
		t.Errorf("Expected error, got valid manifest instead:\n%s", man)
		return
	}
	if !strings.HasPrefix(err.Error(), "invalid Maintainers:") {
		t.Errorf("Unexpected error: %v", err)
		return
	}
}
