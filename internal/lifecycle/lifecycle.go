package lifecycle

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const Version = 1

const (
	KindSessionStart     = "session.start"
	KindSessionHeartbeat = "session.heartbeat"
	KindSessionInactive  = "session.inactive"
	KindSessionStop      = "session.stop"
	KindCommandStart     = "command.start"
	KindCommandFinish    = "command.finish"
	KindContextStart     = "context.start"
	KindContextStop      = "context.stop"
)

type Event struct {
	Version           int       `json:"version"`
	EventID           string    `json:"event_id"`
	Kind              string    `json:"kind"`
	ObservedAt        time.Time `json:"observed_at"`
	Source            string    `json:"source"`
	TerminalSessionID string    `json:"terminal_session_id,omitempty"`
	AgentSessionID    string    `json:"agent_session_id,omitempty"`
	UserSessionID     string    `json:"user_session_id,omitempty"`
	WindowID          string    `json:"window_id,omitempty"`
	TabID             string    `json:"tab_id,omitempty"`
	ShellPID          int32     `json:"shell_pid,omitempty"`
	ShellCreateTime   int64     `json:"shell_create_time,omitempty"`
	ParentPID         int32     `json:"parent_pid,omitempty"`
	ProcessPID        int32     `json:"process_pid,omitempty"`
	ProcessCreateTime int64     `json:"process_create_time,omitempty"`
	TTY               string    `json:"tty,omitempty"`
	Cwd               string    `json:"cwd,omitempty"`
	CommandHash       string    `json:"command_hash,omitempty"`
	ExitCode          *int      `json:"exit_code,omitempty"`
}

type ProcessKey struct {
	PID        int32 `json:"pid"`
	CreateTime int64 `json:"create_time"`
}

type TerminalSession struct {
	ID              string     `json:"id"`
	ShellProcessKey ProcessKey `json:"shell_process_key"`
	AgentSessionID  string     `json:"agent_session_id,omitempty"`
	UserSessionID   string     `json:"user_session_id,omitempty"`
	WindowID        string     `json:"window_id,omitempty"`
	TabID           string     `json:"tab_id,omitempty"`
	Exists          bool       `json:"exists"`
	Active          bool       `json:"active"`
	StartedAt       time.Time  `json:"started_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	InactiveAt      time.Time  `json:"inactive_at,omitempty"`
	EndedAt         time.Time  `json:"ended_at,omitempty"`
}

type Presence struct {
	ID         string    `json:"id"`
	Exists     bool      `json:"exists"`
	Active     bool      `json:"active"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	InactiveAt time.Time `json:"inactive_at,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
}

type ManagedProcess struct {
	ProcessKey        ProcessKey `json:"process_key"`
	TerminalSessionID string     `json:"terminal_session_id"`
	FirstSeenAt       time.Time  `json:"first_seen_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	LastParentPID     int32      `json:"last_parent_pid"`
	Name              string     `json:"name"`
	Cwd               string     `json:"cwd,omitempty"`
}

type DaemonState struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	LastTickAt time.Time `json:"last_tick_at"`
	LastError  string    `json:"last_error,omitempty"`
}

type State struct {
	Sessions      map[string]TerminalSession `json:"sessions,omitempty"`
	Tabs          map[string]Presence        `json:"tabs,omitempty"`
	Windows       map[string]Presence        `json:"windows,omitempty"`
	AgentSessions map[string]Presence        `json:"agent_sessions,omitempty"`
	UserSessions  map[string]Presence        `json:"user_sessions,omitempty"`
	Processes     map[string]ManagedProcess  `json:"processes,omitempty"`
	AppliedEvents map[string]bool            `json:"applied_events,omitempty"`
	Daemon        DaemonState                `json:"daemon"`
	LastDecision  string                     `json:"last_decision,omitempty"`
	LastError     string                     `json:"last_error,omitempty"`
}

func NewEventID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("creating event id: %w", err)
	}
	return hex.EncodeToString(data[:]), nil
}

func (e Event) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported lifecycle version %d", e.Version)
	}
	if !KnownKind(e.Kind) {
		return fmt.Errorf("unknown lifecycle kind %q", e.Kind)
	}
	if e.EventID == "" {
		return fmt.Errorf("event id is required")
	}
	if e.ObservedAt.IsZero() {
		return fmt.Errorf("observed time is required")
	}
	if e.Source == "" {
		return fmt.Errorf("source is required")
	}
	return validateEventContext(e)
}

func KnownKind(kind string) bool {
	switch kind {
	case KindSessionStart, KindSessionHeartbeat, KindSessionInactive:
		return true
	case KindSessionStop, KindCommandStart, KindCommandFinish:
		return true
	case KindContextStart, KindContextStop:
		return true
	default:
		return false
	}
}

func validateEventContext(event Event) error {
	if requiresTerminalSession(event.Kind) {
		return validateTerminalSession(event)
	}
	if hasContextID(event) {
		return nil
	}
	return fmt.Errorf("context identifier is required")
}

func requiresTerminalSession(kind string) bool {
	switch kind {
	case KindSessionStart, KindSessionHeartbeat, KindSessionInactive:
		return true
	case KindSessionStop, KindCommandStart, KindCommandFinish:
		return true
	default:
		return false
	}
}

func validateTerminalSession(event Event) error {
	if event.TerminalSessionID == "" {
		return fmt.Errorf("terminal session id is required")
	}
	if event.ShellPID <= 0 {
		return fmt.Errorf("shell pid is required")
	}
	if event.ShellCreateTime <= 0 {
		return fmt.Errorf("shell create time is required")
	}
	return nil
}

func hasContextID(event Event) bool {
	hasTerminal := event.TerminalSessionID != ""
	hasAgent := event.AgentSessionID != ""
	hasUser := event.UserSessionID != ""
	hasWindow := event.WindowID != ""
	hasTab := event.TabID != ""
	hasSession := hasTerminal || hasAgent
	hasWindowContext := hasWindow || hasTab
	hasContext := hasSession || hasUser || hasWindowContext
	return hasContext
}

func (s *State) Ensure() {
	s.ensurePresenceMaps()
	s.ensureDataMaps()
}

func (s *State) ensurePresenceMaps() {
	if s.Sessions == nil {
		s.Sessions = make(map[string]TerminalSession)
	}
	if s.Tabs == nil {
		s.Tabs = make(map[string]Presence)
	}
	if s.Windows == nil {
		s.Windows = make(map[string]Presence)
	}
	if s.AgentSessions == nil {
		s.AgentSessions = make(map[string]Presence)
	}
	if s.UserSessions == nil {
		s.UserSessions = make(map[string]Presence)
	}
}

func (s *State) ensureDataMaps() {
	if s.Processes == nil {
		s.Processes = make(map[string]ManagedProcess)
	}
	if s.AppliedEvents == nil {
		s.AppliedEvents = make(map[string]bool)
	}
}

func (k ProcessKey) String() string {
	pid := strconv.FormatInt(int64(k.PID), 10)
	createTime := strconv.FormatInt(k.CreateTime, 10)
	return strings.Join([]string{pid, createTime}, ":")
}

func ProcessKeyString(pid int32, createTime int64) string {
	key := ProcessKey{PID: pid, CreateTime: createTime}
	return key.String()
}
