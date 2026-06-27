package notebook

import (
	"fmt"
	"regexp"
	"strings"
)

var cellNamePattern = regexp.MustCompile(`^\w+$`)

// validate appends structural problems to the notebook: duplicate or
// invalid cell names and dependency cycles. Problems are surfaced in the
// UI, not fatal — a broken notebook still loads so it can be fixed there.
func validate(nb *Notebook) {
	seen := make(map[string]string, len(nb.Cells))
	for _, cell := range nb.Cells {
		name := cell.Asset.Name
		lower := strings.ToLower(name)
		if other, ok := seen[lower]; ok {
			nb.Problems = append(nb.Problems, fmt.Sprintf("duplicate cell name %q (also %q)", name, other))
		} else {
			seen[lower] = name
		}
		if !cellNamePattern.MatchString(name) {
			nb.Problems = append(nb.Problems, fmt.Sprintf("cell name %q contains characters outside [A-Za-z0-9_]", name))
		}
	}

	for _, cycle := range findCycles(nb) {
		nb.Problems = append(nb.Problems, "dependency cycle: "+strings.Join(cycle, " → "))
	}
}

// findCycles runs a DFS over the cell dependency edges (upstream names
// restricted to sibling cells) and returns each cycle found, as a name path.
func findCycles(nb *Notebook) [][]string {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)

	state := make(map[string]int, len(nb.Cells))
	cycles := make([][]string, 0)
	stack := make([]string, 0, len(nb.Cells))

	var visit func(cell *Cell)
	visit = func(cell *Cell) {
		key := strings.ToLower(cell.Asset.Name)
		state[key] = visiting
		stack = append(stack, cell.Asset.Name)

		for _, upstream := range cell.Asset.Upstreams {
			sibling := nb.CellByName(upstream.Value)
			if sibling == nil {
				continue
			}
			siblingKey := strings.ToLower(sibling.Asset.Name)
			switch state[siblingKey] {
			case visiting:
				// Found a cycle: slice the stack from the sibling onward.
				start := 0
				for index, name := range stack {
					if strings.EqualFold(name, sibling.Asset.Name) {
						start = index
						break
					}
				}
				cycle := append(append([]string{}, stack[start:]...), sibling.Asset.Name)
				cycles = append(cycles, cycle)
			case unvisited:
				visit(sibling)
			}
		}

		stack = stack[:len(stack)-1]
		state[key] = done
	}

	for _, cell := range nb.Cells {
		if state[strings.ToLower(cell.Asset.Name)] == unvisited {
			visit(cell)
		}
	}
	return cycles
}
