package helpers

import (
	"slices"
	"testing"
)

func TestFilterPreservesOrderAndCallsPredicateOncePerItem(t *testing.T) {
	input := []int{3, 2, 1, 3}
	var calls []int
	keep := func(value int) bool {
		calls = append(calls, value)
		return value != 2
	}
	got := Filter(input, keep)
	want := []int{3, 1, 3}
	resultMatches := slices.Equal(got, want)
	if !resultMatches {
		t.Fatalf("Filter = %v, want %v", got, want)
	}
	callsMatch := slices.Equal(calls, input)
	if !callsMatch {
		t.Fatalf("predicate calls = %v, want %v", calls, input)
	}
}

func TestFilterDoesNotAliasInput(t *testing.T) {
	input := []int{1, 2}
	got := Filter(input, func(int) bool { return true })
	got[0] = 9
	if input[0] != 1 {
		t.Fatal("changing the filtered slice modified the input")
	}
}

func TestFilterPreservesEmptyResultAndCapacity(t *testing.T) {
	inputs := map[string][]int{"nil": nil, "empty": {}, "no matches": {1, 2}}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			got := Filter(input, func(int) bool { return false })
			if got == nil {
				t.Fatal("Filter returned nil")
			}
			if len(got) != 0 {
				t.Fatalf("length = %d, want 0", len(got))
			}
			if cap(got) != len(input) {
				t.Fatalf("capacity = %d, want %d", cap(got), len(input))
			}
		})
	}
}
