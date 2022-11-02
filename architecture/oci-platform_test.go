package architecture_test

import (
	"testing"

	"github.com/docker-library/bashbrew/architecture"
)

func TestString(t *testing.T) {
	tests := map[string]string{
		"amd64":         "linux/amd64",
		"arm32v6":       "linux/arm/v6",
		"windows-amd64": "windows/amd64",
	}
	for arch, platform := range tests {
		t.Run(arch, func(t *testing.T) {
			oci := architecture.SupportedArches[arch]
			if ociPlatform := oci.String(); platform != ociPlatform {
				t.Errorf("expected %q; got %q", platform, ociPlatform)
			}
		})
	}
}
