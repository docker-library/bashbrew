package manifest_test

import (
	"strings"
	"testing"

	"github.com/docker-library/go-dockerlibrary/manifest"
)

func TestParseError(t *testing.T) {
	invalidManifest := `this is just completely bogus and invalid no matter how you slice it`

	man, err := manifest.Parse(strings.NewReader(invalidManifest))
	if err == nil {
		t.Errorf("Expected error, got valid manifest instead:\n%s", man)
	}
	if !strings.HasPrefix(err.Error(), "cannot parse manifest in either format:") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestArchFile(t *testing.T) {
	tests := []struct {
		file            string
		defaultArchFile string
	}{{
		file:            "",
		defaultArchFile: "Dockerfile",
	}, {
		file:            "Dockerfile",
		defaultArchFile: "Dockerfile",
	}, {
		file:            "Dockerfile-foo",
		defaultArchFile: "Dockerfile-foo",
	},
	}

	for _, test := range tests {
		manString := `Maintainers: Giuseppe Valente <gvalente@arista.com> (@7AC)
GitCommit: abcdef
`
		if test.file != "" {
			manString += "File: " + test.file
		}
		man, err := manifest.Parse2822(strings.NewReader(manString))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if man.Global.ArchFile(manifest.DefaultArchitecture) != test.defaultArchFile {
			t.Fatalf("Unexpected arch file: %s", man.Global.ArchFile(manifest.DefaultArchitecture))
		}
	}
}
