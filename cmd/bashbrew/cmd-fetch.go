package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
)

func cmdFetch(c *cli.Context) error {
	repos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	applyConstraints := c.Bool("apply-constraints")
	archFilter := c.Bool("arch-filter")

	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		for _, entry := range r.Entries() {
			if applyConstraints && r.SkipConstraints(entry) {
				continue
			}
			if archFilter && !entry.HasArchitecture(arch) {
				continue
			}

			arches := entry.Architectures
			if applyConstraints || archFilter {
				arches = []string{arch}
			}

			for _, entryArch := range arches {
				commit, err := r.fetchGitRepo(entryArch, entry)
				if err != nil {
					return cli.NewMultiError(fmt.Errorf(`failed fetching git repo for %q (tags %q on arch %q)`, r.RepoName, entry.TagsString(), entryArch), err)
				}
				if debugFlag {
					fmt.Fprintf(os.Stderr, "DEBUG: fetched %s (%q, %q)\n", commit, r.EntryIdentifier(entry), entryArch)
				}
			}
		}
	}

	return nil
}
