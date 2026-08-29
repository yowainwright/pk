package lifecycle

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
)

type Store struct {
	dir            string
	eventPath      string
	processingPath string
	statePath      string
	lockPath       string
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
	return &Store{
		dir:            dir,
		eventPath:      filepath.Join(dir, "lifecycle.jsonl"),
		processingPath: filepath.Join(dir, "lifecycle.processing.jsonl"),
		statePath:      filepath.Join(dir, "lifecycle-state.json"),
		lockPath:       filepath.Join(dir, "lifecycle.lock"),
	}
}

func (s *Store) Append(event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	return s.withLock(func() error {
		return appendJSONLine(s.eventPath, event)
	})
}

func (s *Store) Events() ([]Event, error) {
	if err := ensurePrivateDir(s.dir); err != nil {
		return nil, err
	}
	var events []Event
	err := s.withLock(func() error {
		pending, err := readEventPath(s.processingPath)
		if err != nil {
			return err
		}
		current, err := readEventPath(s.eventPath)
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
	err := s.withLock(func() error {
		if err := rotateEvents(s.eventPath, s.processingPath); err != nil {
			return err
		}
		var readErr error
		events, readErr = readEventPath(s.processingPath)
		return readErr
	})
	return events, err
}

func (s *Store) AcknowledgeEvents() error {
	if err := ensurePrivateDir(s.dir); err != nil {
		return err
	}
	return s.withLock(func() error {
		return removeOptional(s.processingPath)
	})
}

func readEventPath(path string) ([]Event, error) {
	file, err := os.Open(path)
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

func rotateEvents(eventPath string, processingPath string) error {
	if fileExists(processingPath) {
		return nil
	}
	err := os.Rename(eventPath, processingPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rotating lifecycle events: %w", err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Store) LoadState() (State, error) {
	file, err := os.Open(s.statePath)
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
	return writeAtomic(s.statePath, append(data, '\n'))
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, privateDirMode); err != nil {
		return fmt.Errorf("creating lifecycle dir: %w", err)
	}
	return os.Chmod(path, privateDirMode)
}

func appendJSONLine(path string, event Event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, privateFileMode)
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

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating lifecycle temp file: %w", err)
	}
	return finishAtomicWrite(file, path, data)
}

func finishAtomicWrite(file *os.File, path string, data []byte) error {
	tempPath := file.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if err := writeTemp(file, data); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
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

func removeOptional(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) withLock(fn func() error) error {
	file, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, privateFileMode)
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
	return fn()
}

func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	if err != nil {
		return fmt.Errorf("locking lifecycle store: %w", err)
	}
	return nil
}

func unlockFile(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
