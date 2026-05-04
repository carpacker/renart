package service

import (
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

func assetContentHasExplicitName(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "name:") {
			return true
		}
	}
	return false
}

func removePersistedAssetNameField(asset *pipeline.Asset) error {
	path := asset.ExecutableFile.Path
	if strings.TrimSpace(path) == "" {
		path = asset.DefinitionFile.Path
	}
	if strings.TrimSpace(path) == "" {
		return nil
	}
	fs := afero.NewOsFs()
	contentBytes, err := afero.ReadFile(fs, path)
	if err != nil {
		return err
	}
	content := removeAssetNameFieldFromContent(string(contentBytes))
	return afero.WriteFile(fs, path, []byte(content), 0o644)
}

func removeAssetNameFieldFromContent(content string) string {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "name:") {
			lines = append(lines[:index], lines[index+1:]...)
			return strings.Join(lines, "\n")
		}
	}
	return content
}

func assetPathForInferredName(assetName, extension string) string {
	parts := strings.Split(strings.TrimSpace(assetName), ".")
	pathParts := make([]string, 0, len(parts)+1)
	pathParts = append(pathParts, "assets")
	for _, part := range parts {
		pathParts = append(pathParts, SlugUnderscore(part))
	}
	return filepath.ToSlash(filepath.Join(append(pathParts[:len(pathParts)-1], pathParts[len(pathParts)-1]+extension)...))
}

func assetNameLeafPath(assetName string) string {
	parts := strings.Split(strings.TrimSpace(assetName), ".")
	return SlugUnderscore(parts[len(parts)-1])
}

func inferredAssetNameFromPath(relAssetPath string) string {
	clean := filepath.ToSlash(filepath.Clean(relAssetPath))
	parts := strings.Split(clean, "/")
	assetsIndex := -1
	for index, part := range parts {
		if part == "assets" {
			assetsIndex = index
			break
		}
	}
	if assetsIndex < 0 || assetsIndex >= len(parts)-1 {
		return ""
	}
	nameParts := append([]string(nil), parts[assetsIndex+1:]...)
	nameParts[len(nameParts)-1] = trimBruinAssetExtension(nameParts[len(nameParts)-1])
	return strings.Join(nameParts, ".")
}

func trimBruinAssetExtension(fileName string) string {
	for _, suffix := range pipeline.SupportedFileSuffixes {
		if strings.HasSuffix(strings.ToLower(fileName), strings.ToLower(suffix)) {
			return strings.TrimSuffix(fileName[:len(fileName)-len(suffix)], ".")
		}
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName))
}

func pipelineRelPathForAsset(relAssetPath string) string {
	clean := filepath.ToSlash(filepath.Clean(relAssetPath))
	parts := strings.Split(clean, "/")
	for index, part := range parts {
		if part == "assets" {
			if index == 0 {
				return "."
			}
			return filepath.ToSlash(filepath.Join(parts[:index]...))
		}
	}
	return filepath.ToSlash(filepath.Dir(filepath.Dir(clean)))
}
