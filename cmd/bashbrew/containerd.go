package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/metadata"
	"github.com/containerd/containerd/namespaces"

	"go.etcd.io/bbolt"
)

func newBuiltinContainerdServices(ctx context.Context) (containerd.ClientOpt, error) {
	// thanks to https://github.com/Azure/image-rootfs-scanner/blob/e7041e47d1a13e15d73d9c85644542e6758f9f3a/containerd.go#L42-L87 for inspiring this magic

	root := filepath.Join(defaultCache, "containerd", arch) // because our bbolt is so highly contested, we'll use a containerd directory per-arch 😭
	dbPath := filepath.Join(root, "metadata.db")
	contentRoot := filepath.Join(root, "content")

	cs, err := local.NewStore(contentRoot)
	if err != nil {
		return nil, err
	}

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	mdb := metadata.NewDB(db, cs, nil)
	return containerd.WithServices(
		containerd.WithContentStore(mdb.ContentStore()),
		containerd.WithImageStore(metadata.NewImageStore(mdb)),
		containerd.WithLeasesService(metadata.NewLeaseManager(mdb)),
	), nil
}

var containerdClientCache *containerd.Client = nil

// the returned client is cached, don't Close() it!
func newContainerdClient(ctx context.Context) (context.Context, *containerd.Client, error) {
	ns := "bashbrew-" + arch
	for _, envKey := range []string{
		`BASHBREW_CONTAINERD_NAMESPACE`,
		`CONTAINERD_NAMESPACE`,
	} {
		if env, ok := os.LookupEnv(envKey); ok {
			if env != "" {
				// set-but-empty environment variable means use default explicitly
				ns = env
			}
			break
		}
	}
	ctx = namespaces.WithNamespace(ctx, ns)

	if containerdClientCache != nil {
		return ctx, containerdClientCache, nil
	}

	for _, envKey := range []string{
		`BASHBREW_CONTAINERD_CONTENT_ADDRESS`, // TODO if we ever need to connnect to a containerd instance for something more interesting like running containers, we need to have *that* codepath not use _CONTENT_ variants
		`BASHBREW_CONTAINERD_ADDRESS`,
		`CONTAINERD_CONTENT_ADDRESS`,
		`CONTAINERD_ADDRESS`,
	} {
		if socket, ok := os.LookupEnv(envKey); ok {
			if socket == "" {
				// we'll use a set-but-empty variable as an explicit request to use our built-in implementation
				break
			}
			client, err := containerd.New(socket)
			containerdClientCache = client
			return ctx, client, err
		}
	}

	// if we don't have an explicit variable asking us to connect to an existing containerd instance, we set up and use our own in-process content/image store
	services, err := newBuiltinContainerdServices(ctx)
	if err != nil {
		return ctx, nil, err
	}
	client, err := containerd.New("", services)
	containerdClientCache = client
	return ctx, client, err
}
