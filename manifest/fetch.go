package manifest

import (
	"os"
	"net/http"
	"path"
	"path/filepath"
)

// "dir" is the default "library directory"; returns the parsed version of (in order) the file "repo", the file "dir/repo", or the remote file fetched from the URL "repo"
func Fetch(dir, repo string) (string, *Manifest2822, error) {
	repoName := path.Base(repo)

	// try file paths first
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

	// must be remote URL
	resp, err := http.Get(repo)
	if err != nil {
		return repoName, nil, err
	}
	defer resp.Body.Close()
	man, err := Parse(resp.Body)
	return repoName, man, err

	//return repoName, nil, fmt.Errorf("unable to find a manifest named %q (in %q or as a remote URL)", repo, dir)
}
