package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/bgentry/go-netrc/netrc"
)

type netrcTransport struct {
	http.Transport
}

func (t *netrcTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	t.addCredentials(req)
	return t.Transport.RoundTrip(req)
}

func (t *netrcTransport) addCredentials(req *http.Request) (added bool) {
	path, err := netrcPath()
	if err != nil {
		return false
	}

	if debugFlag {
		fmt.Printf("Found netrc file at: %s\n", path)
	}
	machine, err := netrc.FindMachine(path, req.URL.Host)
	if err != nil {
		return false
	}

	if debugFlag {
		fmt.Printf("Found netrc credentials for %s\n", machine.Name)
	}
	req.SetBasicAuth(machine.Login, machine.Password)
	return true
}

func netrcPath() (string, error) {
	if env := os.Getenv("NETRC"); env != "" {
		return env, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := ".netrc"
	if runtime.GOOS == "windows" {
		base = "_netrc"
	}
	return filepath.Join(dir, base), nil
}
