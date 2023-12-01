package main

import (
	"context"
	"fmt"
	"os"

	"github.com/docker-library/bashbrew/registry"
)

var registryImageIdsCache = map[string][]string{}

// assumes the provided image name is NOT a manifest list (used for testing whether we need to "bashbrew push" or whether the remote image is already up-to-date)
// this does NOT handle authentication, and will return the empty string for repositories which require it (causing "bashbrew push" to simply shell out to "docker push" which will handle authentication appropriately)
func fetchRegistryImageIds(image string) []string {
	ctx := context.Background()

	img, err := registry.Resolve(ctx, image)
	if err != nil {
		if debugFlag {
			fmt.Fprintf(os.Stderr, "DEBUG: registry.Resolve(%q) => %v\n", image, err)
		}
		return nil
	}

	digest := img.Desc.Digest.String()
	if ids, ok := registryImageIdsCache[digest]; ok {
		return ids
	}

	ids := []string{}
	if img.IsImageIndex() {
		ids = append(ids, digest)
		return ids // see note above -- this function is used for "docker push" which does not and cannot (currently) support a manifest list / image index
	}

	manifests, err := img.Manifests(ctx)
	if err != nil {
		if debugFlag {
			fmt.Fprintf(os.Stderr, "DEBUG: img.Manifests (%q) => %v\n", image, err)
		}
		return nil
	}

	// TODO balk if manifests has more than one entry in it

	for _, manifestDesc := range manifests {
		ids = append(ids, manifestDesc.Digest.String())
		manifest, err := img.At(manifestDesc).Manifest(ctx)
		if err != nil {
			if debugFlag {
				fmt.Fprintf(os.Stderr, "DEBUG: img.Manifest (%q, %q) => %v\n", image, manifestDesc.Digest.String(), err)
			}
			continue
		}
		ids = append(ids, manifest.Config.Digest.String())
	}
	if len(ids) > 0 {
		registryImageIdsCache[digest] = ids
	}
	return ids
}

var registryManifestListCache = map[string][]string{}

// returns a list of manifest list element digests for the given image name (which might be just one entry, if it's not a manifest list)
func fetchRegistryManiestListDigests(image string) []string {
	ctx := context.Background()

	img, err := registry.Resolve(ctx, image)
	if err != nil {
		if debugFlag {
			fmt.Fprintf(os.Stderr, "DEBUG: registry.Resolve(%q) => %v\n", image, err)
		}
		return nil
	}

	digest := img.Desc.Digest.String()
	if digests, ok := registryManifestListCache[digest]; ok {
		return digests
	}

	manifests, err := img.Manifests(ctx)
	if err != nil {
		if debugFlag {
			fmt.Fprintf(os.Stderr, "DEBUG: img.Manifests (%q) => %v\n", image, err)
		}
		return nil
	}
	digests := []string{}
	for _, manifest := range manifests {
		if manifest.Digest != "" {
			digests = append(digests, manifest.Digest.String())
		}
	}
	if len(digests) > 0 {
		registryManifestListCache[digest] = digests
	}
	return digests
}
