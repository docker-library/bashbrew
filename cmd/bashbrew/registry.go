package main

import (
	"context"

	"github.com/docker-library/bashbrew/registry"
)

var registryImageIdsCache = map[string][]string{}

// assumes the provided image name is NOT a manifest list (used for testing whether we need to "bashbrew push" or whether the remote image is already up-to-date)
// this does NOT handle authentication, and will return the empty string for repositories which require it (causing "bashbrew push" to simply shell out to "docker push" which will handle authentication appropriately)
func fetchRegistryImageIds(image string) []string {
	ctx := context.Background()

	img, err := registry.Resolve(ctx, image)
	if err != nil {
		return nil
	}

	digest := img.Desc.Digest.String()
	if ids, ok := registryImageIdsCache[digest]; ok {
		return ids
	}

	manifests, err := img.Manifests(ctx)
	if err != nil {
		return nil
	}

	ids := []string{}
	if img.IsImageIndex() {
		ids = append(ids, digest)
	}
	for _, manifestDesc := range manifests {
		ids = append(ids, manifestDesc.Digest.String())
		manifest, err := img.Manifest(ctx, manifestDesc)
		if err != nil {
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
		return nil
	}

	digest := img.Desc.Digest.String()
	if digests, ok := registryManifestListCache[digest]; ok {
		return digests
	}

	manifests, err := img.Manifests(ctx)
	if err != nil {
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
