package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/docker-library/bashbrew/registry"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/urfave/cli"
)

func cmdRemoteArches(c *cli.Context) error {
	args := c.Args()
	if len(args) < 1 {
		return fmt.Errorf("expected at least one argument")
	}
	doJson := c.Bool("json")
	ctx := context.Background()
	for _, arg := range args {
		img, err := registry.Resolve(ctx, arg)
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", arg, err)
		}

		arches, err := img.Architectures(ctx)
		if err != nil {
			return fmt.Errorf("failed to query arches of %s: %w", arg, err)
		}

		if doJson {
			ret := struct {
				Ref    string                          `json:"ref"`
				Desc   ocispec.Descriptor              `json:"desc"`
				Arches map[string][]ocispec.Descriptor `json:"arches"`
			}{
				Ref:    img.ImageRef,
				Desc:   img.Desc,
				Arches: map[string][]ocispec.Descriptor{},
			}
			for arch, imgs := range arches {
				for _, obj := range imgs {
					ret.Arches[arch] = append(ret.Arches[arch], obj.Desc)
				}
			}
			out, err := json.Marshal(ret)
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		} else {
			fmt.Printf("%s -> %s\n", img.ImageRef, img.Desc.Digest)

			// Go.....
			keys := []string{}
			for arch := range arches {
				keys = append(keys, arch)
			}
			sort.Strings(keys)
			for _, arch := range keys {
				for _, obj := range arches[arch] {
					fmt.Printf("  %s -> %s\n", arch, obj.Desc.Digest)
				}
			}
		}
	}
	return nil
}
