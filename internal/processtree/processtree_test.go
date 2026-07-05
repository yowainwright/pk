package processtree

import (
	"testing"

	"github.com/jeffrywainwright/pk/internal/process"
)

func TestDescendantsReturnsNestedChildren(t *testing.T) {
	procs := []process.Process{
		testProcess(1, 0),
		testProcess(3, 1),
		testProcess(2, 1),
		testProcess(4, 2),
		testProcess(5, 9),
	}

	descendants := Descendants(procs, 1)

	assertPIDs(t, descendants, 2, 4, 3)
}

func TestKillOrderKillsChildrenBeforeParents(t *testing.T) {
	root := testProcess(1, 0)
	descendants := []process.Process{
		testProcess(2, 1),
		testProcess(3, 2),
	}

	order := KillOrder(root, descendants)

	assertPIDs(t, order, 3, 2, 1)
}

func TestKillOrderIncludesDescendantsBelowFilteredIntermediaries(t *testing.T) {
	root := testProcess(1, 0)
	descendants := []process.Process{
		testProcess(3, 2),
		testProcess(4, 3),
	}

	order := KillOrder(root, descendants)

	assertPIDs(t, order, 4, 3, 1)
}

func TestDescendantsIgnoresCycles(t *testing.T) {
	procs := []process.Process{
		testProcess(2, 1),
		testProcess(1, 2),
	}

	descendants := Descendants(procs, 1)

	assertPIDs(t, descendants, 2)
}

func testProcess(pid int32, parentPID int32) process.Process {
	var proc process.Process
	proc.PID = pid
	proc.ParentPID = parentPID
	return proc
}

func assertPIDs(t *testing.T, procs []process.Process, expected ...int32) {
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

func pids(procs []process.Process) []int32 {
	result := make([]int32, 0, len(procs))
	for _, proc := range procs {
		result = append(result, proc.PID)
	}
	return result
}
