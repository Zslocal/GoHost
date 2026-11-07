package docker

import (
	"strings"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"
)

func volumeCreateOptions(name string) volume.CreateOptions {
	return volume.CreateOptions{Name: name, Labels: map[string]string{"dockerview.managed": "true"}}
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if errdefs.IsConflict(err) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "already exists")
}
