package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"os/exec"
	"path"

	"github.com/docker-library/bashbrew/registry"

	imagespec "github.com/opencontainers/image-spec/specs-go/v1"
	// thanks, go-digest...
	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/containerd/reference/docker"
	"github.com/containerd/containerd/remotes"
)

// given a reader and an interface to read it into, do the JSON decoder dance
func readJSON(r io.Reader, v interface{}) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected extra data")
	}

	return nil
}

// given an io/fs, a file reference, and an interface to read it into, read a JSON blob from the io/fs
func readJSONFile(fs iofs.FS, file string, v interface{}) error {
	f, err := fs.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	return readJSON(f, v)
}

// given a containerd content store and an OCI descriptor, parse the JSON blob
func readContentJSON(ctx context.Context, cs content.Provider, desc imagespec.Descriptor, v interface{}) error {
	ra, err := cs.ReaderAt(ctx, desc)
	if err != nil {
		return err
	}
	defer ra.Close()

	return readJSON(content.NewReader(ra), v)
}

// given a containerd content store, an io/fs reference to an "OCI image layout", and an OCI descriptor, import the given blob into the content store (with appropriate validation)
func importOCIBlob(ctx context.Context, cs content.Ingester, fs iofs.FS, descriptor imagespec.Descriptor) error {
	// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#blobs
	blob, err := fs.Open(path.Join("blobs", string(descriptor.Digest.Algorithm()), descriptor.Digest.Encoded())) // "blobs/sha256/deadbeefdeadbeefdeadbeef..."
	if err != nil {
		return err
	}
	defer blob.Close()

	// WriteBlob does *not* limit reads to the provided size, so let's wrap ourselves in a LimitedReader to prevent reading (much) more than we intend
	r := io.LimitReader(
		blob,
		descriptor.Size+1, // +1 to allow WriteBlob to detect if it reads too much
	)

	// WriteBlob verifies the digest and the size while ingesting
	return content.WriteBlob(ctx, cs, string(descriptor.Digest), r, descriptor)
}

// this is "docker build" but for "Builder: oci-import"
func ociImportBuild(tags []string, commit, dir, file string) error {
	fs, err := gitCommitFS(commit)
	if err != nil {
		return err
	}
	fs, err = iofs.Sub(fs, dir)
	if err != nil {
		return err
	}

	// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#oci-layout-file
	var layout imagespec.ImageLayout
	if err := readJSONFile(fs, "oci-layout", &layout); err != nil {
		return fmt.Errorf("failed reading oci-layout: %w", err)
	}
	if layout.Version != "1.0.0" {
		// "The imageLayoutVersion value will align with the OCI Image Specification version at the time changes to the layout are made, and will pin a given version until changes to the image layout are required."
		return fmt.Errorf("unsupported OCI image layout version %q", layout.Version)
	}

	var manifestDescriptor imagespec.Descriptor

	if file == "index.json" {
		// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#indexjson-file
		var index imagespec.Index
		if err := readJSONFile(fs, file, &index); err != nil {
			return fmt.Errorf("failed reading %q: %w", file, err)
		}
		if index.SchemaVersion != 2 {
			return fmt.Errorf("unsupported schemaVersion %d in %q", index.SchemaVersion, file)
		}
		if len(index.Manifests) != 1 {
			return fmt.Errorf("expected only one manifests entry (not %d) in %q", len(index.Manifests), file)
		}
		manifestDescriptor = index.Manifests[0]
	} else {
		if err := readJSONFile(fs, file, &manifestDescriptor); err != nil {
			return fmt.Errorf("failed reading %q: %w", file, err)
		}
	}

	if manifestDescriptor.MediaType != imagespec.MediaTypeImageManifest {
		return fmt.Errorf("unsupported mediaType %q in descriptor", manifestDescriptor.MediaType)
	}
	if err := manifestDescriptor.Digest.Validate(); err != nil {
		return fmt.Errorf("invalid digest %q: %w", manifestDescriptor.Digest, err)
	}
	if manifestDescriptor.Size < 0 {
		return fmt.Errorf("invalid size %d in descriptor", manifestDescriptor.Size)
	}
	// TODO validate Platform is either unset or matches expected value

	manifestDescriptor.URLs = nil
	manifestDescriptor.Annotations = nil

	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to containerd: %w", err)
	}
	// NO: defer client.Close()

	cs := client.ContentStore()

	if err := importOCIBlob(ctx, cs, fs, manifestDescriptor); err != nil {
		return fmt.Errorf("failed to import manifest blob (%q): %w", manifestDescriptor.Digest, err)
	}
	var manifest imagespec.Manifest
	if err := readContentJSON(ctx, cs, manifestDescriptor, &manifest); err != nil {
		return fmt.Errorf("failed to parse manifest (%q): %w", manifestDescriptor.Digest, err)
	}

	otherBlobs := append([]imagespec.Descriptor{manifest.Config}, manifest.Layers...)
	for _, blob := range otherBlobs {
		if err := importOCIBlob(ctx, cs, fs, blob); err != nil {
			return fmt.Errorf("failed to import blob (%q): %w", blob.Digest, err)
		}
	}

	is := client.ImageService()

	for _, tag := range tags {
		img := images.Image{
			Name:   tag,
			Target: manifestDescriptor,
		}
		img2, err := is.Update(ctx, img, "target") // "target" here is to specify that we want to update the descriptor that "Name" points to (if this image name already exists)
		if err != nil {
			if !errdefs.IsNotFound(err) {
				return fmt.Errorf("failed to update image %q: %w", img.Name, err)
			}
			img2, err = is.Create(ctx, img)
			if err != nil {
				return fmt.Errorf("failed to create image %q: %w", img.Name, err)
			}
		}
		img = img2 // unnecessary? :)
	}

	return nil
}

// `ctr export|docker load` "oci-import" images back into Docker (so dependent images can use them without pulling them back down)
func ociImportDockerLoad(tags []string) error {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return err
	}
	// NO: defer client.Close()

	is := client.ImageService()

	exportOpts := []archive.ExportOpt{
		archive.WithAllPlatforms(),
	}
	for _, tag := range tags {
		exportOpts = append(exportOpts, archive.WithImage(is, tag))
	}

	dockerLoad := exec.Command("docker", "load")
	dockerLoad.Stdout = os.Stdout
	dockerLoad.Stderr = os.Stderr
	dockerLoadStdin, err := dockerLoad.StdinPipe()
	if err != nil {
		return err
	}
	defer dockerLoadStdin.Close()

	err = dockerLoad.Start()
	if err != nil {
		return err
	}
	defer dockerLoad.Process.Kill()

	err = client.Export(ctx, dockerLoadStdin, exportOpts...)
	if err != nil {
		return err
	}
	dockerLoadStdin.Close()

	err = dockerLoad.Wait()
	if err != nil {
		return err
	}

	return nil
}

// given a tag, returns the OCI content descriptor in the containerd image/content store
func ociImportLookup(tag string) (*imagespec.Descriptor, error) {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return nil, err
	}
	// NO: defer client.Close()

	is := client.ImageService()

	img, err := is.Get(ctx, tag)
	if err != nil {
		return nil, err
	}

	return &img.Target, nil
}

// given a descriptor and a list of tags (intended for pushing), return the set of those that are up-to-date (`skip`) and those that need-update (`update`)
func ociImportPushFilter(desc imagespec.Descriptor, destinationTags []string) (skip, update []string, err error) {
	ctx := context.Background()

	for _, tag := range destinationTags {
		obj, err := registry.Resolve(ctx, tag)
		if err != nil {
			if errdefs.IsNotFound(err) {
				update = append(update, tag)
				continue
			}
			return nil, nil, err
		}

		if obj.Desc.MediaType == desc.MediaType && obj.Desc.Digest == desc.Digest && obj.Desc.Size == desc.Size {
			skip = append(skip, tag)
		} else {
			update = append(update, tag)
		}
	}

	return skip, update, nil
}

// given a descriptor and a list of tags, push the content from containerd's content store to the appropriate registry
func ociImportPush(desc imagespec.Descriptor, destinationTags []string) error {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return err
	}
	// NO: defer client.Close()

	cs := client.ContentStore()

	resolver := registry.NewDockerAuthResolver()

	for _, tag := range destinationTags {
		ref, err := docker.ParseAnyReference(tag)
		if err != nil {
			return err
		}

		pusher, err := resolver.Pusher(ctx, ref.String())
		if err != nil {
			return err
		}

		// add the "tag" annotation to our descriptor so that containerd's pusher code does the right thing and pushes *every* tag (even though our Pusher is scoped to this tag, without this it will "cleverly" avoid pushing tag 2+ because the digest we're pushing already exists in the repository)
		desc := desc
		desc.Annotations = map[string]string{imagespec.AnnotationRefName: ref.String()}

		err = remotes.PushContent(ctx, pusher, desc, cs, nil, nil, nil)
		if err != nil {
			return err
		}
	}

	return nil
}
