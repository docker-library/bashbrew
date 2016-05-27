package manifest

import (
	"fmt"
	"os"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
)

// "dir" is the default "library directory"
// returns the parsed version of (in order):
//   if "repo" is a URL, the remote contents of that URL
//   the file "repo"
//   the file "dir/repo"
func Fetch(dir, repo string) (string, *Manifest2822, error) {
	repoName := path.Base(repo)

	u, err := url.Parse(repo)
	if err == nil && u.IsAbs() {
		// must be remote URL!
		resp, err := http.Get(repo)
		if err != nil {
			return repoName, nil, err
		}
		defer resp.Body.Close()
		man, err := Parse(resp.Body)
		return repoName, man, err
	}

	// try file paths
	for _, fileName := range []string{
		repo,
		filepath.Join(dir, repo),
	} {
		f, err := os.Open(fileName)
		if err != nil && !os.IsNotExist(err) {
			return repoName, nil, err
		}
		if err == nil {
			defer f.Close()
			man, err := Parse(f)
			return repoName, man, err
		}
	}

	return repoName, nil, fmt.Errorf("unable to find a manifest named %q (in %q or as a remote URL)", repo, dir)
}
