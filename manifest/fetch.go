package manifest

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// "dir" is the default "library directory"
// returns the parsed version of (in order):
//   if "repo" is a URL, the remote contents of that URL
//   the file "repo"
//   the file "dir/repo"
// (repoName, tagName, man, err)
func Fetch(dir, repo string) (string, string, *Manifest2822, error) {
	repoName := path.Base(repo)
	tagName := ""
	if tagIndex := strings.IndexRune(repoName, ':'); tagIndex > 0 {
		tagName = repoName[tagIndex+1:]
		repoName = repoName[:tagIndex]
		repo = strings.TrimSuffix(repo, ":"+tagName)
	}

	u, err := url.Parse(repo)
	if err == nil && u.IsAbs() {
		// must be remote URL!
		resp, err := http.Get(repo)
		if err != nil {
			return repoName, tagName, nil, err
		}
		defer resp.Body.Close()
		man, err := Parse(resp.Body)
		return repoName, tagName, man, err
	}

	// try file paths
	for _, fileName := range []string{
		repo,
		filepath.Join(dir, repo),
	} {
		f, err := os.Open(fileName)
		if err != nil && !os.IsNotExist(err) {
			return repoName, tagName, nil, err
		}
		if err == nil {
			defer f.Close()
			man, err := Parse(f)
			return repoName, tagName, man, err
		}
	}

	return repoName, tagName, nil, fmt.Errorf("unable to find a manifest named %q (in %q or as a remote URL)", repo, dir)
}
