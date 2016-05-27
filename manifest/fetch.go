package manifest

import (
	"os"
	"net/http"
	"path/filepath"
)

// "dir" is the default "library directory"; returns the parsed version of (in order) the file "repo", the file "dir/repo", or the remote file fetched from the URL "repo"
func Fetch(dir, repo string) (*Manifest2822, error) {
	// try file paths first
	for _, fileName := range []string{
		repo,
		filepath.Join(dir, repo),
	} {
		f, err := os.Open(fileName)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			defer f.Close()
			return Parse(f)
		}
	}

	// must be remote URL
	resp, err := http.Get(repo)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return Parse(resp.Body)

	//return nil, fmt.Errorf("unable to find a manifest named %q (in %q or as a remote URL)", repo, dir)
}
