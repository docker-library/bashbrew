package dockerfile

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"unicode"
)

type Metadata struct {
	StageFroms     []string          // every image "FROM" instruction value (or the parent stage's FROM value in the case of a named stage)
	StageNames     []string          // the name of any named stage (in order)
	StageNameFroms map[string]string // map of stage names to FROM values (or the parent stage's FROM value in the case of a named stage), useful for resolving stage names to FROM values

	Froms []string // every "FROM" or "COPY --from=xxx" value (minus named and/or numbered stages in the case of "--from=")
}

func Parse(dockerfile string) (Metadata, error) {
	return ParseReader(strings.NewReader(dockerfile))
}

func ParseReader(dockerfile io.Reader) (Metadata, error) {
	meta := Metadata{
		// panic: assignment to entry in nil map
		StageNameFroms: map[string]string{},
		// (nil slices work fine)
	}

	scanner := bufio.NewScanner(dockerfile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			// ignore straight up blank lines (no complexity)
			continue
		}

		// (we can't have a comment that ends in a continuation line - that's not continuation, that's part of the comment)
		if line[0] == '#' {
			// TODO handle "escape" parser directive
			// TODO handle "syntax" parser directive -- explode appropriately (since custom syntax invalidates our Dockerfile parsing)
			// ignore comments
			continue
		}

		// handle line continuations
		// (TODO see note above regarding "escape" parser directive)
		for line[len(line)-1] == '\\' {
			if !scanner.Scan() {
				line = line[0 : len(line)-1]
				break
			}
			// "strings.TrimRightFunc(IsSpace)" because whitespace *after* the escape character is supported and ignored 🙈
			nextLine := strings.TrimRightFunc(scanner.Text(), unicode.IsSpace)
			if nextLine == "" { // if it's all space, TrimRight will be TrimSpace 😏
				// ignore "empty continuation" lines (https://github.com/moby/moby/pull/33719)
				continue
			}
			if strings.TrimLeftFunc(nextLine, unicode.IsSpace)[0] == '#' {
				// ignore comments inside continuation (https://github.com/moby/moby/issues/29005)
				continue
			}
			line = line[0:len(line)-1] + nextLine
		}

		// TODO *technically* a line like "   RUN     echo  hi    " should be parsed as "RUN" "echo  hi" (cut off instruction, then the rest of the line with TrimSpace), but for our needs "strings.Fields" is good enough for now

		// line = strings.TrimSpace(line) // (emulated below; "strings.Fields" does essentially the same exact thing so we don't need to do it explicitly here too)

		fields := strings.Fields(line)

		if len(fields) < 1 {
			// ignore empty lines
			continue
		}

		instruction := strings.ToUpper(fields[0])

		// TODO balk at ARG / $ in from values

		switch instruction {
		case "FROM":
			from := fields[1]

			if stageFrom, ok := meta.StageNameFroms[from]; ok {
				// if this is a valid stage name, we should resolve it back to the original FROM value of that previous stage (we don't care about inter-stage dependencies for the purposes of either tag dependency calculation or tag building -- just how many there are and what external things they require)
				from = stageFrom
			}

			// make sure to add ":latest" if it's implied
			from = latestizeRepoTag(from)

			meta.StageFroms = append(meta.StageFroms, from)
			meta.Froms = append(meta.Froms, from)

			if len(fields) == 4 && strings.ToUpper(fields[2]) == "AS" {
				stageName := fields[3]
				meta.StageNames = append(meta.StageNames, stageName)
				meta.StageNameFroms[stageName] = from
			}

		case "COPY":
			for _, arg := range fields[1:] {
				if !strings.HasPrefix(arg, "--") {
					// doesn't appear to be a "flag"; time to bail!
					break
				}
				if !strings.HasPrefix(arg, "--from=") {
					// ignore any flags we're not interested in
					continue
				}
				from := arg[len("--from="):]

				if stageFrom, ok := meta.StageNameFroms[from]; ok {
					// see note above regarding stage names in FROM
					from = stageFrom
				} else if stageNumber, err := strconv.Atoi(from); err == nil && stageNumber < len(meta.StageFroms) {
					// must be a stage number, we should resolve it too
					from = meta.StageFroms[stageNumber]
				}

				// make sure to add ":latest" if it's implied
				from = latestizeRepoTag(from)

				meta.Froms = append(meta.Froms, from)
			}

		case "RUN": // TODO combine this and the above COPY-parsing code somehow sanely
			for _, arg := range fields[1:] {
				if !strings.HasPrefix(arg, "--") {
					// doesn't appear to be a "flag"; time to bail!
					break
				}
				if !strings.HasPrefix(arg, "--mount=") {
					// ignore any flags we're not interested in
					continue
				}
				csv := arg[len("--mount="):]
				// TODO more correct CSV parsing
				fields := strings.Split(csv, ",")
				var mountType, from string
				for _, field := range fields {
					if strings.HasPrefix(field, "type=") {
						mountType = field[len("type="):]
						continue
					}
					if strings.HasPrefix(field, "from=") {
						from = field[len("from="):]
						continue
					}
				}
				if mountType != "bind" || from == "" {
					// this is probably something we should be worried about, but not something we're interested in parsing
					continue
				}

				if stageFrom, ok := meta.StageNameFroms[from]; ok {
					// see note above regarding stage names in FROM
					from = stageFrom
				} else if stageNumber, err := strconv.Atoi(from); err == nil && stageNumber < len(meta.StageFroms) {
					// must be a stage number, we should resolve it too
					from = meta.StageFroms[stageNumber]
				}

				// make sure to add ":latest" if it's implied
				from = latestizeRepoTag(from)

				meta.Froms = append(meta.Froms, from)
			}
		}
	}
	return meta, scanner.Err()
}

func latestizeRepoTag(repoTag string) string {
	if repoTag != "scratch" && strings.IndexRune(repoTag, ':') < 0 {
		return repoTag + ":latest"
	}
	return repoTag
}
