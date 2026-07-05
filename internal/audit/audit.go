package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultRetention       = 48 * time.Hour
	DefaultMaxBytes  int64 = 10 * 1024 * 1024
)

type Event struct {
	Time        time.Time `json:"time"`
	Command     string    `json:"command"`
	Action      string    `json:"action"`
	TargetType  string    `json:"target_type,omitempty"`
	Applied     bool      `json:"applied"`
	PID         int32     `json:"pid,omitempty"`
	Name        string    `json:"name,omitempty"`
	ContainerID string    `json:"container_id,omitempty"`
	Image       string    `json:"image,omitempty"`
	CommandLine string    `json:"command_line,omitempty"`
	Cwd         string    `json:"cwd,omitempty"`
	Reasons     []string  `json:"reasons,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type Log struct {
	path      string
	now       func() time.Time
	retention time.Duration
	maxBytes  int64
}

func New(path string) *Log {
	return &Log{
		path:      path,
		now:       time.Now,
		retention: DefaultRetention,
		maxBytes:  DefaultMaxBytes,
	}
}

func DefaultLog() (*Log, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return New(path), nil
}

func DefaultPath() (string, error) {
	override := os.Getenv("PK_AUDIT_PATH")
	if override != "" {
		return override, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding config dir: %w", err)
	}
	return filepath.Join(configDir, "pk", "events.jsonl"), nil
}

func (l *Log) Record(event Event) error {
	event = l.withTimestamp(event)
	events, err := l.Events()
	if err != nil {
		return err
	}

	events = append(events, event)
	events = l.prune(events)
	return l.write(events)
}

func (l *Log) Events() ([]Event, error) {
	file, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	return readEvents(file)
}

func WriteEvents(w io.Writer, events []Event) error {
	for _, event := range events {
		if err := writeEvent(w, event); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) withTimestamp(event Event) Event {
	if event.Time.IsZero() {
		event.Time = l.now()
	}
	return event
}

func (l *Log) prune(events []Event) []Event {
	recent := recentEvents(events, l.now().Add(-l.retention))
	return trimEvents(recent, l.maxBytes)
}

func (l *Log) write(events []Event) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}
	data, err := encodeEvents(events)
	if err != nil {
		return err
	}
	if err := writeAtomicFile(l.path, data, 0o644); err != nil {
		return fmt.Errorf("writing audit log: %w", err)
	}
	return nil
}

func writeAtomicFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if err := writeTempFile(file, data, perm); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func writeTempFile(file *os.File, data []byte, perm os.FileMode) error {
	if _, err := file.Write(data); err != nil {
		return closeAfterError(file, err)
	}
	if err := file.Chmod(perm); err != nil {
		return closeAfterError(file, err)
	}
	if err := file.Sync(); err != nil {
		return closeAfterError(file, err)
	}
	return file.Close()
}

func closeAfterError(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

func readEvents(r io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(r)
	events := make([]Event, 0)
	for scanner.Scan() {
		event, err := parseLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func parseLine(line []byte) (Event, error) {
	var event Event
	if err := json.Unmarshal(line, &event); err != nil {
		return Event{}, fmt.Errorf("parsing audit event: %w", err)
	}
	return event, nil
}

func recentEvents(events []Event, cutoff time.Time) []Event {
	recent := make([]Event, 0, len(events))
	for _, event := range events {
		if !event.Time.Before(cutoff) {
			recent = append(recent, event)
		}
	}
	return recent
}

func trimEvents(events []Event, maxBytes int64) []Event {
	kept := make([]Event, 0, len(events))
	var size int64
	for i := len(events) - 1; i >= 0; i-- {
		eventSize := encodedSize(events[i])
		if shouldStopTrim(size, eventSize, maxBytes, len(kept)) {
			break
		}
		size += eventSize
		kept = append(kept, events[i])
	}
	reverseEvents(kept)
	return kept
}

func shouldStopTrim(size int64, eventSize int64, maxBytes int64, kept int) bool {
	wouldOverflow := size+eventSize > maxBytes
	hasKeptEvent := kept > 0
	return wouldOverflow && hasKeptEvent
}

func encodedSize(event Event) int64 {
	data, err := json.Marshal(event)
	if err != nil {
		return 0
	}
	return int64(len(data) + 1)
}

func reverseEvents(events []Event) {
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
}

func encodeEvents(events []Event) ([]byte, error) {
	var buf bytes.Buffer
	if err := WriteEvents(&buf, events); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeEvent(w io.Writer, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encoding audit event: %w", err)
	}
	if _, err := fmt.Fprintln(w, string(data)); err != nil {
		return fmt.Errorf("writing audit event: %w", err)
	}
	return nil
}
