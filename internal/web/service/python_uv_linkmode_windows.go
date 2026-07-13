//go:build windows

package service

import (
	"fmt"
	"path/filepath"
	"strings"
)

func pathsSameFilesystem(first, second string) (bool, error) {
	firstAbs, err := filepath.Abs(first)
	if err != nil {
		return false, err
	}
	secondAbs, err := filepath.Abs(second)
	if err != nil {
		return false, err
	}
	firstVolume := filepath.VolumeName(firstAbs)
	secondVolume := filepath.VolumeName(secondAbs)
	if firstVolume == "" || secondVolume == "" {
		return false, fmt.Errorf("could not determine filesystem volumes")
	}
	return strings.EqualFold(firstVolume, secondVolume), nil
}
