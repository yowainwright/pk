package processtree

import (
	"sort"

	"github.com/jeffrywainwright/pk/internal/process"
)

func Descendants(procs []process.Process, rootPID int32) []process.Process {
	children := childrenByParent(procs)
	seen := map[int32]bool{rootPID: true}
	result := make([]process.Process, 0)
	appendDescendants(rootPID, children, seen, &result)
	return result
}

func KillOrder(root process.Process, descendants []process.Process) []process.Process {
	children := childrenByParent(descendants)
	seen := map[int32]bool{root.PID: true}
	order := make([]process.Process, 0, len(descendants)+1)
	appendKillOrder(root.PID, children, seen, &order)
	order = append(order, root)
	return order
}

func childrenByParent(procs []process.Process) map[int32][]process.Process {
	children := make(map[int32][]process.Process)
	for _, proc := range procs {
		children[proc.ParentPID] = append(children[proc.ParentPID], proc)
	}
	sortChildren(children)
	return children
}

func sortChildren(children map[int32][]process.Process) {
	for parentPID := range children {
		sort.Slice(children[parentPID], func(i, j int) bool {
			return children[parentPID][i].PID < children[parentPID][j].PID
		})
	}
}

func appendDescendants(
	pid int32,
	children map[int32][]process.Process,
	seen map[int32]bool,
	result *[]process.Process,
) {
	for _, child := range children[pid] {
		if seen[child.PID] {
			continue
		}
		seen[child.PID] = true
		*result = append(*result, child)
		appendDescendants(child.PID, children, seen, result)
	}
}

func appendKillOrder(
	pid int32,
	children map[int32][]process.Process,
	seen map[int32]bool,
	order *[]process.Process,
) {
	for _, child := range children[pid] {
		if seen[child.PID] {
			continue
		}
		seen[child.PID] = true
		appendKillOrder(child.PID, children, seen, order)
		*order = append(*order, child)
	}
}
