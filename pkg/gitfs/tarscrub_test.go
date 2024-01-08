package gitfs_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"

	"github.com/docker-library/bashbrew/pkg/gitfs"
	"github.com/docker-library/bashbrew/pkg/tarscrub"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/storage/memory"
)

// this example is nice because it has some intentionally dangling symlinks in it that trip things up if they aren't implemented correctly!
// (see also pkg/tarscrub/git_test.go)
func ExampleGitVarnish() {
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:          "https://github.com/varnish/docker-varnish.git",
		SingleBranch: true,
	})
	if err != nil {
		panic(err)
	}

	commit, err := gitfs.CommitHash(repo, "0c295b528f28a98650fb2580eab6d34b30b165c4")
	if err != nil {
		panic(err)
	}

	f, err := fs.Sub(commit, "stable/debian")
	if err != nil {
		panic(err)
	}

	h := sha256.New()

	if err := tarscrub.WriteTar(f, h); err != nil {
		panic(err)
	}

	fmt.Printf("%x\n", h.Sum(nil))
	// Output: 3aef5ac859b23d65dfe5e9f2a47750e9a32852222829cfba762a870c1473fad6
}
