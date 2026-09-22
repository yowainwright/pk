package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode"
)

type Store struct {
	path string
}

type preferences struct {
	IgnoredProcessNames []string `json:"ignored_process_names"`
}

func DefaultStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("finding preferences directory: %w", err)
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("preferences directory must be absolute: %s", dir)
	}
	return NewStore(filepath.Join(dir, "pk", "config.json")), nil
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Names() ([]string, error) {
	root, err := os.OpenRoot(filepath.Dir(s.path))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening preferences directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	return readPreferences(root, filepath.Base(s.path))
}

func readPreferences(root *os.Root, name string) ([]string, error) {
	path := filepath.Join(root.Name(), name)
	// #nosec G304 -- The caller supplies the preferences directory and basename; reject symlink leaves.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading preferences: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := requireRegularFile(file); err != nil {
		return nil, err
	}
	return decodePreferences(file)
}

func requireRegularFile(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("preferences must be a regular file")
	}
	return nil
}

func decodePreferences(reader io.Reader) ([]string, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var value preferences
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decoding preferences: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	if value.IgnoredProcessNames == nil {
		return nil, fmt.Errorf("ignored_process_names must be an array of process names")
	}
	return normalizedNames(value.IgnoredProcessNames)
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("preferences must contain exactly one JSON object")
}

func normalizedNames(names []string) ([]string, error) {
	result := append([]string{}, names...)
	for _, name := range result {
		if err := validateProcessName(name); err != nil {
			return nil, err
		}
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}

func validateProcessName(name string) error {
	empty := strings.TrimSpace(name) == ""
	padded := strings.TrimSpace(name) != name
	control := strings.ContainsFunc(name, unicode.IsControl)
	invalid := empty || padded || control
	if invalid {
		return fmt.Errorf("invalid process name %q: blank, padded, or control characters", name)
	}
	return nil
}

func (s *Store) Add(names []string) error {
	remove := false
	return s.update(names, remove)
}

func (s *Store) Remove(names []string) error {
	remove := true
	return s.update(names, remove)
}

func (s *Store) update(names []string, remove bool) error {
	valid, err := normalizedNames(names)
	if err != nil {
		return err
	}
	root, err := openPreferencesRoot(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return updateLocked(root, filepath.Base(s.path), valid, remove)
}

func openPreferencesRoot(dir string) (*os.Root, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating preferences directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("opening preferences directory: %w", err)
	}
	return root, nil
}

func updateLocked(root *os.Root, name string, names []string, remove bool) error {
	path := filepath.Join(root.Name(), name+".lock")
	// #nosec G304 -- The lock is beside the selected preferences file; O_NOFOLLOW rejects symlink leaves.
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("opening preferences lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	fd, err := preferencesDescriptor(lock)
	if err != nil {
		return err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking preferences: %w", err)
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	return updatePreferences(root, name, names, remove)
}

func preferencesDescriptor(file *os.File) (int, error) {
	fd := file.Fd()
	if fd > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("file descriptor out of range: %d", fd)
	}
	return int(fd), nil
}

func updatePreferences(root *os.Root, name string, names []string, remove bool) error {
	current, err := readPreferences(root, name)
	if err != nil {
		return err
	}
	next := changedNames(current, names, remove)
	value := preferences{IgnoredProcessNames: next}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding preferences: %w", err)
	}
	return writePreferences(root, name, append(data, '\n'))
}

func changedNames(current []string, names []string, remove bool) []string {
	if remove {
		return slices.DeleteFunc(
			current,
			func(name string) bool { return slices.Contains(names, name) },
		)
	}
	combined := append(current, names...)
	slices.Sort(combined)
	return slices.Compact(combined)
}

func writePreferences(root *os.Root, name string, data []byte) error {
	temporary := name + "." + rand.Text() + ".tmp"
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating temporary preferences: %w", err)
	}
	defer func() { _ = file.Close() }()
	defer func() { _ = root.Remove(temporary) }()
	if err := writePreferencesData(file, data); err != nil {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return fmt.Errorf("replacing preferences: %w", err)
	}
	return nil
}

func writePreferencesData(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("writing preferences: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing preferences: %w", err)
	}
	return file.Close()
}
