package dockerfile

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

// internal-only bookkeeping used while scanning to resolve "FROM stage-name",
// "COPY --from=stage-name-or-number", and "RUN --mount=...,from=..." back to
// real image references; never exposed outside this package
// TODO stage name/reference (e.g. "build") is available here if a future consumer needs it
type namedStage struct {
	from     string
	platform string
}

// Parent represents a single parent reference in a Dockerfile: every "FROM"
// (Kind == "FROM") and every "COPY --from="/"RUN --mount=...,from=" that
// references an external image rather than a local stage (Kind == "COPY" or
// "RUN"). A "--from=" referencing a local stage does not get its own Parent,
// since the underlying image is already represented by that stage's own
// Kind == "FROM" entry -- and, per BuildKit's own behavior, an external
// "--from=" reference always resolves against the target platform (there is
// no way to make it inherit a stage's platform, so Platform is always ""
// for Kind != "FROM").
type Parent struct {
	From string
	// "" (also covers an explicit "$TARGETPLATFORM", normalized away here --
	// they're semantically identical) or "$BUILDPLATFORM"; always "" for Kind != "FROM"
	Platform string `json:",omitempty"`
	Kind     string // "FROM", "COPY", "RUN"
}

type Metadata struct {
	Parents []Parent

	Froms []string // every "FROM" or "COPY --from=xxx" value (minus named and/or numbered stages in the case of "--from=")
}

func Parse(dockerfile string) (Metadata, error) {
	return ParseReader(strings.NewReader(dockerfile))
}

func ParseReader(dockerfile io.Reader) (Metadata, error) {
	var meta Metadata

	// parsing-time-only bookkeeping (see "namedStage" above)
	var stages []namedStage
	namedStages := map[string]int{}

	// isStage tells callers to skip adding a Parent for this --from= -- a stage reference's
	// platform (if any, e.g. via "FROM --platform=$BUILDPLATFORM foo AS bar") is already
	// captured on that stage's own Kind == "FROM" Parent, so recording it again here would
	// either duplicate or (worse) misrepresent it as target-pinned
	resolveFrom := func(from string) (resolved string, isStage bool) {
		if i, ok := namedStages[from]; ok {
			// see note above regarding stage names in FROM
			return stages[i].from, true
		} else if stageNumber, err := strconv.Atoi(from); err == nil && stageNumber < len(stages) {
			// must be a stage number, we should resolve it too
			return stages[stageNumber].from, true
		}
		// make sure to add ":latest" if it's implied
		return latestizeRepoTag(from), false
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

		args := fields[1:]

		switch instruction {
		case "FROM":
			var stage namedStage
			var stageName string
			explicitPlatform := "" // exactly as written: "", "$BUILDPLATFORM", or "$TARGETPLATFORM" -- kept distinct from "" (unspecified) until after stage-name resolution below, since normalizing "$TARGETPLATFORM" away too early would make an *explicit* "--platform=$TARGETPLATFORM" indistinguishable from "wrote nothing at all", silently inheriting a referenced stage's "$BUILDPLATFORM" instead of erroring on the mismatch

			if platform, ok := strings.CutPrefix(args[0], "--platform="); ok {
				explicitPlatform = platform
				args = args[1:]
				switch explicitPlatform {
				case "$BUILDPLATFORM", "$TARGETPLATFORM":
					// explicitly allowed for more efficient cross-compiling (see also condition outside the meta loop to ensure the final stage is either without platform or explicitly --platform=$TARGETPLATFORM)
				default:
					return meta, fmt.Errorf("FROM has unsupported --platform=%q -- any --platform must be generic or unspecified for correct dependency calculation", explicitPlatform)
				}
			}
			// normalized for comparison/storage -- "" and "$TARGETPLATFORM" are semantically identical; "explicitPlatform" (unnormalized) is kept for the "was anything written at all" check below and for a more useful error message
			normalizedPlatform := explicitPlatform
			if normalizedPlatform == "$TARGETPLATFORM" {
				normalizedPlatform = ""
			}
			stage.platform = normalizedPlatform

			from := args[0]
			args = args[1:]

			if strings.ContainsRune(from, '$') {
				return meta, fmt.Errorf("FROM %q contains invalid/disallowed character '$' -- explicit FROM values are required for dependency calculation", from)
			}

			if i, ok := namedStages[from]; ok {
				// if this is a valid stage name, we should resolve it back to the original FROM value of that previous stage (we don't care about inter-stage dependencies for the purposes of either tag dependency calculation or tag building -- just how many there are and what external things they require)
				parent := stages[i]
				if explicitPlatform == "" {
					// bare "FROM stage-name" makes no platform assertion of its own -- just continue whatever the referenced stage resolved to
					stage.platform = parent.platform
				} else if normalizedPlatform != parent.platform {
					return meta, fmt.Errorf("FROM %q has --platform=%q but stage %q has --platform=%q", from, explicitPlatform, from, parent.platform)
				}
				stage.from = parent.from
			} else {
				// make sure to add ":latest" if it's implied
				stage.from = latestizeRepoTag(from)
			}

			i := len(stages)
			if len(args) == 2 && strings.ToUpper(args[0]) == "AS" {
				stageName = args[1]
				namedStages[stageName] = i
			}
			stages = append(stages, stage)

			meta.Froms = append(meta.Froms, stage.from)
			meta.Parents = append(meta.Parents, Parent{From: stage.from, Platform: stage.platform, Kind: "FROM"})

		case "COPY":
			for _, arg := range args {
				if !strings.HasPrefix(arg, "--") {
					// doesn't appear to be a "flag"; time to bail!
					break
				}
				from, ok := strings.CutPrefix(arg, "--from=")
				if !ok {
					// ignore any flags we're not interested in
					continue
				}

				resolved, isStage := resolveFrom(from)
				meta.Froms = append(meta.Froms, resolved)
				if !isStage {
					meta.Parents = append(meta.Parents, Parent{From: resolved, Kind: "COPY"})
				}
			}

		case "RUN": // TODO combine this and the above COPY-parsing code somehow sanely
			for _, arg := range args {
				if !strings.HasPrefix(arg, "--") {
					// doesn't appear to be a "flag"; time to bail!
					break
				}
				csv, ok := strings.CutPrefix(arg, "--mount=")
				if !ok {
					// ignore any flags we're not interested in
					continue
				}
				// TODO more correct CSV parsing
				fields := strings.Split(csv, ",")
				var mountType, from string
				for _, field := range fields {
					if val, ok := strings.CutPrefix(field, "type="); ok {
						mountType = val
						continue
					}
					if val, ok := strings.CutPrefix(field, "from="); ok {
						from = val
						continue
					}
				}
				if mountType != "bind" || from == "" {
					// this is probably something we should be worried about, but not something we're interested in parsing
					continue
				}

				resolved, isStage := resolveFrom(from)
				meta.Froms = append(meta.Froms, resolved)
				if !isStage {
					meta.Parents = append(meta.Parents, Parent{From: resolved, Kind: "RUN"})
				}
			}
		}
	}

	// TODO maybe we *shouldn't* support parsing a fully empty Dockerfile? 🤔 (we actively use an "empty" Dockerfile in the tests to test edge cases of continuation though that are otherwise hard to test, so it's probably ~fine)
	if len(stages) > 0 {
		finalStage := stages[len(stages)-1]
		switch finalStage.platform {
		case "":
			// yay, all is well (note: "$TARGETPLATFORM" is normalized to "" above, so this covers both)
		default:
			return meta, fmt.Errorf("final stage/FROM (%q) has --platform=%q but must be unspecified or $TARGETPLATFORM", finalStage.from, finalStage.platform)
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
