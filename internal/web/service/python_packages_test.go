package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRankPyPIMatchesPrioritisesExactPrefixAndAlias(t *testing.T) {
	names := []string{
		"RequestSoup", "requests", "requests-oauthlib", "requestsexceptions",
		"opencv-python", "opencv-contrib-python", "cv2", "numpy", "scikit-learn",
	}

	requests := rankPyPIMatches("requests", names)
	assert.Equal(t, "requests", requests[0], "exact match wins over noisy fuzzy hits")
	assert.Contains(t, requests, "requests-oauthlib", "prefix matches are included")

	// cv2 cannot fuzzy-match opencv-python, so the alias boost provides it first.
	cv2 := rankPyPIMatches("cv2", names)
	assert.Equal(t, "opencv-python", cv2[0])
	assert.Contains(t, cv2, "cv2")
}

func TestSearchPyPIPackagesAttachesSummaries(t *testing.T) {
	index := func(_ context.Context) ([]string, error) {
		return []string{"requests", "RequestSoup", "requests-oauthlib"}, nil
	}
	lookup := func(_ context.Context, name string) (*PyPIPackage, error) {
		if name == "requests" {
			return &PyPIPackage{Name: "requests", Summary: "Python HTTP for Humans.", Version: "2.31.0"}, nil
		}
		return nil, nil
	}

	packages := searchPyPIPackages(context.Background(), "requests", index, lookup)
	assert.NotEmpty(t, packages)
	assert.Equal(t, "requests", packages[0].Name)
	assert.Equal(t, "Python HTTP for Humans.", packages[0].Summary)
	// A package whose summary lookup failed is still returned by name.
	for _, pkg := range packages {
		assert.NotEmpty(t, pkg.Name)
	}
}

func TestSearchPyPIPackagesEmptyWhenIndexUnavailable(t *testing.T) {
	index := func(_ context.Context) ([]string, error) { return nil, assert.AnError }
	lookup := func(_ context.Context, _ string) (*PyPIPackage, error) { return nil, nil }
	assert.Empty(t, searchPyPIPackages(context.Background(), "requests", index, lookup))
}

func TestCanonicalPackageName(t *testing.T) {
	assert.Equal(t, "scikit-learn", canonicalPackageName("scikit_learn"))
	assert.Equal(t, "scikit-learn", canonicalPackageName("Scikit.Learn"))
	assert.Equal(t, "requests", canonicalPackageName("  Requests  "))
}
