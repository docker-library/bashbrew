package registry

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	iofs "io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/containerd/containerd/remotes"
	dockerremote "github.com/containerd/containerd/remotes/docker"
)

// given a registry hostname, return the "user:pass" string decoded from base64 in ~/.docker/config.json
func lookupDockerAuthCredentials(registry string) (usernameColonPassword string, err error) {
	dockerConfigDir := os.Getenv("DOCKER_CONFIG")
	if dockerConfigDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dockerConfigDir = filepath.Join(home, ".docker")
	}
	dockerConfigFile := filepath.Join(dockerConfigDir, "config.json")

	file, err := os.Open(dockerConfigFile)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			err = nil
		}
		return "", err
	}
	defer file.Close()

	dockerConfig := struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}{}
	err = json.NewDecoder(file).Decode(&dockerConfig)
	if err != nil {
		return "", err
	}

	hosts := []string{registry}
	switch registry {
	case "docker.io", "index.docker.io", "":
		hosts = []string{"docker.io", "index.docker.io"}
	}

	for _, host := range hosts {
		// teeeeeeeeeeeechnically, this should loop over the keys in dockerConfig.Auths and strip the "https://" prefix from each and the "/*" suffix from each and then match on that, but what's the point of a map if you can't get a fast lookup?? (https://github.com/moby/moby/blob/34b56728ed7101c6b3cc0405f5fd6351073a8253/registry/auth.go#L202-L235)
		for _, check := range []string{host, "https://" + host + "/v1/"} {
			if authObj, ok := dockerConfig.Auths[check]; ok {
				if base64val := authObj.Auth; base64val != "" {
					rawVal, err := base64.StdEncoding.DecodeString(base64val)
					if err != nil {
						return "", err
					}
					return string(rawVal), nil
				}
			}
		}
	}

	return "", nil
}

var (
	resolver     remotes.Resolver
	resolverOnce sync.Once
)

// returns a containerd "Resolver" suitable for interacting with registries (that will transparently honor DOCKERHUB_PUBLIC_PROXY for read-only lookups *and* deal with looking up credentials from ~/.docker/config.json)
func NewDockerAuthResolver() remotes.Resolver {
	resolverOnce.Do(func() {
		resolver = dockerremote.NewResolver(dockerremote.ResolverOptions{
			Hosts: func(domain string) ([]dockerremote.RegistryHost, error) {
				// https://github.com/containerd/containerd/blob/v1.6.10/remotes/docker/registry.go#L152-L198
				config := dockerremote.RegistryHost{
					Host:         domain,
					Scheme:       "https",
					Path:         "/v2",
					Capabilities: dockerremote.HostCapabilityPull | dockerremote.HostCapabilityResolve | dockerremote.HostCapabilityPush,
					Authorizer: dockerremote.NewDockerAuthorizer(dockerremote.WithAuthCreds(func(_ string) (string, string, error) {
						usernameColonPassword, err := lookupDockerAuthCredentials(domain)
						if err != nil {
							return "", "", err
						}
						username, password, _ := strings.Cut(usernameColonPassword, ":")
						return username, password, nil
					})),
				}
				if domain == "docker.io" {
					// https://github.com/containerd/containerd/blob/v1.6.10/remotes/docker/registry.go#L193
					config.Host = "registry-1.docker.io"

					if publicProxy := os.Getenv("DOCKERHUB_PUBLIC_PROXY"); publicProxy != "" {
						publicProxyURL, err := url.Parse(publicProxy)
						if err != nil {
							return nil, err
						}
						proxyConfig := dockerremote.RegistryHost{
							Host:         publicProxyURL.Host,
							Scheme:       publicProxyURL.Scheme,
							Path:         path.Join(publicProxyURL.Path, config.Path),
							Capabilities: dockerremote.HostCapabilityPull | dockerremote.HostCapabilityResolve,
						}
						return []dockerremote.RegistryHost{
							proxyConfig,
							config,
						}, nil
					}
				} else if strings.Contains(domain, "localhost") {
					config.Scheme = "http"
				}
				return []dockerremote.RegistryHost{config}, nil
			},
		})
	})
	return resolver
}
