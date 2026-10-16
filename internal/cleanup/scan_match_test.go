package cleanup

import (
	"slices"
	"testing"

	"github.com/yowainwright/pk/internal/process"
)

type commandMatchCase struct {
	label   string
	name    string
	command string
	want    bool
}

func commandMatchCases() []commandMatchCase {
	return []commandMatchCase{
		{"name", "node", "", true},
		{"normalized name", `"/usr/bin/NODE"`, "postgres", true},
		{"command fallback", "server", "/usr/bin/node server.js", true},
		{"quoted command", "server", `'NODE' server.js`, true},
		{"unicode whitespace", "server", "\u2003\t/usr/bin/NODE\u00a0server.js", true},
		{"argument only", "server", "postgres node", false},
		{"substring", "node-helper", "node-helper server.js", false},
		{"empty", "", "", false},
		{"whitespace only", "server", " \t\n\u2003", false},
	}
}

func TestMatchesAnyCommand(t *testing.T) {
	commands := []string{"node"}
	for _, tc := range commandMatchCases() {
		t.Run(tc.label, func(t *testing.T) {
			got := matchesAnyCommand(tc.name, tc.command, commands)
			if got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUnprotectedDescendantsPreservesOrderAndIndex(t *testing.T) {
	cfg := baseConfig()
	procs := []process.Process{
		{PID: 1, Name: "node"},
		{PID: 2, ParentPID: 1, Name: "Code"},
		{PID: 3, ParentPID: 2, Name: "node"},
		{PID: 4, ParentPID: 1, Name: "node"},
	}
	tree := process.NewIndex(procs)
	got := unprotectedDescendants(cfg, procs[0], tree)
	want := []process.Process{procs[2], procs[3]}
	filteredMatches := slices.Equal(got, want)
	if !filteredMatches {
		t.Fatalf("filtered descendants = %#v, want %#v", got, want)
	}
	indexMatches := slices.Equal(tree.Descendants(1), procs[1:])
	if !indexMatches {
		t.Fatal("filtering descendants modified the process index")
	}
}
