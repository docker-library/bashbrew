package gitfs_test

import (
	"io"
	"testing"
	"testing/fstest"

	"github.com/docker-library/bashbrew/pkg/gitfs"

	"github.com/go-git/go-git/v5"
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

func TestSymlinkFS(t *testing.T) {
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
