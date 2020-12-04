package main

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/urfave/cli"

	"github.com/docker-library/bashbrew/architecture"
	"github.com/docker-library/bashbrew/manifest"
)

var errPutShared404 = fmt.Errorf("nothing to push")

func entriesToManifestToolYaml(singleArch bool, r Repo, entries ...*manifest.Manifest2822Entry) (string, []string, error) {
	yaml := ""
	remoteDigests := []string{}
	entryIdentifiers := []string{}
	for _, entry := range entries {
		entryIdentifiers = append(entryIdentifiers, r.EntryIdentifier(entry))

		for _, entryArch := range entry.Architectures {
			if singleArch && entryArch != arch {
				continue
			}

			var ok bool

			var ociArch architecture.OCIPlatform
			if ociArch, ok = architecture.SupportedArches[entryArch]; !ok {
				// this should never happen -- the parser validates Architectures
				panic("somehow, an unsupported architecture slipped past the parser validation: " + entryArch)
			}

			var archNamespace string
			if archNamespace, ok = archNamespaces[entryArch]; !ok || archNamespace == "" {
				fmt.Fprintf(os.Stderr, "warning: no arch-namespace specified for %q; skipping (%q)\n", entryArch, r.EntryIdentifier(entry))
				continue
			}

			archImage := fmt.Sprintf("%s/%s:%s", archNamespace, r.RepoName, entry.Tags[0])

			// keep track of how many images we expect to push successfully in this manifest list (and what their manifest digests are)
			// for non-manifest-list tags, this will be exactly 1 and for failed lookups it'll be 0
			// (and if one of _these_ tags is a manifest list, we've goofed somewhere)
			archImageDigests := fetchRegistryManiestListDigests(archImage)
			if len(archImageDigests) != 1 {
				fmt.Fprintf(os.Stderr, "warning: expected 1 image for %q; got %d\n", archImage, len(archImageDigests))
			}
			remoteDigests = append(remoteDigests, archImageDigests...)

			yaml += fmt.Sprintf("  - image: %s\n", archImage)
			yaml += fmt.Sprintf("    platform:\n")
			yaml += fmt.Sprintf("      os: %s\n", ociArch.OS)
			yaml += fmt.Sprintf("      architecture: %s\n", ociArch.Architecture)
			if ociArch.Variant != "" {
				yaml += fmt.Sprintf("      variant: %s\n", ociArch.Variant)
			}
		}
	}

	if yaml == "" {
		// we're not even going to try pushing something, so let's inform the caller of that to skip the unnecessary call to "manifest-tool"
		return "", nil, errPutShared404
	}

	return "manifests:\n" + yaml, remoteDigests, nil
}

func tagsToManifestToolYaml(repo string, tags ...string) string {
	yaml := fmt.Sprintf("image: %s:%s\n", repo, tags[0])
	if len(tags) > 1 {
		yaml += "tags:\n"
		for _, tag := range tags[1:] {
			yaml += fmt.Sprintf("  - %s\n", tag)
		}
	}
	return yaml
}

func cmdPutShared(c *cli.Context) error {
	repos, err := repos(c.Bool("all"), c.Args()...)
	if err != nil {
		return cli.NewMultiError(fmt.Errorf(`failed gathering repo list`), err)
	}

	dryRun := c.Bool("dry-run")
	targetNamespace := c.String("target-namespace")
	force := c.Bool("force")
	singleArch := c.Bool("single-arch")

	if targetNamespace == "" {
		targetNamespace = namespace
	}
	if targetNamespace == "" {
		return fmt.Errorf(`either "--target-namespace" or "--namespace" is a required flag for "put-shared"`)
	}

	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return cli.NewMultiError(fmt.Errorf(`failed fetching repo %q`, repo), err)
		}

		targetRepo := path.Join(targetNamespace, r.RepoName)

		sharedTagGroups := []manifest.SharedTagGroup{}

		if !singleArch {
			// handle all multi-architecture tags first (regardless of whether they have SharedTags)
			// turn them into SharedTagGroup objects so all manifest-tool invocations can be handled by a single process/loop
			for _, entry := range r.Entries() {
				entryCopy := *entry
				sharedTagGroups = append(sharedTagGroups, manifest.SharedTagGroup{
					SharedTags: entry.Tags,
					Entries:    []*manifest.Manifest2822Entry{&entryCopy},
				})
			}
		}

		// TODO do something smarter with r.TagName (ie, the user has done something crazy like "bashbrew put-shared single-repo:single-tag")
		if r.TagName == "" {
			sharedTagGroups = append(sharedTagGroups, r.Manifest.GetSharedTagGroups()...)
		} else {
			fmt.Fprintf(os.Stderr, "warning: a single tag was requested -- skipping SharedTags\n")
		}

		if len(sharedTagGroups) == 0 {
			continue
		}

		failed := []string{}
		for _, group := range sharedTagGroups {
			yaml, expectedRemoteDigests, err := entriesToManifestToolYaml(singleArch, *r, group.Entries...)
			if err == errPutShared404 {
				fmt.Fprintf(os.Stderr, "skipping %s (nothing to push)\n", fmt.Sprintf("%s:%s", targetRepo, group.SharedTags[0]))
				continue
			} else if err != nil {
				return err
			}

			if len(expectedRemoteDigests) < 1 {
				// if "expectedRemoteDigests" comes back empty, we've probably got an API issue (or a build error/push timing problem)
				fmt.Fprintf(os.Stderr, "warning: no images expected to push for %q\n", fmt.Sprintf("%s:%s", targetRepo, group.SharedTags[0]))
			}

			tagsToPush := []string{}
			for _, tag := range group.SharedTags {
				image := fmt.Sprintf("%s:%s", targetRepo, tag)
				if !force {
					remoteDigests := fetchRegistryManiestListDigests(image)
					if len(expectedRemoteDigests) == 0 && remoteDigests == nil {
						// https://github.com/golang/go/issues/12918 ...
						remoteDigests = []string{}
						// ("fetchRegistryManiestListDigests" returns a nil slice for things like 404, which if we expect to push 0 items is exactly what we want/expect)
					}
					if reflect.DeepEqual(remoteDigests, expectedRemoteDigests) {
						fmt.Fprintf(os.Stderr, "skipping %s (%d remote digests up-to-date)\n", image, len(remoteDigests))
						continue
					}
				}
				tagsToPush = append(tagsToPush, tag)
			}

			if len(tagsToPush) == 0 {
				continue
			}

			groupIdentifier := fmt.Sprintf("%s:%s", targetRepo, tagsToPush[0])
			fmt.Printf("Putting %s\n", groupIdentifier)
			if !dryRun {
				tagYaml := tagsToManifestToolYaml(targetRepo, tagsToPush...) + yaml
				if err := manifestToolPushFromSpec(tagYaml); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed putting %s, skipping (collecting errors)\n", groupIdentifier)
					failed = append(failed, fmt.Sprintf("- %s: %v", groupIdentifier, err))
					continue
				}
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("failed putting groups:\n%s", strings.Join(failed, "\n"))
		}
	}

	return nil
}
