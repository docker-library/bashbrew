package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/urfave/cli"
	"pault.ag/go/topsort"

	"github.com/docker-library/bashbrew/manifest"
)

func cmdOffspring(c *cli.Context) error {
	return cmdFamily(false, c)
}

func cmdParents(c *cli.Context) error {
	return cmdFamily(true, c)
}

type topsortDepthNodes struct {
	depth int
	nodes []*topsort.Node
}

func cmdFamily(parents bool, c *cli.Context) error {
	depsRepos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	uniq := c.Bool("uniq")
	applyConstraints := c.Bool("apply-constraints")
	archFilter := c.Bool("arch-filter")
	depth := c.Int("depth")

	allRepos, err := repos(true)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering ALL repos list`), err)
	}

	// create network (all repos)
	network := topsort.NewNetwork()

	// add nodes
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

			for _, tag := range r.Tags(namespace, false, entry) {
				network.AddNode(tag, entry)
			}
		}
	}

	// add edges
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

			entryArches := []string{arch}
			if !applyConstraints && !archFilter {
				entryArches = entry.Architectures
			}

			for _, entryArch := range entryArches {
				froms, err := r.ArchDockerFroms(entryArch, entry)
				if err != nil {
					return cli.NewMultiError(fmt.Errorf(`failed fetching/scraping FROM for %q (tags %q, arch %q)`, r.RepoName, entry.TagsString(), entryArch), err)
				}
				for _, from := range froms {
					for _, tag := range r.Tags(namespace, false, entry) {
						network.AddEdge(from, tag)
					}
				}
			}
		}
	}

	// now the real work
	seen := map[string]bool{}
	for _, repo := range depsRepos {
		tagsToConsider := []string{}

		if r, err := fetch(repo); err == nil {
			// fetch succeeded, must be an object we know about so let's do a proper listing/search
			for _, entry := range r.Entries() {
				if applyConstraints && r.SkipConstraints(entry) {
					continue
				}
				if archFilter && !entry.HasArchitecture(arch) {
					continue
				}

				if len(r.TagEntries) == 1 {
					// we can't include SharedTags here or else they'll make "bashbrew parents something:simple" show the parents of the shared tags too ("nats:scratch" leading to both "nats:alpine" *and* "nats:windowsservercore" instead of just "nats:alpine" like it should), so we have to reimplement bits of "r.Tags" to exclude them
					tagRepo := path.Join(namespace, r.RepoName)
					for _, rawTag := range entry.Tags {
						tag := tagRepo + ":" + rawTag
						tagsToConsider = append(tagsToConsider, tag)
					}
				} else {
					// if we're doing something like "bashbrew children full-repo" (or "bashbrew children full-repo:shared-tag"), we *need* to include the SharedTags or else things that are "FROM full-repo:shared-tag" won't show up 😅 ("bashbrew children adoptopenjdk" was just "cassandra" instead of the full list)
					tagsToConsider = append(tagsToConsider, r.Tags(namespace, false, entry)...)
				}
			}
		} else {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		for _, tag := range tagsToConsider {
			nodes := []topsortDepthNodes{}
			if parents {
				nodes = append(nodes, topsortDepthNodes{
					depth: 1,
					nodes: network.Get(tag).InboundEdges,
				})
			} else {
				nodes = append(nodes, topsortDepthNodes{
					depth: 1,
					nodes: network.Get(tag).OutboundEdges,
				})
			}
			for len(nodes) > 0 {
				depthNodes := nodes[0]
				nodes = nodes[1:]
				if depth > 0 && depthNodes.depth > depth {
					continue
				}
				for _, node := range depthNodes.nodes {
					seenKey := node.Name
					if uniq {
						seenKey = seenKey[:strings.Index(seenKey, ":")+1] + node.Value.(*manifest.Manifest2822Entry).Tags[0]
					}
					if seen[seenKey] {
						continue
					}
					seen[seenKey] = true
					fmt.Printf("%s\n", seenKey)
					if parents {
						nodes = append(nodes, topsortDepthNodes{
							depth: depthNodes.depth + 1,
							nodes: node.InboundEdges,
						})
					} else {
						nodes = append(nodes, topsortDepthNodes{
							depth: depthNodes.depth + 1,
							nodes: node.OutboundEdges,
						})
					}
				}
			}
		}
	}

	return nil
}
