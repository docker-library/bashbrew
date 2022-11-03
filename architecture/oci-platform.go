package architecture

import (
	"path"

	"github.com/containerd/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// https://github.com/opencontainers/image-spec/blob/v1.0.1/image-index.md#image-index-property-descriptions
// see "platform" (under "manifests")
type OCIPlatform ocispec.Platform

var SupportedArches = map[string]OCIPlatform{
	"amd64":    {OS: "linux", Architecture: "amd64"},
	"arm32v5":  {OS: "linux", Architecture: "arm", Variant: "v5"},
	"arm32v6":  {OS: "linux", Architecture: "arm", Variant: "v6"},
	"arm32v7":  {OS: "linux", Architecture: "arm", Variant: "v7"},
	"arm64v8":  {OS: "linux", Architecture: "arm64", Variant: "v8"},
	"i386":     {OS: "linux", Architecture: "386"},
	"mips64le": {OS: "linux", Architecture: "mips64le"},
	"ppc64le":  {OS: "linux", Architecture: "ppc64le"},
	"riscv64":  {OS: "linux", Architecture: "riscv64"},
	"s390x":    {OS: "linux", Architecture: "s390x"},

	"windows-amd64": {OS: "windows", Architecture: "amd64"},
}

// https://pkg.go.dev/github.com/containerd/containerd/platforms
func (p OCIPlatform) String() string {
	return path.Join(
		p.OS,
		p.Architecture,
		p.Variant,
	)
}

func Normalize(p ocispec.Platform) ocispec.Platform {
	p = platforms.Normalize(p)
	if p.Architecture == "arm64" && p.Variant == "" {
		// 😭 https://github.com/containerd/containerd/blob/1c90a442489720eec95342e1789ee8a5e1b9536f/platforms/database.go#L98 (inconsistent normalization of "linux/arm -> linux/arm/v7" vs "linux/arm64/v8 -> linux/arm64")
		p.Variant = "v8"
		// TODO get pedantic about amd64 variants too? (in our defense, those variants didn't exist when we defined our "amd64", unlike "arm64v8" 👀)
	}
	return p
}

func (p OCIPlatform) Is(q OCIPlatform) bool {
	// (assumes "p" and "q" are both already bashbrew normalized, like one of the SupportedArches above)
	return p.OS == q.OS && p.Architecture == q.Architecture && p.Variant == q.Variant
}
