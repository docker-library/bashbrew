package manifest_test

import (
	"errors"
	"testing"

	"github.com/docker-library/bashbrew/manifest"
)

func TestFetchErrors(t *testing.T) {
	repoName, tagName, _, err := manifest.Fetch("testdata", "bash:69.420")
	if err == nil {
		t.Fatalf("expected tag-not-found error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	var tagNotFoundErr manifest.TagNotFoundError
	if !errors.As(err, &tagNotFoundErr) {
		t.Fatalf("expected tag-not-found error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)

	repoName, tagName, _, err = manifest.Fetch("testdata", "nonexistent-project:1.2.3")
	if err == nil {
		t.Fatalf("expected manifest-not-found error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	var manifestNotFoundErr manifest.ManifestNotFoundError
	if !errors.As(err, &manifestNotFoundErr) {
		t.Fatalf("expected manifest-not-found error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)
}
