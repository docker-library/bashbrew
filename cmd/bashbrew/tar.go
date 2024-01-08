package main

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/docker-library/bashbrew/manifest"
	"github.com/docker-library/bashbrew/pkg/tarscrub"
)

func (r Repo) archContextTar(arch string, entry *manifest.Manifest2822Entry, w io.Writer) error {
	f, err := r.archGitFS(arch, entry)
	if err != nil {
		return err
	}

	return tarscrub.WriteTar(f, w)
}

func (r Repo) ArchGitChecksum(arch string, entry *manifest.Manifest2822Entry) (string, error) {
	h := sha256.New()
	err := r.archContextTar(arch, entry, h)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
