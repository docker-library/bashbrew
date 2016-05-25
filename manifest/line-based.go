package manifest

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// TODO write more of a proper parser? (probably not worthwhile given that 2822 is the preferred format)
func ParseLineBasedLine(line string, defaults Manifest2822Entry) (*Manifest2822Entry, error) {
	entry := defaults.Clone()

	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("manifest line missing ':': %s", line)
	}
	entry.Tags = []string{strings.TrimSpace(parts[0])}

	parts = strings.SplitN(parts[1], "@", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("manifest line missing '@': %s", line)
	}
	entry.GitRepo = strings.TrimSpace(parts[0])

	parts = strings.SplitN(parts[1], " ", 2)
	entry.GitCommit = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		entry.Directory = strings.TrimSpace(parts[1])
	}

	return &entry, nil
}

func ParseLineBased(readerIn io.Reader) (*Manifest2822, error) {
	reader := bufio.NewReader(readerIn)

	manifest := &Manifest2822{
		Global: DefaultManifestEntry.Clone(),
	}
	manifest.Global.Maintainers = []string{`TODO parse old-style "maintainer:" comment lines?`}
	manifest.Global.GitFetch = "refs/heads/*" // backwards compatibility

	for {
		line, err := reader.ReadString('\n')

		line = strings.TrimSpace(line)
		if len(line) > 0 && line[0] != '#' {
			entry, parseErr := ParseLineBasedLine(line, manifest.Global)
			if parseErr != nil {
				return nil, parseErr
			}

			err = manifest.AddEntry(*entry)
			if err != nil {
				return nil, err
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	return manifest, nil
}
