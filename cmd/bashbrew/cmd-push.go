package main

import (
	"fmt"
	"os"
	"path"

	"github.com/urfave/cli"
)

func cmdPush(c *cli.Context) error {
	repos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	uniq := c.Bool("uniq")
	targetNamespace := c.String("target-namespace")
	dryRun := c.Bool("dry-run")
	force := c.Bool("force")

	if targetNamespace == "" {
		targetNamespace = namespace
	}
	if targetNamespace == "" {
		return fmt.Errorf(`either "--target-namespace" or "--namespace" is a required flag for "push"`)
	}

	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		tagRepo := path.Join(targetNamespace, r.RepoName)
		for _, entry := range r.Entries() {
			if r.SkipConstraints(entry) {
				continue
			}

			// we can't use "r.Tags()" here because it will include SharedTags, which we never want to push directly (see "cmd-put-shared.go")
		TagsLoop:
			for i, tag := range entry.Tags {
				if uniq && i > 0 {
					break
				}
				tag = tagRepo + ":" + tag

				if !force {
					localImageId, _ := dockerInspect("{{.Id}}", tag)
					registryImageIds := fetchRegistryImageIds(tag)
					for _, registryImageId := range registryImageIds {
						if localImageId == registryImageId {
							fmt.Fprintf(os.Stderr, "skipping %s (remote image matches local)\n", tag)
							continue TagsLoop
						}
					}
				}
				fmt.Printf("Pushing %s\n", tag)
				if !dryRun {
					err = dockerPush(tag)
					if err != nil {
						return cli.NewMultiError(fmt.Errorf(`failed pushing %q`, tag), err)
					}
				}
			}
		}
	}

	return nil
}
