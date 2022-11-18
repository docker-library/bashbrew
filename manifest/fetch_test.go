package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/docker-library/bashbrew/manifest"
)

func TestFetchErrors(t *testing.T) {
	repoName, tagName, _, err := manifest.Fetch("/dev/null", "testdata/bash:69.420")
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

	repoName, tagName, _, err = manifest.Fetch("/dev/null", "/proc/kmsg")
	if err == nil {
		t.Fatalf("expected filesystem error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "not permitted") {
		t.Fatalf("expected filesystem error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)

	repoName, tagName, _, err = manifest.Fetch("/dev/null", "./testdata")
	if err == nil {
		t.Fatalf("expected directory error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	if !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)

	repoName, tagName, _, err = manifest.Fetch("/dev/null", "https://nonexistent.subdomain.example.com/nonexistent-project:1.2.3")
	if err == nil {
		t.Fatalf("expected no such host error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	if !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("expected no such host error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)

	repoName, tagName, _, err = manifest.Fetch("/dev/null", "https://example.com:1.2.3")
	if err == nil {
		t.Fatalf("expected parse error, got repoName=%q, tagName=%q instead", repoName, tagName)
	}
	if !strings.HasPrefix(err.Error(), "Bad line:") {
		t.Fatalf("expected parse error, got %q instead", err)
	}
	t.Logf("correct, expected error: %s", err)
}
