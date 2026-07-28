package executiongraph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Node struct {
	Position     int
	Dependencies []int
	Connections  []string
	Resources    []string
	Exclusive    bool
}

type BypassStatus string

const (
	BypassSkipped   BypassStatus = "skipped"
	BypassCancelled BypassStatus = "cancelled"
)

type Bypass struct {
	Node      Node
	Status    BypassStatus
	BlockedBy []int
	Err       error
}

type Runner struct {
	MaxActive        int
	Workspace        *Budget
	ConnectionLimits map[string]int
	OnBypass         func(Bypass) error
}

type nodeStatus uint8

const (
	nodePending nodeStatus = iota
	nodeRunning
	nodeSucceeded
	nodeFailed
	nodeSkipped
	nodeCancelled
)

type nodeResult struct {
	position int
	err      error
}

type reservations struct {
	connections []string
	resources   []string
	exclusive   bool
}

func (r Runner) Run(ctx context.Context, nodes []Node, run func(context.Context, Node) error) error {
	if run == nil {
		return errNilRun
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateNodes(nodes); err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	nodes = canonicalNodes(nodes)
	maxActive := r.MaxActive
	if maxActive < 1 {
		maxActive = 1
	}
	if maxActive > len(nodes) {
		maxActive = len(nodes)
	}
	connectionLimits := canonicalConnectionLimits(r.ConnectionLimits)

	statuses := make([]nodeStatus, len(nodes))
	activeConnections := make(map[string]int)
	activeResources := make(map[string]int)
	active := 0
	exclusiveActive := false
	results := make(chan nodeResult, len(nodes))
	reserved := make(map[int]reservations, len(nodes))
	remaining := len(nodes)
	var runErrors []error

	for remaining > 0 {
		if ctx.Err() != nil {
			for position := range nodes {
				if statuses[position] != nodePending {
					continue
				}
				statuses[position] = nodeCancelled
				remaining--
				if err := r.bypass(Bypass{
					Node: nodes[position], Status: BypassCancelled, Err: ctx.Err(),
				}); err != nil {
					runErrors = append(runErrors, err)
				}
			}
		} else {
			for {
				progressed := false
				for position := range nodes {
					if statuses[position] != nodePending {
						continue
					}
					blockedBy := failedDependencies(nodes[position], statuses)
					if len(blockedBy) == 0 {
						continue
					}
					statuses[position] = nodeSkipped
					remaining--
					progressed = true
					err := fmt.Errorf("dependency execution failed")
					if callbackErr := r.bypass(Bypass{
						Node: nodes[position], Status: BypassSkipped,
						BlockedBy: blockedBy, Err: err,
					}); callbackErr != nil {
						runErrors = append(runErrors, callbackErr)
					}
				}
				if !progressed {
					break
				}
			}
		}

		launched := false
		if ctx.Err() == nil {
			for active < maxActive {
				position := nextRunnableNode(
					nodes,
					statuses,
					active,
					exclusiveActive,
					activeConnections,
					connectionLimits,
					activeResources,
				)
				if position < 0 {
					break
				}
				lease, err := r.Workspace.Acquire(ctx)
				if err != nil {
					break
				}
				node := nodes[position]
				reservation := reserveNode(node, activeConnections, activeResources)
				reserved[position] = reservation
				statuses[position] = nodeRunning
				active++
				exclusiveActive = exclusiveActive || node.Exclusive
				launched = true
				go executeNode(ctx, node, lease, run, results)
			}
		}

		if active == 0 {
			if remaining == 0 {
				break
			}
			if ctx.Err() != nil {
				break
			}
			if !launched {
				return errors.Join(
					append(runErrors, errors.New("execution graph stalled with no runnable units"))...,
				)
			}
		}
		if active == 0 {
			continue
		}

		result := <-results
		if result.position < 0 || result.position >= len(nodes) || statuses[result.position] != nodeRunning {
			runErrors = append(runErrors, fmt.Errorf("execution graph received invalid result for unit %d", result.position))
			continue
		}
		releaseNode(reserved[result.position], activeConnections, activeResources)
		delete(reserved, result.position)
		active--
		exclusiveActive = false
		for position, reservation := range reserved {
			if statuses[position] == nodeRunning && reservation.exclusive {
				exclusiveActive = true
				break
			}
		}
		remaining--
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) ||
				errors.Is(result.err, context.DeadlineExceeded) ||
				ctx.Err() != nil {
				statuses[result.position] = nodeCancelled
			} else {
				statuses[result.position] = nodeFailed
			}
			runErrors = append(
				runErrors,
				fmt.Errorf("execution unit %d failed: %w", result.position, result.err),
			)
		} else {
			statuses[result.position] = nodeSucceeded
		}
	}

	if ctx.Err() != nil {
		runErrors = append(runErrors, ctx.Err())
	}
	return errors.Join(runErrors...)
}

func executeNode(
	ctx context.Context,
	node Node,
	lease *BudgetLease,
	run func(context.Context, Node) error,
	results chan<- nodeResult,
) {
	result := nodeResult{position: node.Position}
	defer func() {
		lease.Release()
		if recovered := recover(); recovered != nil {
			result.err = fmt.Errorf("execution unit panicked: %v", recovered)
		}
		results <- result
	}()
	result.err = run(ctx, node)
}

func (r Runner) bypass(event Bypass) error {
	if r.OnBypass == nil {
		return nil
	}
	if err := r.OnBypass(event); err != nil {
		return fmt.Errorf("persist %s execution unit %d: %w", event.Status, event.Node.Position, err)
	}
	return nil
}

func validateNodes(nodes []Node) error {
	for position, node := range nodes {
		if node.Position != position {
			return fmt.Errorf("execution graph node %d has position %d", position, node.Position)
		}
		previous := -1
		for index, dependency := range node.Dependencies {
			if dependency < 0 || dependency >= position {
				return fmt.Errorf(
					"execution graph node %d dependency %d must refer to an earlier node",
					position,
					index,
				)
			}
			if index > 0 && dependency <= previous {
				return fmt.Errorf("execution graph node %d dependencies must be sorted and unique", position)
			}
			previous = dependency
		}
	}
	return nil
}

func canonicalConnectionLimits(limits map[string]int) map[string]int {
	result := make(map[string]int, len(limits))
	for name, limit := range limits {
		name = strings.TrimSpace(name)
		if name != "" && limit > 0 {
			result[name] = limit
		}
	}
	return result
}

func canonicalNodes(nodes []Node) []Node {
	result := make([]Node, len(nodes))
	for index, node := range nodes {
		node.Dependencies = append([]int(nil), node.Dependencies...)
		node.Connections = canonicalStrings(node.Connections)
		node.Resources = canonicalStrings(node.Resources)
		result[index] = node
	}
	return result
}

func nextRunnableNode(
	nodes []Node,
	statuses []nodeStatus,
	active int,
	exclusiveActive bool,
	activeConnections map[string]int,
	connectionLimits map[string]int,
	activeResources map[string]int,
) int {
	for position, node := range nodes {
		if statuses[position] != nodePending || !dependenciesSucceeded(node, statuses) {
			continue
		}
		if node.Exclusive {
			if active != 0 {
				continue
			}
		} else if exclusiveActive {
			continue
		}
		if !connectionsAvailable(node.Connections, activeConnections, connectionLimits) {
			continue
		}
		if !resourcesAvailable(node.Resources, activeResources) {
			continue
		}
		return position
	}
	return -1
}

func dependenciesSucceeded(node Node, statuses []nodeStatus) bool {
	for _, dependency := range node.Dependencies {
		if statuses[dependency] != nodeSucceeded {
			return false
		}
	}
	return true
}

func failedDependencies(node Node, statuses []nodeStatus) []int {
	result := make([]int, 0)
	for _, dependency := range node.Dependencies {
		switch statuses[dependency] {
		case nodeFailed, nodeSkipped, nodeCancelled:
			result = append(result, dependency)
		}
	}
	return result
}

func connectionsAvailable(names []string, active, limits map[string]int) bool {
	for _, name := range names {
		if limit, limited := limits[name]; limited && active[name] >= limit {
			return false
		}
	}
	return true
}

func resourcesAvailable(resources []string, active map[string]int) bool {
	for _, resource := range resources {
		if active[resource] > 0 {
			return false
		}
	}
	return true
}

func reserveNode(node Node, activeConnections, activeResources map[string]int) reservations {
	connections := canonicalStrings(node.Connections)
	resources := canonicalStrings(node.Resources)
	for _, name := range connections {
		activeConnections[name]++
	}
	for _, resource := range resources {
		activeResources[resource]++
	}
	return reservations{
		connections: connections,
		resources:   resources,
		exclusive:   node.Exclusive,
	}
}

func releaseNode(reservation reservations, activeConnections, activeResources map[string]int) {
	for _, name := range reservation.connections {
		if activeConnections[name] <= 1 {
			delete(activeConnections, name)
		} else {
			activeConnections[name]--
		}
	}
	for _, resource := range reservation.resources {
		if activeResources[resource] <= 1 {
			delete(activeResources, resource)
		} else {
			activeResources[resource]--
		}
	}
}

func canonicalStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	deduped := result[:1]
	for _, value := range result[1:] {
		if value != deduped[len(deduped)-1] {
			deduped = append(deduped, value)
		}
	}
	return deduped
}
