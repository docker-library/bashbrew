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

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/containerd/platforms"
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
func importOCIBlob(ctx context.Context, cs content.Store, fs iofs.FS, descriptor imagespec.Descriptor) error {
	// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#blobs
	blob, err := fs.Open(path.Join("blobs", string(descriptor.Digest.Algorithm()), descriptor.Digest.Encoded())) // "blobs/sha256/deadbeefdeadbeefdeadbeef..."
	if err != nil {
		return err
	}
	defer blob.Close()

	ingestRef := string(descriptor.Digest)

	// explicitly "abort" the ref we're about to use in case there's a partial or failed ingest already (which content.WriteBlob will then quietly reuse, over and over)
	_ = cs.Abort(ctx, ingestRef)

	// WriteBlob does *not* limit reads to the provided size, so let's wrap ourselves in a LimitedReader to prevent reading (much) more than we intend
	r := io.LimitReader(
		blob,
		descriptor.Size+1, // +1 to allow WriteBlob to detect if it reads too much
	)

	// WriteBlob verifies the digest and the size while ingesting
	return content.WriteBlob(ctx, cs, ingestRef, r, descriptor)
}

// this is "docker build" but for "Builder: oci-import"
func ociImportBuild(tags []string, commit, dir, file string) (*imagespec.Descriptor, error) {
	// TODO use r.archGitFS (we have no r or arch or entry here 😅)
	fs, err := gitCommitFS(commit)
	if err != nil {
		return nil, err
	}
	fs, err = iofs.Sub(fs, dir)
	if err != nil {
		return nil, err
	}

	// added to any string errors we generate to add more helpful debugging context for things like the wrong filename, directory, commit ID, etc.
	errFileStr := func(file string) string { return fmt.Sprintf("%q (from directory %q in commit %q)", file, dir, commit) }

	// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#oci-layout-file
	var layout imagespec.ImageLayout
	if err := readJSONFile(fs, "oci-layout", &layout); err != nil {
		return nil, fmt.Errorf("failed reading %s: %w", errFileStr("oci-layout"), err)
	}
	if layout.Version != "1.0.0" {
		// "The imageLayoutVersion value will align with the OCI Image Specification version at the time changes to the layout are made, and will pin a given version until changes to the image layout are required."
		return nil, fmt.Errorf("unsupported OCI image layout version %q in %s", layout.Version, errFileStr("oci-layout"))
	}

	var manifestDescriptor imagespec.Descriptor

	if file == "index.json" {
		// https://github.com/opencontainers/image-spec/blob/v1.0.2/image-layout.md#indexjson-file
		var index imagespec.Index
		if err := readJSONFile(fs, file, &index); err != nil {
			return nil, fmt.Errorf("failed reading %s: %w", errFileStr(file), err)
		}
		if index.SchemaVersion != 2 {
			return nil, fmt.Errorf("unsupported schemaVersion %d in %s", index.SchemaVersion, errFileStr(file))
		}
		if len(index.Manifests) != 1 {
			return nil, fmt.Errorf("expected only one manifests entry (not %d) in %s", len(index.Manifests), errFileStr(file))
		}
		manifestDescriptor = index.Manifests[0]
	} else {
		if err := readJSONFile(fs, file, &manifestDescriptor); err != nil {
			return nil, fmt.Errorf("failed reading %s: %w", errFileStr(file), err)
		}
	}

	if manifestDescriptor.MediaType != imagespec.MediaTypeImageManifest {
		return nil, fmt.Errorf("unsupported mediaType %q in descriptor in %s", manifestDescriptor.MediaType, errFileStr(file))
	}
	if err := manifestDescriptor.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid digest %q in %s: %w", manifestDescriptor.Digest, errFileStr(file), err)
	}
	if manifestDescriptor.Size < 0 {
		return nil, fmt.Errorf("invalid size %d in descriptor in %s", manifestDescriptor.Size, errFileStr(file))
	}
	// TODO validate Platform is either unset or matches expected value

	manifestDescriptor.URLs = nil
	manifestDescriptor.Annotations = nil

	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to containerd: %w", err)
	}
	// NO: defer client.Close()

	cs := client.ContentStore()

	if err := importOCIBlob(ctx, cs, fs, manifestDescriptor); err != nil {
		return nil, fmt.Errorf("failed to import manifest blob %s: %w", errFileStr(string(manifestDescriptor.Digest)), err)
	}
	var manifest imagespec.Manifest
	if err := readContentJSON(ctx, cs, manifestDescriptor, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest %s: %w", errFileStr(string(manifestDescriptor.Digest)), err)
	}

	otherBlobs := append([]imagespec.Descriptor{manifest.Config}, manifest.Layers...)
	for i, blob := range otherBlobs {
		if i == 0 && blob.MediaType != imagespec.MediaTypeImageConfig {
			return nil, fmt.Errorf("unsupported mediaType %q for config descriptor %s", blob.MediaType, errFileStr(string(blob.Digest)))
		} else if i != 0 && blob.MediaType != imagespec.MediaTypeImageLayer && blob.MediaType != imagespec.MediaTypeImageLayerGzip && blob.MediaType != imagespec.MediaTypeImageLayerZstd {
			return nil, fmt.Errorf("unsupported mediaType %q for layer descriptor %s", blob.MediaType, errFileStr(string(blob.Digest)))
		}
		if blob.Size < 0 {
			return nil, fmt.Errorf("invalid size %d in blob descriptor %s", blob.Size, errFileStr(string(blob.Digest)))
		}
		if err := importOCIBlob(ctx, cs, fs, blob); err != nil {
			return nil, fmt.Errorf("failed to import blob %s: %w", errFileStr(string(blob.Digest)), err)
		}
	}

	is := client.ImageService()

	for _, tag := range tags {
		ref, err := docker.ParseAnyReference(tag)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tag %q while updating image in containerd: %w", tag, err)
		}
		img := images.Image{
			Name:   ref.String(),
			Target: manifestDescriptor,
		}
		img2, err := is.Update(ctx, img, "target") // "target" here is to specify that we want to update the descriptor that "Name" points to (if this image name already exists)
		if err != nil {
			if !errdefs.IsNotFound(err) {
				return nil, fmt.Errorf("failed to update image %q in containerd: %w", img.Name, err)
			}
			img2, err = is.Create(ctx, img)
			if err != nil {
				return nil, fmt.Errorf("failed to create image %q in containerd: %w", img.Name, err)
			}
		}
		img = img2 // unnecessary? :)
	}

	return &manifestDescriptor, nil
}

// `ctr image import` (used when interfacing with buildx build + SBOMs that Docker can't represent correctly)
func containerdImageLoad(r io.Reader) ([]images.Image, error) {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return nil, err
	}
	// NO: defer client.Close()

	return client.Import(ctx, r, containerd.WithAllPlatforms(true))
}

// `ctr image export | docker load` containerd images back into Docker (so dependent images can use them without pulling them back down)
func containerdDockerLoad(desc imagespec.Descriptor, tags []string) error {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return err
	}
	// NO: defer client.Close()

	names := make([]string, len(tags))
	for i, tag := range tags {
		ref, err := docker.ParseAnyReference(tag)
		if err != nil {
			return fmt.Errorf("failed to parse tag %q while loading containerd image into Docker: %w", tag, err)
		}
		names[i] = ref.String()
	}
	exportOpts := []archive.ExportOpt{
		archive.WithPlatform(platforms.All),
		archive.WithAllPlatforms(),
		archive.WithManifest(desc, names...),
	}

	dockerLoad := exec.Command("docker", "load")
	dockerLoad.Stdout = os.Stdout
	dockerLoad.Stderr = os.Stderr
	dockerLoadStdin, err := dockerLoad.StdinPipe()
	if err != nil {
		return err
	}
	defer dockerLoadStdin.Close()

	if debugFlag {
		fmt.Printf("$ docker load\n")
	}
	err = dockerLoad.Start()
	if err != nil {
		return err
	}
	defer dockerLoad.Process.Kill()

	if debugFlag {
		fmt.Printf("$ ctr image export %q\n", names)
	}
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
func containerdImageLookup(tag string) (*imagespec.Descriptor, error) {
	ctx := context.Background()

	ctx, client, err := newContainerdClient(ctx)
	if err != nil {
		return nil, err
	}
	// NO: defer client.Close()

	is := client.ImageService()

	ref, err := docker.ParseAnyReference(tag)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tag %q while looking up containerd ref: %w", tag, err)
	}

	img, err := is.Get(ctx, ref.String())
	if err != nil {
		return nil, err
	}

	return &img.Target, nil
}

// given a descriptor and a list of tags (intended for pushing), return the set of those that are up-to-date (`skip`) and those that need-update (`update`)
func containerdPushFilter(desc imagespec.Descriptor, destinationTags []string) (skip, update []string, err error) {
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
func containerdPush(desc imagespec.Descriptor, destinationTags []string) error {
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
