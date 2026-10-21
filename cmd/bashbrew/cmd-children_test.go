package main

import "testing"

func TestRepositoryFromImageReference(t *testing.T) {
	tests := []struct {
		reference  string
		repository string
		ok         bool
	}{
		{"alpine:3.20", "alpine", true},
		{"registry.example:5000/team/base:latest", "registry.example:5000/team/base", true},
		{"registry.example:5000/team/base@sha256:123456", "registry.example:5000/team/base", true},
		{"registry.example:5000/team/base:3@sha256:123456", "registry.example:5000/team/base", true},
		{"scratch", "scratch", false},
	}

	for _, test := range tests {
		t.Run(test.reference, func(t *testing.T) {
			repository, ok := repositoryFromImageReference(test.reference)
			if repository != test.repository || ok != test.ok {
				t.Fatalf("repositoryFromImageReference(%q) = %q, %t; want %q, %t", test.reference, repository, ok, test.repository, test.ok)
			}
		})
	}
}
