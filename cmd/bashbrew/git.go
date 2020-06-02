package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codegangsta/cli"

	"github.com/docker-library/go-dockerlibrary/manifest"
	"github.com/docker-library/go-dockerlibrary/pkg/execpipe"

	goGit "github.com/go-git/go-git/v5"
	goGitConfig "github.com/go-git/go-git/v5/config"
	goGitPlumbing "github.com/go-git/go-git/v5/plumbing"
	goGitClient "github.com/go-git/go-git/v5/plumbing/transport/client"
	goGitHttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func gitCache() string {
	return filepath.Join(defaultCache, "git")
}

func gitCommand(args ...string) *exec.Cmd {
	if debugFlag {
		fmt.Printf("$ git %q\n", args)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = gitCache()
	return cmd
}

func git(args ...string) ([]byte, error) {
	out, err := gitCommand(args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%v\ncommand: git %q\n%s", ee, args, string(ee.Stderr))
		}
	}
	return out, err
}

var gitRepo *goGit.Repository

func ensureGitInit() error {
	if gitRepo != nil {
		return nil
	}

	gitCacheDir := gitCache()
	err := os.MkdirAll(gitCacheDir, os.ModePerm)
	if err != nil {
		return err
	}

	gitRepo, err = goGit.PlainInit(gitCacheDir, true)
	if err == goGit.ErrRepositoryAlreadyExists {
		gitRepo, err = goGit.PlainOpen(gitCacheDir)
	}
	if err != nil {
		return err
	}

	// ensure garbage collection is disabled so we keep dangling commits
	config, err := gitRepo.Config()
	if err != nil {
		return err
	}
	config.Raw = config.Raw.SetOption("gc", "", "auto", "0")
	gitRepo.Storer.SetConfig(config)

	netrcClient := &http.Client{Transport: &netrcTransport{}}
	goGitClient.InstallProtocol("https", goGitHttp.NewClient(netrcClient))

	return nil
}

var fullGitCommitRegex = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

func getGitCommit(commit string) (string, error) {
	if fullGitCommitRegex.MatchString(commit) {
		_, err := gitRepo.CommitObject(goGitPlumbing.NewHash(commit))
		if err != nil {
			return "", err
		}
		return commit, nil
	}

	h, err := gitRepo.ResolveRevision(goGitPlumbing.Revision(commit + "^{commit}"))
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

func gitStream(args ...string) (io.ReadCloser, error) {
	return execpipe.Run(gitCommand(args...))
}

func gitArchive(commit string, dir string) (io.ReadCloser, error) {
	if dir == "." {
		dir = ""
	} else {
		dir += "/"
	}
	return gitStream("archive", "--format=tar", commit+":"+dir)
}

func gitShow(commit string, file string) (string, error) {
	gitCommit, err := gitRepo.CommitObject(goGitPlumbing.NewHash(commit))
	if err != nil {
		return "", err
	}

	gitFile, err := gitCommit.File(file)
	if err != nil {
		return "", err
	}

	contents, err := gitFile.Contents()
	if err != nil {
		return "", err
	}

	return contents, nil
}

// for gitNormalizeForTagUsage()
// see http://stackoverflow.com/a/26382358/433558
var (
	gitBadTagChars = regexp.MustCompile(`(?:` + strings.Join([]string{
		`[^0-9a-zA-Z/._-]+`,

		// They can include slash `/` for hierarchical (directory) grouping, but no slash-separated component can begin with a dot `.` or end with the sequence `.lock`.
		`/[.]+`,
		`[.]lock(?:/|$)`,

		// They cannot have two consecutive dots `..` anywhere.
		`[.][.]+`,

		// They cannot end with a dot `.`
		// They cannot begin or end with a slash `/`
		`[/.]+$`,
		`^[/.]+`,
	}, `|`) + `)`)

	gitMultipleSlashes = regexp.MustCompile(`(?://+)`)
)

// strip/replace "bad" characters from text for use as a Git tag
func gitNormalizeForTagUsage(text string) string {
	return gitMultipleSlashes.ReplaceAllString(gitBadTagChars.ReplaceAllString(text, "-"), "/")
}

var gitRepoCache = map[string]string{}

func (r Repo) fetchGitRepo(arch string, entry *manifest.Manifest2822Entry) (string, error) {
	cacheKey := strings.Join([]string{
		entry.ArchGitRepo(arch),
		entry.ArchGitFetch(arch),
		entry.ArchGitCommit(arch),
	}, "\n")
	if commit, ok := gitRepoCache[cacheKey]; ok {
		entry.SetGitCommit(arch, commit)
		return commit, nil
	}

	err := ensureGitInit()
	if err != nil {
		return "", err
	}

	if manifest.GitCommitRegex.MatchString(entry.ArchGitCommit(arch)) {
		commit, err := getGitCommit(entry.ArchGitCommit(arch))
		if err == nil {
			gitRepoCache[cacheKey] = commit
			entry.SetGitCommit(arch, commit)
			return commit, nil
		}
	}

	fetchStrings := []string{
		entry.ArchGitFetch(arch) + ":",
	}
	if entryArchGitCommit := entry.ArchGitCommit(arch); entryArchGitCommit == "FETCH_HEAD" {
		// fetch remote tag references to a local tag ref so that we can cache them and not re-fetch every time
		localRef := "refs/tags/" + gitNormalizeForTagUsage(cacheKey)
		commit, err := getGitCommit(localRef)
		if err == nil {
			gitRepoCache[cacheKey] = commit
			entry.SetGitCommit(arch, commit)
			return commit, nil
		}
		fetchStrings[0] += localRef
	} else {
		// we create a temporary remote dir so that we can clean it up completely afterwards
		refBase := "refs/remotes"
		refBaseDir := filepath.Join(gitCache(), refBase)

		err := os.MkdirAll(refBaseDir, os.ModePerm)
		if err != nil {
			return "", err
		}

		tempRefDir, err := ioutil.TempDir(refBaseDir, "temp")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tempRefDir)

		tempRef := path.Join(refBase, filepath.Base(tempRefDir))
		if entry.ArchGitFetch(arch) == manifest.DefaultLineBasedFetch {
			// backwards compat (see manifest/line-based.go in go-dockerlibrary)
			fetchStrings[0] += tempRef + "/*"
		} else {
			fetchStrings[0] += tempRef + "/temp"
		}

		fetchStrings = append([]string{
			// Git (and more recently, GitHub) support "git fetch"ing a specific commit directly!
			// (The "actions/checkout@v2" GitHub action uses this to fetch commits for running workflows even after branches are deleted!)
			// https://github.com/git/git/commit/f8edeaa05d8623a9f6dad408237496c51101aad8
			// https://github.com/go-git/go-git/pull/58
			// If that works, we want to prefer it (since it'll be much more efficient at getting us the commit we care about), so we prepend it to our list of "things to try fetching"
			entryArchGitCommit + ":" + tempRef + "/temp",
		}, fetchStrings...)
	}

	if strings.HasPrefix(entry.ArchGitRepo(arch), "git://github.com/") {
		fmt.Fprintf(os.Stderr, "warning: insecure protocol git:// detected: %s\n", entry.ArchGitRepo(arch))
		entry.SetGitRepo(arch, strings.Replace(entry.ArchGitRepo(arch), "git://", "https://", 1))
	}

	gitRemote, err := gitRepo.CreateRemoteAnonymous(&goGitConfig.RemoteConfig{
		Name: "anonymous",
		URLs: []string{entry.ArchGitRepo(arch)},
	})
	if err != nil {
		return "", err
	}

	var commit string
	fetchErrors := []error{}
	for _, fetchString := range fetchStrings {
		err := gitRemote.Fetch(&goGit.FetchOptions{
			RefSpecs: []goGitConfig.RefSpec{goGitConfig.RefSpec(fetchString)},
			Tags:     goGit.NoTags,

			//Progress: os.Stdout,
		})
		if err != nil {
			fetchErrors = append(fetchErrors, err)
			continue
		}

		commit, err = getGitCommit(entry.ArchGitCommit(arch))
		if err != nil {
			fetchErrors = append(fetchErrors, err)
			continue
		}

		fetchErrors = nil
		break
	}

	if len(fetchErrors) > 0 {
		return "", cli.NewMultiError(fetchErrors...)
	}

	gitTag := gitNormalizeForTagUsage(path.Join(arch, namespace, r.RepoName, entry.Tags[0]))
	gitRepo.DeleteTag(gitTag) // avoid "ErrTagExists"
	_, err = gitRepo.CreateTag(gitTag, goGitPlumbing.NewHash(commit), nil)
	if err != nil {
		return "", err
	}

	gitRepoCache[cacheKey] = commit
	entry.SetGitCommit(arch, commit)
	return commit, nil
}
