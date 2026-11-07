package docker

import (
	"strings"

	"github.com/google/shlex"
)

func shellSplit(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	return shlex.Split(s)
}
