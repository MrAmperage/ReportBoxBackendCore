// Package app defines service metadata.
package app

import (
	"errors"
	"strings"
)

// Info identifies a service and its build.
type Info struct{ Name, Version, Commit string }

// Normalize validates the service name and supplies development build metadata.
func (i Info) Normalize() (Info, error) {
	i.Name = strings.TrimSpace(i.Name)
	if i.Name == "" {
		return Info{}, errors.New("service name is required")
	}
	if strings.TrimSpace(i.Version) == "" {
		i.Version = "dev"
	}
	if strings.TrimSpace(i.Commit) == "" {
		i.Commit = "unknown"
	}
	return i, nil
}
