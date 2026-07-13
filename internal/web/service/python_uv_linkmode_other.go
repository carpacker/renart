//go:build !unix && !windows

package service

import "errors"

func pathsSameFilesystem(_, _ string) (bool, error) {
	return false, errors.New("filesystem identity is not supported on this platform")
}
