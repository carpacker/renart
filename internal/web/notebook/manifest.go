package notebook

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// manifest is the parsed notebook.yml.
type manifest struct {
	ID     string
	Title  string
	Target string
	Blocks []Block
}

type manifestYAML struct {
	ID     string      `yaml:"id,omitempty"`
	Title  string      `yaml:"title,omitempty"`
	Target string      `yaml:"target,omitempty"`
	Blocks []blockYAML `yaml:"blocks"`
}

type blockYAML struct {
	Cell     string `yaml:"cell,omitempty"`
	Markdown string `yaml:"markdown,omitempty"`
}

func readManifest(filesystem afero.Fs, path string) (*manifest, error) {
	content, err := afero.ReadFile(filesystem, path)
	if err != nil {
		return nil, err
	}

	var parsed manifestYAML
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("invalid notebook manifest: %w", err)
	}

	result := &manifest{
		ID:     parsed.ID,
		Title:  parsed.Title,
		Target: parsed.Target,
		Blocks: make([]Block, 0, len(parsed.Blocks)),
	}
	for _, block := range parsed.Blocks {
		result.Blocks = append(result.Blocks, Block{Cell: block.Cell, Markdown: block.Markdown})
	}
	return result, nil
}

// MarshalManifest serializes a notebook's manifest deterministically: fixed
// key order, two-space indent, block order exactly as given — so saving an
// unchanged notebook produces a byte-identical file (no reordering noise in
// git diffs).
func MarshalManifest(nb *Notebook) ([]byte, error) {
	doc := manifestYAML{
		ID:     nb.UUID,
		Title:  nb.Title,
		Target: nb.Target,
		Blocks: make([]blockYAML, 0, len(nb.Blocks)),
	}
	// The folder name is the default title; don't persist it redundantly.
	if doc.Title == filepath.Base(nb.Dir) {
		doc.Title = ""
	}
	for _, block := range nb.Blocks {
		doc.Blocks = append(doc.Blocks, blockYAML{Cell: block.Cell, Markdown: block.Markdown})
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SaveManifest writes the notebook manifest, skipping the write when the
// on-disk content is already identical.
func SaveManifest(filesystem afero.Fs, nb *Notebook) error {
	content, err := MarshalManifest(nb)
	if err != nil {
		return err
	}

	path := filepath.Join(nb.Dir, ManifestFileName)
	if existing, readErr := afero.ReadFile(filesystem, path); readErr == nil && bytes.Equal(existing, content) {
		return nil
	}
	return afero.WriteFile(filesystem, path, content, 0o644)
}

// reconcileBlocks merges the manifest's ordered blocks with the cells found
// on disk: blocks referencing missing cells are dropped (with a problem
// note), and cells absent from the blocks are appended in filename order.
// Returns the effective block list and the cells ordered by appearance.
func reconcileBlocks(blocks []Block, cells []*Cell, problems *[]string) ([]Block, []*Cell) {
	cellByID := make(map[string]*Cell, len(cells))
	for _, cell := range cells {
		cellByID[cell.ID] = cell
	}

	resultBlocks := make([]Block, 0, len(blocks)+len(cells))
	orderedCells := make([]*Cell, 0, len(cells))
	referenced := make(map[string]bool, len(cells))

	for _, block := range blocks {
		if block.Cell == "" {
			resultBlocks = append(resultBlocks, block)
			continue
		}
		cell, ok := cellByID[block.Cell]
		if !ok {
			*problems = append(*problems, fmt.Sprintf("notebook.yml references unknown cell %q", block.Cell))
			continue
		}
		if referenced[block.Cell] {
			*problems = append(*problems, fmt.Sprintf("notebook.yml lists cell %q more than once", block.Cell))
			continue
		}
		referenced[block.Cell] = true
		resultBlocks = append(resultBlocks, block)
		orderedCells = append(orderedCells, cell)
	}

	unreferenced := make([]*Cell, 0)
	for _, cell := range cells {
		if !referenced[cell.ID] {
			unreferenced = append(unreferenced, cell)
		}
	}
	sort.Slice(unreferenced, func(i, j int) bool {
		return filepath.Base(unreferenced[i].Path) < filepath.Base(unreferenced[j].Path)
	})
	for _, cell := range unreferenced {
		resultBlocks = append(resultBlocks, Block{Cell: cell.ID})
		orderedCells = append(orderedCells, cell)
	}

	return resultBlocks, orderedCells
}
