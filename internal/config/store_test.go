package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestSavedIgnoresSurviveReopening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pk", "config.json")
	store := NewStore(path)
	requireNoError(t, store.Add([]string{"postgres", "redis-server", "postgres"}))
	names, err := NewStore(path).Names()
	requireNoError(t, err)
	want := []string{"postgres", "redis-server"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("saved ignores: got %v, want %v", names, want)
	}
}

func TestMissingPreferencesAreEmptyWithoutCreatingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pk", "config.json")
	names, err := NewStore(path).Names()
	unexpected := err != nil || len(names) != 0
	if unexpected {
		t.Fatalf("missing preferences: %v, %v", names, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("read created preferences directory: %v", err)
	}
}

func TestRemoveSavedIgnoresPreservesBuiltins(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
	requireNoError(t, store.Add([]string{"node", "pk"}))
	cfg, err := Default()
	requireNoError(t, err)
	requireNoError(t, cfg.UseStore(store))
	if !cfg.IsProtected("node") {
		t.Fatal("saved name not protected")
	}
	requireNoError(t, store.Remove([]string{"node", "pk", "missing"}))
	requireNoError(t, cfg.Reload())
	wrongProtections := cfg.IsProtected("node") || !cfg.IsProtected("pk")
	if wrongProtections {
		t.Fatalf("unexpected protections: %v", cfg.Protected)
	}
}

func TestMalformedPreferencesAreNotOverwritten(t *testing.T) {
	cases := []string{
		`{`,
		`{}`,
		`null`,
		`{"ignored_process_names":null}`,
		`{"ignored_process_names":[1]}`,
		`{"ignored_process_names":[],"typo":true}`,
		`{"ignored_process_names":[]} {}`,
		`{"ignored_process_names":[" node"]}`,
	}
	for _, data := range cases {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			requireNoError(t, os.WriteFile(path, []byte(data), 0o600))
			store := NewStore(path)
			if _, err := store.Names(); err == nil {
				t.Fatal("accepted malformed preferences")
			}
			if err := store.Add([]string{"node"}); err == nil {
				t.Fatal("overwrote malformed preferences")
			}
			got, err := os.ReadFile(path)
			changed := err != nil || string(got) != data
			if changed {
				t.Fatalf("preferences changed: %q, %v", got, err)
			}
		})
	}
}

func TestConcurrentIgnoreUpdatesPreserveAllNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var group sync.WaitGroup
	for index := range 12 {
		group.Go(func() {
			name := fmt.Sprintf("worker-%02d", index)
			if err := NewStore(path).Add([]string{name}); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	names, err := NewStore(path).Names()
	lostUpdate := err != nil || len(names) != 12
	if lostUpdate {
		t.Fatalf("lost update: %v, %v", names, err)
	}
	info, err := os.Stat(path)
	requireNoError(t, err)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences mode: %o", info.Mode().Perm())
	}
}

func TestPreferencesRejectSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original.json")
	data := []byte(`{"ignored_process_names":["node"]}`)
	requireNoError(t, os.WriteFile(target, data, 0o600))
	path := filepath.Join(dir, "config.json")
	requireNoError(t, os.Symlink(filepath.Base(target), path))
	store := NewStore(path)
	if _, err := store.Names(); err == nil {
		t.Fatal("read preferences symlink")
	}
	if err := store.Remove([]string{"node"}); err == nil {
		t.Fatal("changed preferences symlink")
	}
}

func TestInvalidNamesDoNotPartiallyUpdatePreferences(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
	for _, invalid := range []string{"", " ", " node", "node\n"} {
		if err := store.Add([]string{"valid", invalid}); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	names, err := store.Names()
	partialUpdate := err != nil || len(names) != 0
	if partialUpdate {
		t.Fatalf("partial update: %v, %v", names, err)
	}
}

func TestIgnoreRejectsSymlinkedLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	target := filepath.Join(dir, "other.lock")
	requireNoError(t, os.WriteFile(target, nil, 0o600))
	requireNoError(t, os.Symlink(filepath.Base(target), path+".lock"))
	if err := NewStore(path).Add([]string{"node"}); err == nil {
		t.Fatal("accepted symlinked preferences lock")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("wrote preferences without its own lock: %v", err)
	}
}

func TestReloadFailureRetainsLastProtectionPolicy(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "config.json"))
	requireNoError(t, store.Add([]string{"node"}))
	cfg, err := Default()
	requireNoError(t, err)
	requireNoError(t, cfg.UseStore(store))
	requireNoError(t, os.WriteFile(store.Path(), []byte("{"), 0o600))
	if err := cfg.Reload(); err == nil {
		t.Fatal("accepted corrupt config")
	}
	weakened := !cfg.IsProtected("node") || !cfg.IsProtected("pk")
	if weakened {
		t.Fatal("reload weakened protection")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
