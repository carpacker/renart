package notebook

import "strings"

// TopoOrder returns the notebook's cells in dependency order (upstreams
// first). Cells in cycles are appended at the end in block order — the
// cycle itself is already reported as a validation problem.
func TopoOrder(nb *Notebook) []*Cell {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)

	state := make(map[string]int, len(nb.Cells))
	ordered := make([]*Cell, 0, len(nb.Cells))

	var visit func(cell *Cell)
	visit = func(cell *Cell) {
		key := strings.ToLower(cell.Asset.Name)
		if state[key] != unvisited {
			return
		}
		state[key] = visiting
		for _, upstream := range cell.Asset.Upstreams {
			if sibling := nb.CellByName(upstream.Value); sibling != nil {
				if state[strings.ToLower(sibling.Asset.Name)] == unvisited {
					visit(sibling)
				}
			}
		}
		state[key] = done
		ordered = append(ordered, cell)
	}

	for _, cell := range nb.Cells {
		visit(cell)
	}
	return ordered
}

// Ancestors returns the transitive upstream cells of the given cell, in
// dependency order (furthest upstream first), excluding the cell itself.
func Ancestors(nb *Notebook, cell *Cell) []*Cell {
	wanted := map[string]bool{}
	var mark func(c *Cell)
	mark = func(c *Cell) {
		for _, upstream := range c.Asset.Upstreams {
			sibling := nb.CellByName(upstream.Value)
			if sibling == nil {
				continue
			}
			key := strings.ToLower(sibling.Asset.Name)
			if wanted[key] {
				continue
			}
			wanted[key] = true
			mark(sibling)
		}
	}
	mark(cell)

	result := make([]*Cell, 0, len(wanted))
	for _, candidate := range TopoOrder(nb) {
		if wanted[strings.ToLower(candidate.Asset.Name)] {
			result = append(result, candidate)
		}
	}
	return result
}

// Descendants returns the transitive downstream cells of the given cell, in
// dependency order, excluding the cell itself. Used by run-from-here.
func Descendants(nb *Notebook, cell *Cell) []*Cell {
	downstream := map[string][]*Cell{}
	for _, candidate := range nb.Cells {
		for _, upstream := range candidate.Asset.Upstreams {
			if sibling := nb.CellByName(upstream.Value); sibling != nil {
				key := strings.ToLower(sibling.Asset.Name)
				downstream[key] = append(downstream[key], candidate)
			}
		}
	}

	wanted := map[string]bool{}
	var mark func(c *Cell)
	mark = func(c *Cell) {
		for _, child := range downstream[strings.ToLower(c.Asset.Name)] {
			key := strings.ToLower(child.Asset.Name)
			if wanted[key] {
				continue
			}
			wanted[key] = true
			mark(child)
		}
	}
	mark(cell)

	result := make([]*Cell, 0, len(wanted))
	for _, candidate := range TopoOrder(nb) {
		if wanted[strings.ToLower(candidate.Asset.Name)] {
			result = append(result, candidate)
		}
	}
	return result
}
