package gitfs_test

import (
	"io"
	"testing"
	"testing/fstest"

	"github.com/docker-library/bashbrew/pkg/gitfs"

	"github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"
)

func TestCommitFS(t *testing.T) {
	// TODO instead of cloning a remote repository, synthesize a very simple Git repository right in the test here (benefit of the remote repository is that it's much larger, so fstest.TestFS has a lot more data to test against)
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:          "https://github.com/docker-library/hello-world.git",
		SingleBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := gitfs.CommitHash(repo, "480c62c690c0af4427372cf7f0de11da4e00e6c5")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Open+ReadAll", func(t *testing.T) {
		r, err := f.Open("greetings/hello-world.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		expected := "Hello from Docker!\n"
		if string(b) != expected {
			t.Fatalf("expected %q, got %q", expected, string(b))
		}
	})

	t.Run("fstest.TestFS", func(t *testing.T) {
		if err := fstest.TestFS(f, "greetings/hello-world.txt"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRootSymlinkFS(t *testing.T) {
	// TODO instead of cloning a remote repository, synthesize a very simple Git repository right in the test here (benefit of the remote repository is that it's much larger, so fstest.TestFS has a lot more data to test against)
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:          "https://github.com/tianon/gosu.git", // just a repository with a known symlink (`.dockerignore` -> `.gitignore`)
		SingleBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := gitfs.CommitHash(repo, "b73cc93b6f5b5a045c397ff0f75190e33d853946")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Open+ReadAll", func(t *testing.T) {
		r, err := f.Open(".dockerignore")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		expected := ".git\nSHA256SUMS*\ngosu*\n"
		if string(b) != expected {
			t.Fatalf("expected %q, got %q", expected, string(b))
		}
	})

	// might as well run fstest again, now that we have a new filesystem tree 😅
	t.Run("fstest.TestFS", func(t *testing.T) {
		if err := fstest.TestFS(f, ".dockerignore", "hub/Dockerfile.debian"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSubdirSymlinkFS(t *testing.T) {
	// TODO instead of cloning a remote repository, synthesize a very simple Git repository right in the test here (benefit of the remote repository is that it's much larger, so fstest.TestFS has a lot more data to test against)
	// Init + CreateRemoteAnonymous + Fetch because Clone doesn't support fetch-by-commit
	repo, err := git.Init(memory.NewStorage(), nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := repo.CreateRemoteAnonymous(&goGitConfig.RemoteConfig{
		Name: "anonymous",
		URLs: []string{"https://github.com/docker-library/busybox.git"}, // just a repository with a known symlink at a non-root level (`latest/musl/amd64/blobs/sha256/6e5e0f90c009d12db9478afe5656920e7bdd548e9fd8f50eab2be694102ae318` -> `../../image-config.json`)
	})
	if err != nil {
		t.Fatal(err)
	}
	commit := "668d52e6f0596e0fd0b1be1d8267c4b9240dc2b3"
	err = remote.Fetch(&git.FetchOptions{
		RefSpecs: []goGitConfig.RefSpec{goGitConfig.RefSpec(commit + ":FETCH_HEAD")},
		Tags:     git.NoTags,
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := gitfs.CommitHash(repo, commit)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Open+ReadAll", func(t *testing.T) {
		r, err := f.Open("latest/musl/amd64/blobs/sha256/6e5e0f90c009d12db9478afe5656920e7bdd548e9fd8f50eab2be694102ae318")
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		expected := `{
	"config": {
		"Cmd": [
			"sh"
		]
	},
	"created": "2023-05-18T22:34:17Z",
	"history": [
		{
			"created": "2023-05-18T22:34:17Z",
			"created_by": "BusyBox 1.36.1 (musl), Alpine 3.19.1"
		}
	],
	"rootfs": {
		"type": "layers",
		"diff_ids": [
			"sha256:994bf8f4adc78c5c1e4a6b5e3b59ad57902b301e0e79255a3e95ea4b213a76bd"
		]
	},
	"architecture": "amd64",
	"os": "linux"
}
`
		if string(b) != expected {
			t.Fatalf("expected %q, got %q", expected, string(b))
		}
	})

	// might as well run fstest again, now that we have a new filesystem tree 😅
	t.Run("fstest.TestFS", func(t *testing.T) {
		if err := fstest.TestFS(f, "latest/musl/amd64/blobs/sha256/6e5e0f90c009d12db9478afe5656920e7bdd548e9fd8f50eab2be694102ae318", "latest/musl/amd64/index.json"); err != nil {
			t.Fatal(err)
		}
	})
}
