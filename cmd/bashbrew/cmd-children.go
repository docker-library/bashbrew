package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/urfave/cli"
)

func cmdChildren(c *cli.Context) error {
	// we don't need this until later, but we want to bail early if we don't have it (before we've done a lot of work creating the graph)
	args := c.Args()
	if len(args) < 1 {
		return fmt.Errorf(`need at least one argument`)
	}

	allRepos, err := repos(true)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering ALL repos list`), err)
	}

	applyConstraints := c.Bool("apply-constraints")
	archFilter := c.Bool("arch-filter")

	// build up a list of canonical tag mappings and canonical tag architectures
	canonical := map[string]string{}
	arches := dedupeSliceMap[string, string]{}
	for _, repo := range allRepos {
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

			tags := r.Tags(namespace, false, entry)
			for _, tag := range tags {
				canonical[tag] = tags[0]
			}

			entryArches := []string{arch}
			if !applyConstraints && !archFilter {
				entryArches = entry.Architectures
			}
			for _, entryArch := range entryArches {
				arches.add(tags[0], entryArch)
			}
		}
	}

	// now build up a map of FROM -> canonical tag references and a "repo -> tags" lookup (including things like "alpine:3.11" that are no longer supported)
	// for non-canonical/unsupported tags, auto-create/supplement their "arches" list from the thing that's FROM them (so we can filter properly later and make sure "bashbrew children mcr.microsoft.com/windows/servercore" doesn't list non-Windows images that happen to be "FROM xyz-shared-tag" that includes Windows)
	children := dedupeSliceMap[string, string]{}
	repoTags := dedupeSliceMap[string, string]{}
	for _, repo := range allRepos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		nsRepo := path.Join(namespace, r.RepoName)

		for _, entry := range r.Entries() {
			if applyConstraints && r.SkipConstraints(entry) {
				continue
			}
			if archFilter && !entry.HasArchitecture(arch) {
				continue
			}

			entryArches := []string{arch}
			if !applyConstraints && !archFilter {
				entryArches = entry.Architectures
			}

			tag := nsRepo + ":" + entry.Tags[0]
			repoTags.add(nsRepo, tag)

			for _, entryArch := range entryArches {
				froms, err := r.ArchDockerFroms(entryArch, entry)
				if err != nil {
					return cli.NewMultiError(fmt.Errorf(`failed fetching/scraping FROM for %q (tags %q, arch %q)`, r.RepoName, entry.TagsString(), entryArch), err)
				}

				for _, from := range froms {
					if canon, ok := canonical[from]; ok {
						from = canon
					} else {
						// must be unsupported, let's make sure our current implied supported architecture value for it is recorded!
						arches.add(from, entryArch)
					}
					children.add(from, tag)
					if fromRepo, _, ok := strings.Cut(from, ":"); ok {
						// make sure things like old "alpine" tags that are no longer supported still come up with "bashbrew children alpine"
						repoTags.add(fromRepo, from)
					}
				}
			}
		}
	}

	uniq := c.Bool("uniq")
	depth := c.Int("depth")

	// used in conjunction with "uniq" to make sure we print a given tag once and only once when enabled
	seen := map[string]struct{}{}

	for _, arg := range args {
		var tags []string
		if children.has(arg) {
			// if the string has children, let's walk them verbatim
			tags = []string{arg}
		} else if tag, ok := canonical[arg]; ok {
			// if the string has a "canonical" tag (meaning is a supported tag), let's use it verbatim (whether it has children or not)
			tags = []string{tag}
		} else if nsArg := path.Join(namespace, arg); repoTags.has(nsArg) {
			// otherwise, let's do a couple lookups based on the provided argument being a repository like "alpine"
			tags = repoTags.slice(nsArg)
		} else if repoTags.has(arg) {
			tags = repoTags.slice(arg)
		}
		if len(tags) < 1 {
			return fmt.Errorf(`failed to resolve argument as repo or tag %q`, arg)
		}

		for _, tag := range tags {
			supportedArches := arches.slice(tag) // this will already be filtered in terms of archFilter / applyConstraints and is pre-implied by the above code for non-supported images like Windows base images (used to filter the children to only those that have intersection to avoid "bashbrew from .../windows/servercore" from listing non-Windows images, for example)
			if debugFlag {
				fmt.Fprintf(os.Stderr, "DEBUG: relevant architectures of %q: %s\n", tag, strings.Join(supportedArches, ", "))
			}
			if depth == -1 {
				// special value to let "bashbrew children mcr.microsoft.com/windows/servercore" print the list of FROM values in use for a repo
				fmt.Println(tag)
				continue
			}
			lookup := []string{tag}
			for d := depth; len(lookup) > 0 && (depth == 0 || d > 0); d-- {
				nextLookup := []string{}
				for _, tag := range lookup {
					kids := children.slice(tag)
					for _, kid := range kids {
						supported := false
						for _, arch := range arches.slice(kid) {
							if sliceHas[string](supportedArches, arch) {
								supported = true
								break
							}
						}
						if !supported {
							continue
						}
						nextLookup = append(nextLookup, kid)
						if uniq {
							if _, ok := seen[kid]; ok {
								continue
							}
							seen[kid] = struct{}{}
						}
						fmt.Println(kid)
					}
				}
				lookup = nextLookup
			}
		}
	}

	return nil
}
