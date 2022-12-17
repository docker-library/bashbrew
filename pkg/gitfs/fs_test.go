package gitfs_test

import (
	"io"
	"testing"
	// TODO "testing/fstest"

	"github.com/docker-library/bashbrew/pkg/gitfs"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/storage/memory"
)

func TestCommitFS(t *testing.T) {
	// TODO instead of cloning a remote repository, synthesize a very simple Git repository right in the test here
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:          "https://github.com/docker-library/hello-world.git",
		SingleBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := gitfs.CommitHash(repo, "480c62c690c0af4427372cf7f0de11da4e00e6c5")
	if err != nil {
		t.Fatal(err)
	}
	r, err := fs.Open("greetings/hello-world.txt")
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
	/*
		TODO (we have to implement fake directory handling for this to work; it gets ".: Open: file not found" immediately)
		if err := fstest.TestFS(fs, "greetings/hello-world.txt"); err != nil {
			t.Fatal(err)
		}
	*/
}
