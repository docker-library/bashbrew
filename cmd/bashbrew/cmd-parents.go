package main

import (
	"errors"
	"fmt"

	"github.com/docker-library/bashbrew/manifest"

	"github.com/urfave/cli"
)

func cmdParents(c *cli.Context) error {
	repos := c.Args()
	if len(repos) < 1 {
		return fmt.Errorf(`need at least one repo`)
	}

	uniq := c.Bool("uniq")
	applyConstraints := c.Bool("apply-constraints")
	archFilter := c.Bool("arch-filter")
	depth := c.Int("depth")

	// used in conjunction with "uniq" to make sure we print a given tag once and only once when enabled
	seen := map[string]struct{}{}

	for _, repo := range repos {
		lookup := []string{repo}
		lookupArches := dedupeSlice[string]{} // this gets filled with the Architectures of the entries of the specified "repo" (such that we can then filter the architectures of the parents of the parents appropriately to prevent "orientdb" from having "mcr.microsoft.com/windows/servercore" as a parent due to being "FROM eclipse-temurin:8-jdk" but with a Linux-limited set of supported architectures)
		for d := depth; len(lookup) > 0 && (depth == 0 || d > 0); d-- {
			nextLookup := dedupeSlice[string]{}
			for _, repo := range lookup {
				r, err := fetch(repo)
				if err != nil {
					var (
						manifestNotFoundErr manifest.ManifestNotFoundError
						tagNotFoundErr      manifest.TagNotFoundError
					)
					if d != depth && (errors.As(err, &manifestNotFoundErr) || errors.As(err, &tagNotFoundErr)) {
						// if this repo isn't one of the original top-level arguments and our error is just that it's not a supported tag, walk no further ("FROM mcr.microsoft.com/...", etc)
						continue
					}
					return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
				}
				for _, entry := range r.Entries() {
					if applyConstraints && r.SkipConstraints(entry) {
						continue
					}
					if archFilter && !entry.HasArchitecture(arch) {
						continue
					}

					if d == depth {
						if !applyConstraints && !archFilter {
							for _, entryArch := range entry.Architectures {
								lookupArches.add(entryArch)
							}
						} else {
							lookupArches.add(arch)
						}
					}

					entryFroms := dedupeSlice[string]{}
					for _, lookupArch := range lookupArches.slice() {
						if !entry.HasArchitecture(lookupArch) {
							continue
						}
						froms, err := r.ArchDockerFroms(lookupArch, entry)
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed fetching/scraping FROM for %q (tags %q, arch %q)`, r.RepoName, entry.TagsString(), lookupArch), err)
						}
						for _, from := range froms {
							if from == "scratch" {
								// "scratch" isn't really anyone's actual parent (it's a special-case built-in)
								continue
							}
							entryFroms.add(from)
						}
					}
					for _, from := range entryFroms.slice() {
						nextLookup.add(from)
						if uniq {
							if _, ok := seen[from]; ok {
								continue
							}
							seen[from] = struct{}{}
						}
						fmt.Println(from)
					}
				}
			}
			lookup = nextLookup.slice()
		}
	}

	return nil
}
