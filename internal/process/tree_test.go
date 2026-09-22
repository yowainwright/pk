package process

import (
	"testing"
)

func TestDescendantsReturnsNestedChildren(t *testing.T) {
	procs := []Process{
		testProcess(1, 0),
		testProcess(3, 1),
		testProcess(2, 1),
		testProcess(4, 2),
		testProcess(5, 9),
	}

	descendants := NewIndex(procs).Descendants(1)

	assertPIDs(t, descendants, 2, 4, 3)
}

func TestKillOrderKillsChildrenBeforeParents(t *testing.T) {
	root := testProcess(1, 0)
	descendants := []Process{
		testProcess(2, 1),
		testProcess(3, 2),
	}

	order := KillOrder(root, descendants)

	assertPIDs(t, order, 3, 2, 1)
}

func TestKillOrderIncludesDescendantsBelowFilteredIntermediaries(t *testing.T) {
	root := testProcess(1, 0)
	descendants := []Process{
		testProcess(3, 2),
		testProcess(4, 3),
	}

	order := KillOrder(root, descendants)

	assertPIDs(t, order, 4, 3, 1)
}

func TestDescendantsIgnoresCycles(t *testing.T) {
	procs := []Process{
		testProcess(2, 1),
		testProcess(1, 2),
	}

	descendants := NewIndex(procs).Descendants(1)

	assertPIDs(t, descendants, 2)
}

func testProcess(pid int32, parentPID int32) Process {
	var proc Process
	proc.PID = pid
	proc.ParentPID = parentPID
	return proc
}

func assertPIDs(t *testing.T, procs []Process, expected ...int32) {
	t.Helper()
	if len(procs) != len(expected) {
		t.Fatalf("expected pids %#v, got %#v", expected, pids(procs))
	}
	for i, pid := range expected {
		if procs[i].PID != pid {
			t.Fatalf("expected pid %d at %d, got %#v", pid, i, pids(procs))
		}
	}
}

func pids(procs []Process) []int32 {
	result := make([]int32, 0, len(procs))
	for _, proc := range procs {
		result = append(result, proc.PID)
	}
	return result
}

func TestIndexKeepsTraversalsIndependent(t *testing.T) {
	procs := []Process{
		testProcess(1, 3),
		testProcess(2, 1),
		testProcess(3, 2),
	}
	tree := NewIndex(procs)
	assertPIDs(t, tree.Descendants(1), 2, 3)
	assertPIDs(t, tree.Descendants(2), 3, 1)
	assertPIDs(t, tree.Descendants(1), 2, 3)
}

func BenchmarkSnapshotDescendants(b *testing.B) {
	procs := make([]Process, 0, 1000)
	for pid := int32(1); pid <= 1000; pid++ {
		procs = append(procs, testProcess(pid, pid/2))
	}
	b.Run("rebuild-per-root", func(b *testing.B) {
		for b.Loop() {
			rebuildForEachRoot(procs)
		}
	})
	b.Run("index-per-snapshot", func(b *testing.B) {
		for b.Loop() {
			indexOncePerSnapshot(procs)
		}
	})
}

func rebuildForEachRoot(procs []Process) {
	for _, proc := range procs {
		NewIndex(procs).Descendants(proc.PID)
	}
}

func indexOncePerSnapshot(procs []Process) {
	tree := NewIndex(procs)
	for _, proc := range procs {
		tree.Descendants(proc.PID)
	}
}
