package lifecycle

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
)

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
	eventFile       = "lifecycle.jsonl"
	processingFile  = "lifecycle.processing.jsonl"
	stateFile       = "lifecycle-state.json"
	lockFileName    = "lifecycle.lock"
)

type Store struct {
	dir string
}

func DefaultDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding config dir: %w", err)
	}
	return filepath.Join(configDir, "pk"), nil
}

func DefaultStore() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return NewStore(dir), nil
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Append(event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	return s.withLock(func(root *os.Root) error {
		return appendJSONLine(root, eventFile, event)
	})
}

func (s *Store) Events() ([]Event, error) {
	if err := ensurePrivateDir(s.dir); err != nil {
		return nil, err
	}
	var events []Event
	err := s.withLock(func(root *os.Root) error {
		pending, err := readEventPath(root, processingFile)
		if err != nil {
			return err
		}
		current, err := readEventPath(root, eventFile)
		if err != nil {
			return err
		}
		events = append(pending, current...)
		return nil
	})
	return events, err
}

func (s *Store) TakeEvents() ([]Event, error) {
	if err := ensurePrivateDir(s.dir); err != nil {
		return nil, err
	}
	var events []Event
	err := s.withLock(func(root *os.Root) error {
		if err := rotateEvents(root, eventFile, processingFile); err != nil {
			return err
		}
		var readErr error
		events, readErr = readEventPath(root, processingFile)
		return readErr
	})
	return events, err
}

func (s *Store) AcknowledgeEvents() error {
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	return s.withLock(func(root *os.Root) error {
		return removeOptional(root, processingFile)
	})
}

func readEventPath(root *os.Root, name string) ([]Event, error) {
	file, err := root.Open(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening lifecycle events: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	return readEvents(file)
}

func rotateEvents(root *os.Root, eventName string, processingName string) error {
	if fileExists(root, processingName) {
		return nil
	}
	err := root.Rename(eventName, processingName)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rotating lifecycle events: %w", err)
	}
	return nil
}

func fileExists(root *os.Root, name string) bool {
	_, err := root.Stat(name)
	return err == nil
}

func (s *Store) LoadState() (State, error) {
	root, err := os.OpenRoot(s.dir)
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("opening lifecycle dir: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.Open(stateFile)
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("opening lifecycle state: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	return decodeState(file)
}

func (s *Store) SaveState(state State) error {
	state.Ensure()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding lifecycle state: %w", err)
	}
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return fmt.Errorf("opening lifecycle dir: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	return writeAtomic(root, stateFile, append(data, '\n'))
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, privateDirMode); err != nil {
		return fmt.Errorf("creating lifecycle dir: %w", err)
	}
	return os.Chmod(path, privateDirMode)
}

func appendJSONLine(root *os.Root, name string, event Event) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, privateFileMode)
	if err != nil {
		return fmt.Errorf("opening lifecycle events: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	return encodeEventLine(file, event)
}

func encodeEventLine(file *os.File, event Event) error {
	if err := file.Chmod(privateFileMode); err != nil {
		return fmt.Errorf("securing lifecycle events: %w", err)
	}
	if err := json.NewEncoder(file).Encode(event); err != nil {
		return fmt.Errorf("writing lifecycle event: %w", err)
	}
	return nil
}

func readEvents(file *os.File) ([]Event, error) {
	scanner := bufio.NewScanner(file)
	events := make([]Event, 0)
	for scanner.Scan() {
		event, err := parseEventLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func parseEventLine(line []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(line, &event); err != nil {
		return Event{}, fmt.Errorf("parsing lifecycle event: %w", err)
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func decodeState(file *os.File) (State, error) {
	var state State
	if err := json.NewDecoder(file).Decode(&state); err != nil {
		return State{}, fmt.Errorf("parsing lifecycle state: %w", err)
	}
	state.Ensure()
	return state, nil
}

func writeAtomic(root *os.Root, name string, data []byte) error {
	tempName, err := tempFileName(name)
	if err != nil {
		return err
	}
	file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFileMode)
	if err != nil {
		return fmt.Errorf("creating lifecycle temp file: %w", err)
	}
	return finishAtomicWrite(root, file, tempName, name, data)
}

func tempFileName(name string) (string, error) {
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("creating lifecycle temp name: %w", err)
	}
	encoded := hex.EncodeToString(token[:])
	tempName := "." + name + "." + encoded + ".tmp"
	return tempName, nil
}

func finishAtomicWrite(
	root *os.Root,
	file *os.File,
	tempName string,
	name string,
	data []byte,
) error {
	defer func() {
		_ = root.Remove(tempName)
	}()
	if err := writeTemp(file, data); err != nil {
		return err
	}
	return root.Rename(tempName, name)
}

func writeTemp(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return closeAfterWriteError(file, err)
	}
	if err := file.Chmod(privateFileMode); err != nil {
		return closeAfterWriteError(file, err)
	}
	if err := file.Sync(); err != nil {
		return closeAfterWriteError(file, err)
	}
	return file.Close()
}

func closeAfterWriteError(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func removeOptional(root *os.Root, name string) error {
	err := root.Remove(name)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) withLock(fn func(*os.Root) error) error {
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return fmt.Errorf("opening lifecycle dir: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	file, err := root.OpenFile(lockFileName, os.O_CREATE|os.O_RDWR, privateFileMode)
	if err != nil {
		return fmt.Errorf("opening lifecycle lock: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	if err := lockFile(file); err != nil {
		return err
	}
	defer unlockFile(file)
	return fn(root)
}

func lockFile(file *os.File) error {
	fd, err := fileDescriptor(file)
	if err != nil {
		return err
	}
	err = syscall.Flock(fd, syscall.LOCK_EX)
	if err != nil {
		return fmt.Errorf("locking lifecycle store: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) {
	fd, err := fileDescriptor(file)
	if err != nil {
		return
	}
	_ = syscall.Flock(fd, syscall.LOCK_UN)
}

func fileDescriptor(file *os.File) (int, error) {
	fd := file.Fd()
	if fd > uintptr(math.MaxInt) {
		return 0, fmt.Errorf("file descriptor out of range: %d", fd)
	}
	return int(fd), nil
}
