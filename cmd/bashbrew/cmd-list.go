package main

import (
	"fmt"
	"path"

	"github.com/docker-library/bashbrew/manifest"
	"github.com/urfave/cli"
)

func cmdList(c *cli.Context) error {
	repos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	uniq := c.Bool("uniq")
	applyConstraints := c.Bool("apply-constraints")
	archFilter := c.Bool("arch-filter")
	onlyRepos := c.Bool("repos")

	buildOrder := c.Bool("build-order")
	if buildOrder {
		repos, err = sortRepos(repos, applyConstraints)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed sorting repo list`), err)
		}
	}

	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		if onlyRepos {
			if r.TagEntry == nil {
				fmt.Printf("%s\n", path.Join(namespace, r.RepoName))
			} else {
				for _, tag := range r.Tags(namespace, uniq, r.TagEntry) {
					fmt.Printf("%s\n", tag)
				}
			}
			continue
		}

		var entries []*manifest.Manifest2822Entry
		if buildOrder {
			entries, err = r.SortedEntries(applyConstraints)
			if err != nil {
				return cli.NewMultiError(fmt.Errorf(`failed sorting entries list for %q`, repo), err)
			}
		} else {
			entries = r.Entries()
		}

		for _, entry := range entries {
			if applyConstraints && r.SkipConstraints(entry) {
				continue
			}
			if archFilter && !entry.HasArchitecture(arch) {
				continue
			}

			for _, tag := range r.Tags(namespace, uniq, entry) {
				fmt.Printf("%s\n", tag)
			}
		}
	}

	return nil
}
