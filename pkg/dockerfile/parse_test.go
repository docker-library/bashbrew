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
		wantErr    bool
	}{
		{
			dockerfile: `FROM scratch`,
			metadata: dockerfile.Metadata{
				Froms:   []string{"scratch"},
				Parents: []dockerfile.Parent{{From: "scratch", Kind: "FROM"}},
			},
		},
		{
			dockerfile: `from bash`,
			metadata: dockerfile.Metadata{
				Froms:   []string{"bash:latest"},
				Parents: []dockerfile.Parent{{From: "bash:latest", Kind: "FROM"}},
			},
		},
		{
			dockerfile: `fRoM bash:5`,
			metadata: dockerfile.Metadata{
				Froms:   []string{"bash:5"},
				Parents: []dockerfile.Parent{{From: "bash:5", Kind: "FROM"}},
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
				Parents: []dockerfile.Parent{
					{From: "scratch", Kind: "FROM"},
					{From: "bash:latest", Kind: "FROM"},
				},
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
				COPY --from=hello-world /hello /usr/local/bin/
			`,
			metadata: dockerfile.Metadata{
				Froms: []string{"bash:latest", "busybox:uclibc", "bash:5", "bash:latest", "scratch", "bash:latest", "bash:5", "bash:latest", "hello-world:latest"},
				Parents: []dockerfile.Parent{
					// one per FROM (including "foo2", resolved to "foo"'s underlying image)
					{From: "bash:latest", Kind: "FROM"},
					{From: "busybox:uclibc", Kind: "FROM"},
					{From: "bash:5", Kind: "FROM"},
					{From: "bash:latest", Kind: "FROM"},
					{From: "scratch", Kind: "FROM"},
					// COPY --from=foo/bar/foo2 all reference local stages, already represented above -- no Parent
					// COPY --chown=... has no --from= at all
					{From: "hello-world:latest", Kind: "COPY"},
				},
			},
		},
		{
			name: "empty continuations",
			dockerfile: `
				\
				\
				\
				\
				\
				\
			`,
		},
		{
			name: "continuation edge cases",
			dockerfile: `
				# continuation does not apply to this comment \
				FROM scratch
				# but everything below this is part of a single continuation

				FROM\

				\     
				\      

				\     
				\    

				# comments inside are fine
				# and really yucky empty lines:
							     						
				\
				\

				scratch\
			`,
			metadata: dockerfile.Metadata{
				Froms: []string{"scratch", "scratch"},
				Parents: []dockerfile.Parent{
					{From: "scratch", Kind: "FROM"},
					{From: "scratch", Kind: "FROM"},
				},
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
				Froms: []string{"bash:latest", "scratch", "bash:latest", "scratch", "scratch", "bash:latest", "scratch"},
				Parents: []dockerfile.Parent{
					// COPY/RUN --from=<number> all reference local stages, already represented here -- no separate Parent
					{From: "bash:latest", Kind: "FROM"},
					{From: "scratch", Kind: "FROM"},
					{From: "scratch", Kind: "FROM"},
					{From: "bash:latest", Kind: "FROM"},
				},
			},
		},
		{
			name: "RUN --mount",
			dockerfile: `
				FROM scratch
				RUN --mount=type=bind,from=busybox:uclibc,target=/tmp ["/tmp/bin/sh","-euxc","echo foo > /foo"]
			`,
			metadata: dockerfile.Metadata{
				Froms: []string{"scratch", "busybox:uclibc"},
				Parents: []dockerfile.Parent{
					{From: "scratch", Kind: "FROM"},
					{From: "busybox:uclibc", Kind: "RUN"},
				},
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
				Froms: []string{"busybox:uclibc", "scratch", "busybox:uclibc"},
				Parents: []dockerfile.Parent{
					// RUN --mount=...,from=bb references a local stage, already represented here -- no separate Parent
					{From: "busybox:uclibc", Kind: "FROM"},
					{From: "scratch", Kind: "FROM"},
				},
			},
		},
		{
			name: "FROM --platform",
			dockerfile: `
				FROM --platform=$BUILDPLATFORM golang AS build
				RUN do some stuff
				FROM --platform=$TARGETPLATFORM debian
				COPY --from=build /some/binary /some/other/place
			`,
			metadata: dockerfile.Metadata{
				Froms: []string{"golang:latest", "debian:latest", "golang:latest"},
				Parents: []dockerfile.Parent{
					{From: "golang:latest", Platform: "$BUILDPLATFORM", Kind: "FROM"},
					// explicit "$TARGETPLATFORM" is normalized to "" -- semantically identical, no reason to make callers treat two values as equivalent
					{From: "debian:latest", Kind: "FROM"},
					// COPY --from=build references a local stage, already represented above -- no separate Parent
				},
			},
		},
		{
			name: "FROM --platform mismatch with referenced stage",
			dockerfile: `
				FROM --platform=$BUILDPLATFORM golang AS build
				FROM --platform=$TARGETPLATFORM build
			`,
			wantErr: true,
		},
	} {
		td := td
		// some light normalization
		if td.name == "" {
			td.name = td.dockerfile
		}
		t.Run(td.name, func(t *testing.T) {
			parsed, err := dockerfile.Parse(td.dockerfile)
			if td.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got:\n%#v", parsed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(parsed, td.metadata) {
				t.Fatalf("expected:\n%#v\ngot:\n%#v", td.metadata, parsed)
			}
		})
	}
}
