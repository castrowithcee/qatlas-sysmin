package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
)

// Docker Hub is addressed by its API host in image references but keyed by this URL in a docker config.
const (
	dockerHubAPIHost = "registry-1.docker.io"
	dockerHubAuthKey = "https://index.docker.io/v1/"
)

// dockerAuthRunner gives every docker call the registry logins of the project's approved credentials. The
// docker config lives in a private temporary directory for the duration of one call only, so no secret
// outlives the command. DOCKER_CONFIG replaces any value of the service environment: the vault alone is
// authoritative, a login of the service user is never used or merged.
type dockerAuthRunner struct {
	inner execute.Runner
	// auths maps the docker config key of a registry to base64(user:token).
	auths map[string]string
}

// dockerAuths derives the config entries of the images that have a credential. Two credentials for one
// registry host cannot both apply, so that is refused (the validator reports it earlier).
func dockerAuths(images []config.Image, creds *approvedCredentials) (map[string]string, error) {
	auths := map[string]string{}
	owner := map[string]string{}
	for _, img := range images {
		if img.Credential == "" {
			continue
		}
		repo, err := registry.ParseRepo(img.Ref)
		if err != nil {
			return nil, errors.New("the image reference is not valid")
		}
		key := repo.Host
		if key == dockerHubAPIHost {
			key = dockerHubAuthKey
		}
		if prev, ok := owner[key]; ok && prev != img.Credential {
			return nil, errors.New("images of one registry use different credentials")
		}
		cred, err := creds.Registry(img.Credential)
		if err != nil {
			return nil, errors.New("the registry credential cannot be used")
		}
		owner[key] = img.Credential
		auths[key] = base64.StdEncoding.EncodeToString([]byte(cred.User + ":" + cred.Token))
	}
	return auths, nil
}

func (r *dockerAuthRunner) Run(ctx context.Context, c execute.Command) (execute.Result, error) {
	if len(r.auths) == 0 || c.Name != "docker" {
		return r.inner.Run(ctx, c)
	}
	dir, err := os.MkdirTemp("", "sysmin-*")
	if err != nil {
		return execute.Result{}, errors.New("cannot prepare the registry login")
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return execute.Result{}, errors.New("cannot prepare the registry login")
	}
	entries := map[string]map[string]string{}
	for host, auth := range r.auths {
		entries[host] = map[string]string{"auth": auth}
	}
	data, err := json.Marshal(map[string]any{"auths": entries})
	if err != nil {
		return execute.Result{}, errors.New("cannot prepare the registry login")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		return execute.Result{}, errors.New("cannot prepare the registry login")
	}
	// The last entry wins over a DOCKER_CONFIG that the process environment holds.
	c.Env = append(slices.Clone(c.Env), "DOCKER_CONFIG="+dir)
	return r.inner.Run(ctx, c)
}
