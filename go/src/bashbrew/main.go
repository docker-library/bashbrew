package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codegangsta/cli"

	"pault.ag/go/topsort"
)

var (
	configPath  string
	flagsConfig *FlagsConfig

	defaultLibrary string
	defaultCache   string

	constraints          []string
	exclusiveConstraints bool

	verboseFlag = false
	noSortFlag  = false
)

func initDefaultConfigPath() string {
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(xdgConfig, "bashbrew")
}

func initDefaultCachePath() string {
	xdgCache := os.Getenv("XDG_CACHE_HOME")
	if xdgCache == "" {
		xdgCache = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(xdgCache, "bashbrew")
}

func repos(all bool, args ...string) ([]string, error) {
	ret := []string{}

	if all {
		dir, err := os.Open(defaultLibrary)
		if err != nil {
			return nil, err
		}
		names, err := dir.Readdirnames(-1)
		dir.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		for _, name := range names {
			ret = append(ret, filepath.Join(defaultLibrary, name))
		}
	}

	ret = append(ret, args...)

	if len(ret) < 1 {
		return nil, fmt.Errorf(`need at least one repo (either explicitly or via "--all")`)
	}

	return ret, nil
}

func sortRepos(repos []string) ([]string, error) {
	if noSortFlag || len(repos) <= 1 {
		return repos, nil
	}

	network := topsort.NewNetwork()

	rs := []*Repo{}
	for _, repo := range repos {
		r, err := fetch(repo)
		if err != nil {
			return nil, err
		}
		rs = append(rs, r)
		network.AddNode(r.RepoName, repo)
	}

	for _, r := range rs {
		for _, entry := range r.Entries() {
			from, err := r.DockerFrom(&entry)
			if err != nil {
				return nil, err
			}
			if i := strings.IndexRune(from, ':'); i >= 0 {
				// we want "repo -> repo" relations, no tags
				from = from[:i]
			}
			if from == r.RepoName {
				// "a:a -> a:b" is OK (ignore that here -- see Repo.SortedEntries for that)
				continue
			}
			// TODO somehow reconcile/avoid "a:a -> b:b, b:b -> a:c" (which will exhibit here as cyclic)
			network.AddEdgeIfExists(from, r.RepoName)
		}
	}

	nodes, err := network.Sort()
	if err != nil {
		return nil, err
	}

	ret := []string{}
	for _, node := range nodes {
		ret = append(ret, node.Value.(string))
	}

	return ret, nil
}

func main() {
	app := cli.NewApp()
	app.Name = "bashbrew"
	app.Usage = "canonical build tool for the official images"
	app.Version = "dev"
	app.HideVersion = true
	app.EnableBashCompletion = true

	// TODO add "Description" to app and commands (for longer-form description of their functionality)

	cli.VersionFlag.Name = "version" // remove "-v" from VersionFlag
	cli.HelpFlag.Name = "help, h, ?" // add "-?" to HelpFlag
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:   "verbose, v",
			EnvVar: "BASHBREW_VERBOSE",
			Usage:  `enable more output (esp. "docker build" output)`,
		},
		cli.BoolFlag{
			Name:  "no-sort",
			Usage: "do not apply any sorting, even via --build-order",
		},

		cli.StringSliceFlag{
			Name:  "constraint",
			Usage: "build constraints (see Constraints in Manifest2822Entry)",
		},
		cli.BoolFlag{
			Name:  "exclusive-constraints",
			Usage: "skip entries which do not have Constraints",
		},

		cli.StringFlag{
			Name:   "config",
			Value:  initDefaultConfigPath(),
			EnvVar: "BASHBREW_CONFIG",
			Usage:  `where default "flags" configuration can be overridden more persistently`,
		},
		cli.StringFlag{
			Name:   "library",
			Value:  filepath.Join(os.Getenv("HOME"), "docker", "official-images", "library"),
			EnvVar: "BASHBREW_LIBRARY",
			Usage:  "where the bodies are buried",
		},
		cli.StringFlag{
			Name:   "cache",
			Value:  initDefaultCachePath(),
			EnvVar: "BASHBREW_CACHE",
			Usage:  "where the git wizardry is stashed",
		},
	}

	app.Before = func(c *cli.Context) error {
		var err error

		configPath, err = filepath.Abs(c.String("config"))
		if err != nil {
			return err
		}

		flagsConfig, err = ParseFlagsConfigFile(filepath.Join(configPath, "flags"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		return nil
	}

	subcommandBeforeFactory := func(cmd string) cli.BeforeFunc {
		return func(c *cli.Context) error {
			err := flagsConfig.ApplyTo(cmd, c)
			if err != nil {
				return err
			}

			verboseFlag = c.GlobalBool("verbose")
			noSortFlag = c.GlobalBool("no-sort")

			constraints = c.GlobalStringSlice("constraint")
			exclusiveConstraints = c.GlobalBool("exclusive-constraints")

			defaultLibrary, err = filepath.Abs(c.GlobalString("library"))
			if err != nil {
				return err
			}
			defaultCache, err = filepath.Abs(c.GlobalString("cache"))
			if err != nil {
				return err
			}

			return nil
		}
	}

	// define a few useful flags so their usage, etc can be consistent
	commonFlags := map[string]cli.Flag{
		"all": cli.BoolFlag{
			Name:  "all",
			Usage: "act upon all repos listed in --library",
		},
		"uniq": cli.BoolFlag{
			Name:  "uniq, unique",
			Usage: "only act upon the first tag of each entry",
		},
		"namespace": cli.StringFlag{
			Name:  "namespace",
			Usage: "a repo namespace to act upon/in",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:    "list",
			Aliases: []string{"ls"},
			Usage:   "list repo:tag combinations for a given repo",
			Flags: []cli.Flag{
				commonFlags["all"],
				commonFlags["uniq"],
				cli.BoolFlag{
					Name:  "build-order",
					Usage: "sort by the order repos would need to build (topsort)",
				},
				cli.BoolFlag{
					Name:  "apply-constraints",
					Usage: "apply Constraints as if repos were building",
				},
			},
			Before: subcommandBeforeFactory("list"),
			Action: cmdList,
		},
		{
			Name:  "build",
			Usage: "build (and tag) repo:tag combinations for a given repo",
			Flags: []cli.Flag{
				commonFlags["all"],
				commonFlags["uniq"],
				commonFlags["namespace"],
				cli.BoolFlag{
					// TODO consider switching this to be a StringFlag with values of "always", "missing", "never" (default never)
					Name:  "pull-missing",
					Usage: "pull missing FROM tags while building",
				},
			},
			Before: subcommandBeforeFactory("build"),
			Action: cmdBuild,
		},
		{
			Name:  "tag",
			Usage: "tag repo:tag into a namespace (especially for pushing)",
			Flags: []cli.Flag{
				commonFlags["all"],
				commonFlags["uniq"],
				commonFlags["namespace"],
			},
			Before: subcommandBeforeFactory("tag"),
			Action: cmdTag,
		},
		{
			Name:  "push",
			Usage: `push namespace/repo:tag (see also "tag")`,
			Flags: []cli.Flag{
				commonFlags["all"],
				commonFlags["uniq"],
				commonFlags["namespace"],
			},
			Before: subcommandBeforeFactory("push"),
			Action: cmdPush,
		},

		{
			Name: "children",
			Aliases: []string{
				"offspring",
				"descendants",
				"progeny",
			},
			Usage:  `print the repos built FROM a given repo or repo:tag`,
			Flags:  []cli.Flag{},
			Before: subcommandBeforeFactory("children"),
			Action: cmdOffspring,

			Category: "plumbing",
		},
		{
			Name: "parents",
			Aliases: []string{
				"ancestors",
				"progenitors",
			},
			Usage:  `print the repos this repo or repo:tag is FROM`,
			Flags:  []cli.Flag{},
			Before: subcommandBeforeFactory("parents"),
			Action: cmdParents,

			Category: "plumbing",
		},
		{
			Name:  "cat",
			Usage: "print manifest contents for repo or repo:tag",
			Flags: []cli.Flag{
				commonFlags["all"],
				cli.StringFlag{
					Name:  "format, f",
					Usage: "change the `FORMAT` of the output",
					Value: DefaultCatFormat,
				},
				cli.StringFlag{
					Name:  "format-file, F",
					Usage: "use the contents of `FILE` for \"--format\"",
				},
			},
			Before: subcommandBeforeFactory("cat"),
			Action: cmdCat,

			Description: `see Go's "text/template" package (https://golang.org/pkg/text/template/) for details on the syntax expected in "--format"`,

			Category: "plumbing",
		},
		{
			Name:  "from",
			Usage: "print FROM for repo:tag",
			Flags: []cli.Flag{
				commonFlags["all"],
				commonFlags["uniq"],
			},
			Before: subcommandBeforeFactory("from"),
			Action: cmdFrom,

			Category: "plumbing",
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
