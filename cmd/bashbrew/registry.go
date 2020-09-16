package main

import (
	"context"
	"encoding/json"

	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes"
	dockerremote "github.com/containerd/containerd/remotes/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var registryImageIdCache = map[string]string{}

// assumes the provided image name is NOT a manifest list (used for testing whether we need to "bashbrew push" or whether the remote image is already up-to-date)
// this does NOT handle authentication, and will return the empty string for repositories which require it (causing "bashbrew push" to simply shell out to "docker push" which will handle authentication appropriately)
func fetchRegistryImageId(image string) string {
	ctx := context.Background()

	ref, resolver, err := fetchRegistryResolveHelper(image)
	if err != nil {
		return ""
	}

	name, desc, err := resolver.Resolve(ctx, ref)
	if err != nil {
		return ""
	}

	if desc.MediaType != images.MediaTypeDockerSchema2Manifest && desc.MediaType != ocispec.MediaTypeImageManifest {
		return ""
	}

	digest := desc.Digest.String()
	if id, ok := registryImageIdCache[digest]; ok {
		return id
	}

	fetcher, err := resolver.Fetcher(ctx, name)
	if err != nil {
		return ""
	}

	r, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return ""
	}
	defer r.Close()

	var manifest ocispec.Manifest
	if err := json.NewDecoder(r).Decode(&manifest); err != nil {
		return ""
	}
	id := manifest.Config.Digest.String()
	if id != "" {
		registryImageIdCache[digest] = id
	}
	return id
}

var registryManifestListCache = map[string][]string{}

// returns a list of manifest list element digests for the given image name (which might be just one entry, if it's not a manifest list)
func fetchRegistryManiestListDigests(image string) []string {
	ctx := context.Background()

	ref, resolver, err := fetchRegistryResolveHelper(image)
	if err != nil {
		return nil
	}

	name, desc, err := resolver.Resolve(ctx, ref)
	if err != nil {
		return nil
	}

	digest := desc.Digest.String()
	if desc.MediaType == images.MediaTypeDockerSchema2Manifest || desc.MediaType == ocispec.MediaTypeImageManifest {
		return []string{digest}
	}

	if desc.MediaType != images.MediaTypeDockerSchema2ManifestList && desc.MediaType != ocispec.MediaTypeImageIndex {
		return nil
	}

	if digests, ok := registryManifestListCache[digest]; ok {
		return digests
	}

	fetcher, err := resolver.Fetcher(ctx, name)
	if err != nil {
		return nil
	}

	r, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil
	}
	defer r.Close()

	var manifestList ocispec.Index
	if err := json.NewDecoder(r).Decode(&manifestList); err != nil {
		return nil
	}
	digests := []string{}
	for _, manifest := range manifestList.Manifests {
		if manifest.Digest != "" {
			digests = append(digests, manifest.Digest.String())
		}
	}
	if len(digests) > 0 {
		registryManifestListCache[digest] = digests
	}
	return digests
}

func fetchRegistryResolveHelper(image string) (string, remotes.Resolver, error) {
	ref, err := docker.ParseAnyReference(image)
	if err != nil {
		return "", nil, err
	}
	if namedRef, ok := ref.(docker.Named); ok {
		// add ":latest" if necessary
		namedRef = docker.TagNameOnly(namedRef)
		ref = namedRef
	}
	return ref.String(), dockerremote.NewResolver(dockerremote.ResolverOptions{}), nil
}
