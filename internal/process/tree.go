package process

import (
	"sort"
)

type Index struct {
	byPID    map[int32]Process
	children map[int32][]Process
}

func NewIndex(procs []Process) *Index {
	byPID := make(map[int32]Process, len(procs))
	for _, proc := range procs {
		byPID[proc.PID] = proc
	}
	return &Index{byPID: byPID, children: childrenByParent(procs)}
}

func (i *Index) Process(pid int32) (Process, bool) {
	proc, ok := i.byPID[pid]
	return proc, ok
}

func (i *Index) Descendants(rootPID int32) []Process {
	seen := map[int32]bool{rootPID: true}
	result := make([]Process, 0)
	appendDescendants(rootPID, i.children, seen, &result)
	return result
}

func KillOrder(root Process, descendants []Process) []Process {
	children := childrenByParent(descendants)
	seen := map[int32]bool{root.PID: true}
	order := make([]Process, 0, len(descendants)+1)
	appendKillOrder(root.PID, children, seen, &order)
	appendRemainingKillOrder(descendants, children, seen, &order)
	order = append(order, root)
	return order
}

func childrenByParent(procs []Process) map[int32][]Process {
	children := make(map[int32][]Process)
	for _, proc := range procs {
		children[proc.ParentPID] = append(children[proc.ParentPID], proc)
	}
	sortChildren(children)
	return children
}

func sortChildren(children map[int32][]Process) {
	for parentPID := range children {
		sort.Slice(children[parentPID], func(i, j int) bool {
			return children[parentPID][i].PID < children[parentPID][j].PID
		})
	}
}

func appendDescendants(
	pid int32,
	children map[int32][]Process,
	seen map[int32]bool,
	result *[]Process,
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
	children map[int32][]Process,
	seen map[int32]bool,
	order *[]Process,
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

func appendRemainingKillOrder(
	descendants []Process,
	children map[int32][]Process,
	seen map[int32]bool,
	order *[]Process,
) {
	for _, proc := range descendants {
		if seen[proc.PID] {
			continue
		}
		seen[proc.PID] = true
		appendKillOrder(proc.PID, children, seen, order)
		*order = append(*order, proc)
	}
}
