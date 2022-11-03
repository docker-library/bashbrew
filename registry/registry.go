package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"unicode"

	// thanks, go-digest...
	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes"
	dockerremote "github.com/containerd/containerd/remotes/docker"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type ResolvedObject struct {
	Ref  string
	Desc ocispec.Descriptor

	resolver remotes.Resolver
	fetcher  remotes.Fetcher
}

func (obj ResolvedObject) FetchJSON(ctx context.Context, v interface{}) error {
	// prevent go-digest panics later
	if err := obj.Desc.Digest.Validate(); err != nil {
		return err
	}

	r, err := obj.fetcher.Fetch(ctx, obj.Desc)
	if err != nil {
		return err
	}
	defer r.Close()

	// make sure we can't possibly read (much) more than we're supposed to
	limited := &io.LimitedReader{
		R: r,
		N: obj.Desc.Size + 1, // +1 to allow us to detect if we read too much (see verification below)
	}

	// copy all read data into the digest verifier so we can validate afterwards
	verifier := obj.Desc.Digest.Verifier()
	tee := io.TeeReader(limited, verifier)

	// decode directly! (mostly avoids double memory hit for big objects)
	// (TODO protect against malicious objects somehow?)
	if err := json.NewDecoder(tee).Decode(v); err != nil {
		return err
	}

	// read anything leftover ...
	bs, err := io.ReadAll(tee)
	if err != nil {
		return err
	}
	// ... and make sure it was just whitespace, if anything
	for _, b := range bs {
		if !unicode.IsSpace(rune(b)) {
			return fmt.Errorf("unexpected non-whitespace at the end of %q: %+v\n", obj.Desc.Digest.String(), rune(b))
		}
	}

	// after reading *everything*, we should have exactly one byte left in our LimitedReader (anything else is an error)
	if limited.N < 1 {
		return fmt.Errorf("size of %q is bigger than it should be (%d)", obj.Desc.Digest.String(), obj.Desc.Size)
	} else if limited.N > 1 {
		return fmt.Errorf("size of %q is %d bytes smaller than it should be (%d)", obj.Desc.Digest.String(), limited.N-1, obj.Desc.Size)
	}

	// and finally, let's verify our checksum
	if !verifier.Verified() {
		return fmt.Errorf("digest of %q not correct", obj.Desc.Digest.String())
	}

	return nil
}

func (obj ResolvedObject) Manifests(ctx context.Context) ([]ocispec.Descriptor, error) {
	if obj.IsImageManifest() {
		return []ocispec.Descriptor{obj.Desc}, nil
	}

	if !obj.IsImageIndex() {
		return nil, fmt.Errorf("unknown media type: %q", obj.Desc.MediaType)
	}

	// (perhaps use a containerd content store??)
	var index ocispec.Index
	if err := obj.FetchJSON(ctx, &index); err != nil {
		return nil, err
	}
	return index.Manifests, nil
}

func (obj ResolvedObject) Manifest(ctx context.Context, desc ocispec.Descriptor) (*ocispec.Manifest, error) {
	obj.Desc = desc // swap our object to point at this manifest
	if !obj.IsImageManifest() {
		return nil, fmt.Errorf("unknown media type: %q", obj.Desc.MediaType)
	}

	// (perhaps use a containerd content store??)
	var manifest ocispec.Manifest
	if err := obj.FetchJSON(ctx, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (obj ResolvedObject) IsImageManifest() bool {
	return obj.Desc.MediaType == ocispec.MediaTypeImageManifest || obj.Desc.MediaType == images.MediaTypeDockerSchema2Manifest
}

func (obj ResolvedObject) IsImageIndex() bool {
	return obj.Desc.MediaType == ocispec.MediaTypeImageIndex || obj.Desc.MediaType == images.MediaTypeDockerSchema2ManifestList
}

func Resolve(ctx context.Context, image string) (*ResolvedObject, error) {
	var (
		obj = ResolvedObject{
			Ref: image,
		}
		err error
	)

	obj.Ref, obj.resolver, err = resolverHelper(obj.Ref)
	if err != nil {
		return nil, err
	}

	obj.Ref, obj.Desc, err = obj.resolver.Resolve(ctx, obj.Ref)
	if err != nil {
		return nil, err
	}

	obj.fetcher, err = obj.resolver.Fetcher(ctx, obj.Ref)
	if err != nil {
		return nil, err
	}

	return &obj, nil
}

func resolverHelper(image string) (string, remotes.Resolver, error) {
	ref, err := docker.ParseAnyReference(image)
	if err != nil {
		return "", nil, err
	}
	if namedRef, ok := ref.(docker.Named); ok {
		// add ":latest" if necessary
		namedRef = docker.TagNameOnly(namedRef)
		ref = namedRef
	}
	return ref.String(), dockerremote.NewResolver(dockerremote.ResolverOptions{
		// TODO port this to "Hosts:" (especially so we can return Scheme correctly) but requires reimplementing some of https://github.com/containerd/containerd/blob/v1.6.9/remotes/docker/resolver.go#L161-L184 😞
		Host: func(host string) (string, error) {
			if host == "docker.io" {
				if publicProxy := os.Getenv("DOCKERHUB_PUBLIC_PROXY"); publicProxy != "" {
					if publicProxyURL, err := url.Parse(publicProxy); err == nil {
						// TODO Scheme (also not sure if "host:port" will be satisfactory to containerd here, but 🤷)
						return publicProxyURL.Host, nil
					} else {
						return "", err
					}
				}
				return "registry-1.docker.io", nil // https://github.com/containerd/containerd/blob/1c90a442489720eec95342e1789ee8a5e1b9536f/remotes/docker/registry.go#L193
			}
			return host, nil
		},
	}), nil
}
