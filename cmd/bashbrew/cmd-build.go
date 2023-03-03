package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli"
)

func cmdBuild(c *cli.Context) error {
	repos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	repos, err = sortRepos(repos, true)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed sorting repo list`), err)
	}

	uniq := c.Bool("uniq")
	pull := c.String("pull")
	switch pull {
	case "always", "missing", "never":
		// legit
	default:
		return fmt.Errorf(`invalid value for --pull: %q`, pull)
	}
	dryRun := c.Bool("dry-run")

	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		entries, err := r.SortedEntries(true)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed sorting entries list for %q`, repo), err)
		}

		for _, entry := range entries {
			if r.SkipConstraints(entry) {
				continue
			}

			froms, err := r.DockerFroms(entry)
			if err != nil {
				return cli.NewMultiError(fmt.Errorf(`failed fetching/scraping FROM for %q (tags %q)`, r.RepoName, entry.TagsString()), err)
			}

			fromScratch := false
			for _, from := range froms {
				fromScratch = fromScratch || from == "scratch"
				if from != "scratch" && pull != "never" {
					doPull := false
					switch pull {
					case "always":
						doPull = true
					case "missing":
						_, err := dockerInspect("{{.Id}}", from)
						doPull = (err != nil)
					default:
						return fmt.Errorf(`unexpected value for --pull: %s`, pull)
					}
					if doPull {
						// TODO detect if "from" is something we've built (ie, "python:3-onbuild" is "FROM python:3" but we don't want to pull "python:3" if we "bashbrew build python")
						fmt.Printf("Pulling %s (%s)\n", from, r.EntryIdentifier(entry))
						if !dryRun {
							dockerPull(from)
						}
					}
				}
			}

			cacheTag, err := r.DockerCacheName(entry)
			if err != nil {
				return cli.NewMultiError(fmt.Errorf(`failed calculating "cache hash" for %q (tags %q)`, r.RepoName, entry.TagsString()), err)
			}
			imageTags := r.Tags(namespace, uniq, entry)
			tags := append([]string{cacheTag}, imageTags...)

			// check whether we've already built this artifact
			cachedDesc, err := containerdImageLookup(cacheTag)
			if err != nil {
				cachedDesc = nil
				_, err = dockerInspect("{{.Id}}", cacheTag)
			}
			if err != nil {
				fmt.Printf("Building %s (%s)\n", cacheTag, r.EntryIdentifier(entry))
				if !dryRun {
					commit, err := r.fetchGitRepo(arch, entry)
					if err != nil {
						return cli.NewMultiError(fmt.Errorf(`failed fetching git repo for %q (tags %q)`, r.RepoName, entry.TagsString()), err)
					}

					switch builder := entry.ArchBuilder(arch); builder {
					case "buildkit", "classic", "":
						var platform string
						if fromScratch {
							platform = ociArch.String()
						}

						archive, err := gitArchive(commit, entry.ArchDirectory(arch))
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed generating git archive for %q (tags %q)`, r.RepoName, entry.TagsString()), err)
						}
						defer archive.Close()

						if builder == "buildkit" {
							err = dockerBuildxBuild(tags, entry.ArchFile(arch), archive, platform)
						} else {
							// TODO use "meta.StageNames" to do "docker build --target" so we can tag intermediate stages too for cache (streaming "git archive" directly to "docker build" makes that a little hard to accomplish without re-streaming)
							err = dockerBuild(tags, entry.ArchFile(arch), archive, platform)
						}
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed building %q (tags %q)`, r.RepoName, entry.TagsString()), err)
						}

						archive.Close() // be sure this happens sooner rather than later (defer might take a while, and we want to reap zombies more aggressively)

					case "oci-import":
						desc, err := ociImportBuild(tags, commit, entry.ArchDirectory(arch), entry.ArchFile(arch))
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed oci-import build of %q (tags %q)`, r.RepoName, entry.TagsString()), err)
						}

						fmt.Printf("Importing %s (%s) into Docker\n", r.EntryIdentifier(entry), desc.Digest)
						err = containerdDockerLoad(*desc, imageTags)
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed oci-import into Docker of %q (tags %q)`, r.RepoName, entry.TagsString()), err)
						}

					default:
						return cli.NewMultiError(fmt.Errorf(`unknown builder %q`, builder))
					}
				}
			} else {
				fmt.Printf("Using %s (%s)\n", cacheTag, r.EntryIdentifier(entry))

				if !dryRun {
					if cachedDesc == nil {
						// https://github.com/docker-library/bashbrew/pull/61#discussion_r1044926620
						// abusing "docker build" for "tag something a lot of times, but efficiently" 👀
						err := dockerBuild(imageTags, "", strings.NewReader("FROM "+cacheTag), "")
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed tagging %q: %q`, cacheTag, strings.Join(imageTags, ", ")), err)
						}
					} else {
						fmt.Printf("Importing %s into Docker\n", cachedDesc.Digest)
						err = containerdDockerLoad(*cachedDesc, tags)
						if err != nil {
							return cli.NewMultiError(fmt.Errorf(`failed (re-)import into Docker of %q (tags %q)`, r.RepoName, entry.TagsString()), err)
						}
					}
				}
			}
		}
	}

	return nil
}
