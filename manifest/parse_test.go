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
		file         string
		arch         string
		expectedFile string
	}{{
		file:         "",
		arch:         manifest.DefaultArchitecture,
		expectedFile: "Dockerfile",
	}, {
		file:         "Dockerfile",
		arch:         manifest.DefaultArchitecture,
		expectedFile: "Dockerfile",
	}, {
		file:         "Dockerfile-foo",
		arch:         manifest.DefaultArchitecture,
		expectedFile: "Dockerfile-foo",
	}, {
		file:         "Dockerfile-i386",
		arch:         "i386",
		expectedFile: "Dockerfile-i386",
	},
	}

	for _, test := range tests {
		manString := `Maintainers: Giuseppe Valente <gvalente@arista.com> (@7AC)
GitCommit: abcdef
`
		if test.arch != manifest.DefaultArchitecture {
			manString += test.arch + "-"
		}
		if test.file != "" {
			manString += "File: " + test.file
		}
		man, err := manifest.Parse2822(strings.NewReader(manString))
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		actualFile := man.Global.ArchFile(test.arch)
		if actualFile != test.expectedFile {
			t.Fatalf("Unexpected arch file: %s (expected %q)", actualFile, test.expectedFile)
		}
	}
}
