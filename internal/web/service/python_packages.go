package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sahilm/fuzzy"
)

// PyPIPackage is a candidate PyPI package for an import name.
type PyPIPackage struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Version string `json:"version"`
}

// importPackageAliases maps import names to the PyPI package(s) that provide
// them when the names differ (fuzzy search alone cannot bridge cv2 ->
// opencv-python). These are boosted to the top of the results.
var importPackageAliases = map[string][]string{
	"bs4":      {"beautifulsoup4"},
	"cv2":      {"opencv-python"},
	"dateutil": {"python-dateutil"},
	"dotenv":   {"python-dotenv"},
	"jwt":      {"PyJWT"},
	"PIL":      {"Pillow"},
	"sklearn":  {"scikit-learn"},
	"yaml":     {"PyYAML"},
}

const pypiSearchLimit = 8

// pypiLookupFunc returns metadata for an exact PyPI package name, or nil when it
// does not exist.
type pypiLookupFunc func(ctx context.Context, name string) (*PyPIPackage, error)

// pypiIndexFunc returns the full list of PyPI package names.
type pypiIndexFunc func(ctx context.Context) ([]string, error)

// SearchPyPIPackages returns probable PyPI packages for an import name by fuzzy
// searching the cached PyPI index, then fetching each candidate's summary.
// Unreachable PyPI / index degrades to an empty list.
func SearchPyPIPackages(ctx context.Context, importName string) []PyPIPackage {
	return searchPyPIPackages(ctx, importName, pypiIndex.ensure, fetchPyPIPackage)
}

func searchPyPIPackages(ctx context.Context, importName string, index pypiIndexFunc, lookup pypiLookupFunc) []PyPIPackage {
	importName = strings.TrimSpace(importName)
	if importName == "" {
		return nil
	}
	names, err := index(ctx)
	if err != nil || len(names) == 0 {
		return nil
	}

	ranked := rankPyPIMatches(importName, names)
	if len(ranked) == 0 {
		return nil
	}

	// Fetch summaries in parallel, preserving rank order. A failed lookup still
	// yields the name (descriptions are best-effort).
	summaries := make([]*PyPIPackage, len(ranked))
	var wg sync.WaitGroup
	for i, name := range ranked {
		wg.Add(1)
		go func(index int, pkg string) {
			defer wg.Done()
			if info, lookupErr := lookup(ctx, pkg); lookupErr == nil && info != nil {
				summaries[index] = info
			}
		}(i, name)
	}
	wg.Wait()

	packages := make([]PyPIPackage, 0, len(ranked))
	for i, name := range ranked {
		if summaries[i] != nil {
			packages = append(packages, *summaries[i])
			continue
		}
		packages = append(packages, PyPIPackage{Name: name})
	}
	return packages
}

// rankPyPIMatches fuzzy-searches the index for an import name and returns the
// best package names, ordered: curated aliases, then exact, prefix, substring,
// and remaining fuzzy matches.
func rankPyPIMatches(importName string, names []string) []string {
	result := make([]string, 0, pypiSearchLimit)
	seen := map[string]bool{}
	add := func(name string) bool {
		key := canonicalPackageName(name)
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
		result = append(result, name)
		return len(result) >= pypiSearchLimit
	}

	for _, alias := range importPackageAliases[importName] {
		if add(alias) {
			return result
		}
	}

	query := canonicalPackageName(importName)
	var exact, prefix, substring, other []string
	for _, match := range fuzzy.Find(importName, names) {
		canonical := canonicalPackageName(match.Str)
		switch {
		case canonical == query:
			exact = append(exact, match.Str)
		case strings.HasPrefix(canonical, query):
			prefix = append(prefix, match.Str)
		case strings.Contains(canonical, query):
			substring = append(substring, match.Str)
		default:
			other = append(other, match.Str)
		}
	}
	for _, group := range [][]string{exact, prefix, substring, other} {
		for _, name := range group {
			if add(name) {
				return result
			}
		}
	}
	return result
}

// canonicalPackageName normalizes per PEP 503 so "scikit_learn" == "scikit-learn".
func canonicalPackageName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		if r == '-' || r == '_' || r == '.' {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
			continue
		}
		b.WriteRune(r)
		prevDash = false
	}
	return strings.Trim(b.String(), "-")
}

var pypiHTTPClient = &http.Client{Timeout: 6 * time.Second}

func fetchPyPIPackage(ctx context.Context, name string) (*PyPIPackage, error) {
	url := fmt.Sprintf("https://pypi.org/pypi/%s/json", name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "renart-notebook")
	resp, err := pypiHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var payload struct {
		Info struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.Info.Name) == "" {
		return nil, nil
	}
	return &PyPIPackage{
		Name:    payload.Info.Name,
		Summary: strings.TrimSpace(payload.Info.Summary),
		Version: payload.Info.Version,
	}, nil
}
