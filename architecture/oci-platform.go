package architecture

import "path"

// https://github.com/opencontainers/image-spec/blob/v1.0.1/image-index.md#image-index-property-descriptions
// see "platform" (under "manifests")
type OCIPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`

	//OSVersion  string   `json:"os.version,omitempty"`
	//OSFeatures []string `json:"os.features,omitempty"`
}

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
