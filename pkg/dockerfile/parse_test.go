package dockerfile_test

import (
	"reflect"
	"testing"

	"github.com/docker-library/bashbrew/pkg/dockerfile"
)

func TestParse(t *testing.T) {
	for _, td := range []struct {
		name       string
		dockerfile string
		metadata   dockerfile.Metadata
	}{
		{
			dockerfile: `FROM scratch`,
			metadata: dockerfile.Metadata{
				Froms: []string{"scratch"},
			},
		},
		{
			dockerfile: `from bash`,
			metadata: dockerfile.Metadata{
				Froms: []string{"bash:latest"},
			},
		},
		{
			dockerfile: `fRoM bash:5`,
			metadata: dockerfile.Metadata{
				Froms: []string{"bash:5"},
			},
		},
		{
			name: "comments+whitespace+continuation",
			dockerfile: `
				FROM scratch

				# foo

				# bar

				FROM bash
				RUN echo \
				# comment inside continuation
					hello \
					world
			`,
			metadata: dockerfile.Metadata{
				Froms: []string{"scratch", "bash:latest"},
			},
		},
		{
			name: "multi-stage",
			dockerfile: `
				FROM bash:latest AS foo
				FROM busybox:uclibc
				# intermediate stage without name
				FROM bash:5 AS bar
				FROM foo AS foo2
				FROM scratch
				COPY --from=foo / /
				COPY --from=bar / /
				COPY --from=foo2 / /
				COPY --chown=1234:5678 /foo /bar
			`,
			metadata: dockerfile.Metadata{
				StageFroms: []string{"bash:latest", "busybox:uclibc", "bash:5", "bash:latest", "scratch"},
				StageNames: []string{"foo", "bar", "foo2"},
				StageNameFroms: map[string]string{
					"foo":  "bash:latest",
					"bar":  "bash:5",
					"foo2": "bash:latest",
				},
				Froms: []string{"bash:latest", "busybox:uclibc", "bash:5", "bash:latest", "scratch", "bash:latest", "bash:5", "bash:latest"},
			},
		},
		{
			// TODO is this even something that's supported by classic builder/buildkit? (Tianon *thinks* it was supported once, but maybe he's misremembering and it's never been a thing Dockerfiles, only docker build --target=N ?)
			name: "numbered stages",
			dockerfile: `
				FROM bash:latest
				RUN echo foo > /foo
				FROM scratch
				COPY --from=0 /foo /foo
				FROM scratch
				COPY --chown=1234:5678 --from=1 /foo /foo
				FROM bash:latest
				RUN --mount=type=bind,from=2 cat /foo
			`,
			metadata: dockerfile.Metadata{
				StageFroms: []string{"bash:latest", "scratch", "scratch", "bash:latest"},
				Froms:      []string{"bash:latest", "scratch", "bash:latest", "scratch", "scratch", "bash:latest", "scratch"},
			},
		},
		{
			name: "RUN --mount",
			dockerfile: `
				FROM scratch
				RUN --mount=type=bind,from=busybox:uclibc,target=/tmp ["/tmp/bin/sh","-euxc","echo foo > /foo"]
			`,
			metadata: dockerfile.Metadata{
				StageFroms: []string{"scratch"},
				Froms:      []string{"scratch", "busybox:uclibc"},
			},
		},
		{
			name: "RUN --mount=stage",
			dockerfile: `
				FROM busybox:uclibc AS bb
				RUN --network=none echo hi, a flag that is ignored
				RUN --mount=type=tmpfs,dst=/foo touch /foo/bar # this should be ignored
				FROM scratch
				RUN --mount=type=bind,from=bb,target=/tmp ["/tmp/bin/sh","-euxc","echo foo > /foo"]
			`,
			metadata: dockerfile.Metadata{
				StageFroms:     []string{"busybox:uclibc", "scratch"},
				StageNames:     []string{"bb"},
				StageNameFroms: map[string]string{"bb": "busybox:uclibc"},
				Froms:          []string{"busybox:uclibc", "scratch", "busybox:uclibc"},
			},
		},
	} {
		td := td
		// some light normalization
		if td.name == "" {
			td.name = td.dockerfile
		}
		if len(td.metadata.Froms) > 0 && len(td.metadata.StageFroms) == 0 {
			td.metadata.StageFroms = td.metadata.Froms
		}
		if td.metadata.StageNameFroms == nil {
			td.metadata.StageNameFroms = map[string]string{}
		}
		t.Run(td.name, func(t *testing.T) {
			parsed, err := dockerfile.Parse(td.dockerfile)
			if err != nil {
				t.Fatal(err)
			}

			if parsed == nil {
				t.Fatalf("expected:\n%#v\ngot:\n%#v", td.metadata, parsed)
			}

			if !reflect.DeepEqual(*parsed, td.metadata) {
				t.Fatalf("expected:\n%#v\ngot:\n%#v", td.metadata, *parsed)
			}
		})
	}
}
