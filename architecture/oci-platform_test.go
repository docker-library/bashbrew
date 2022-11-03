package architecture_test

import (
	"testing"

	"github.com/docker-library/bashbrew/architecture"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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

func TestIs(t *testing.T) {
	tests := map[bool][][2]architecture.OCIPlatform{
		true: {
			{architecture.SupportedArches["amd64"], architecture.SupportedArches["amd64"]},
			{architecture.SupportedArches["arm32v5"], architecture.SupportedArches["arm32v5"]},
			{architecture.SupportedArches["arm32v6"], architecture.SupportedArches["arm32v6"]},
			{architecture.SupportedArches["arm32v7"], architecture.SupportedArches["arm32v7"]},
			{architecture.SupportedArches["arm64v8"], architecture.OCIPlatform{OS: "linux", Architecture: "arm64", Variant: "v8"}},
			{architecture.SupportedArches["windows-amd64"], architecture.OCIPlatform{OS: "windows", Architecture: "amd64", OSVersion: "1.2.3.4"}},
		},
		false: {
			{architecture.SupportedArches["amd64"], architecture.OCIPlatform{OS: "linux", Architecture: "amd64", Variant: "v4"}},
			{architecture.SupportedArches["amd64"], architecture.SupportedArches["arm64v8"]},
			{architecture.SupportedArches["amd64"], architecture.SupportedArches["i386"]},
			{architecture.SupportedArches["amd64"], architecture.SupportedArches["windows-amd64"]},
			{architecture.SupportedArches["arm32v7"], architecture.SupportedArches["arm32v6"]},
			{architecture.SupportedArches["arm32v7"], architecture.SupportedArches["arm64v8"]},
			{architecture.SupportedArches["arm64v8"], architecture.OCIPlatform{OS: "linux", Architecture: "arm64", Variant: "v9"}},
		},
	}
	for expected, test := range tests {
		for _, platforms := range test {
			t.Run(platforms[0].String()+" vs "+platforms[1].String(), func(t *testing.T) {
				if got := platforms[0].Is(platforms[1]); got != expected {
					t.Errorf("expected %v; got %v", expected, got)
				}
			})
		}
	}
}

func TestNormalize(t *testing.T) {
	for arch, expected := range architecture.SupportedArches {
		t.Run(arch, func(t *testing.T) {
			normal := architecture.OCIPlatform(architecture.Normalize(ocispec.Platform(expected)))
			if !expected.Is(normal) {
				t.Errorf("expected %#v; got %#v", expected, normal)
			}
		})
	}
}
