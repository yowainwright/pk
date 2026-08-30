package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/killer"
	"github.com/yowainwright/pk/internal/lifecycle"
	"github.com/yowainwright/pk/internal/process"
	"github.com/yowainwright/pk/internal/processtree"
)

type Store interface {
	TakeEvents() ([]lifecycle.Event, error)
	AcknowledgeEvents() error
	LoadState() (lifecycle.State, error)
	SaveState(lifecycle.State) error
}

type Audit interface {
	Record(audit.Event) error
}

type Options struct {
	Now       func() time.Time
	StartedAt time.Time
}

type Runner struct {
	cfg       *config.Config
	lister    process.Lister
	killer    killer.Killer
	store     Store
	audit     Audit
	now       func() time.Time
	startedAt time.Time
}

const maxAppliedEventIDs = 2048

func New(
	cfg *config.Config,
	lister process.Lister,
	k killer.Killer,
	store Store,
	audit Audit,
	options Options,
) *Runner {
	return &Runner{
		cfg:       cfg,
		lister:    lister,
		killer:    k,
		store:     store,
		audit:     audit,
		now:       daemonClock(options.Now),
		startedAt: daemonStartedAt(options.StartedAt),
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.Tick(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	return r.loop(ctx, ticker.C)
}

func (r *Runner) Tick(ctx context.Context) error {
	state, events, err := r.load()
	if err != nil {
		return err
	}
	state = applyEvents(state, events)
	procs, err := r.lister.List(ctx)
	if err != nil {
		return r.saveError(state, err)
	}
	state = r.reconcile(ctx, state, procs)
	setDaemonState(&state, os.Getpid(), r.startedAt, r.now())
	retainAppliedEvents(&state, events)
	if err := r.store.SaveState(state); err != nil {
		return err
	}
	return r.store.AcknowledgeEvents()
}

func (r *Runner) loop(ctx context.Context, ticks <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticks:
			if err := r.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *Runner) load() (lifecycle.State, []lifecycle.Event, error) {
	state, err := r.store.LoadState()
	if err != nil {
		return lifecycle.State{}, nil, fmt.Errorf("loading lifecycle state: %w", err)
	}
	events, err := r.store.TakeEvents()
	if err != nil {
		return lifecycle.State{}, nil, fmt.Errorf("taking lifecycle events: %w", err)
	}
	state.Ensure()
	return state, events, nil
}

func (r *Runner) saveError(state lifecycle.State, cause error) error {
	state.LastError = cause.Error()
	state.Daemon.LastError = cause.Error()
	setDaemonState(&state, os.Getpid(), r.startedAt, r.now())
	if err := r.store.SaveState(state); err != nil {
		return fmt.Errorf("%w; saving lifecycle error: %v", cause, err)
	}
	return cause
}

func applyEvents(state lifecycle.State, events []lifecycle.Event) lifecycle.State {
	state.Ensure()
	for _, event := range events {
		if state.AppliedEvents[event.EventID] {
			continue
		}
		state = applyEvent(state, event)
		state.AppliedEvents[event.EventID] = true
	}
	return state
}

func retainAppliedEvents(state *lifecycle.State, events []lifecycle.Event) {
	if len(state.AppliedEvents) <= maxAppliedEventIDs {
		return
	}
	current := currentEventIDs(events)
	pruneAppliedEvents(state.AppliedEvents, current)
}

func currentEventIDs(events []lifecycle.Event) map[string]bool {
	current := make(map[string]bool, len(events))
	for _, event := range events {
		current[event.EventID] = true
	}
	return current
}

func pruneAppliedEvents(applied map[string]bool, current map[string]bool) {
	for eventID := range applied {
		if len(applied) <= maxAppliedEventIDs {
			return
		}
		if current[eventID] {
			continue
		}
		delete(applied, eventID)
	}
}

func applyEvent(state lifecycle.State, event lifecycle.Event) lifecycle.State {
	if lifecycleRequiresSession(event.Kind) {
		applySessionEvent(&state, event)
		return state
	}
	applyContextPresenceEvent(&state, event)
	state.LastDecision = "context event: " + event.Kind
	return state
}

func lifecycleRequiresSession(kind string) bool {
	switch kind {
	case lifecycle.KindSessionStart, lifecycle.KindSessionHeartbeat:
		return true
	case lifecycle.KindSessionInactive, lifecycle.KindSessionStop:
		return true
	case lifecycle.KindCommandStart, lifecycle.KindCommandFinish:
		return true
	default:
		return false
	}
}

func applySessionEvent(state *lifecycle.State, event lifecycle.Event) {
	session := sessionFromEvent(*state, event)
	switch event.Kind {
	case lifecycle.KindSessionStop:
		markSessionEnded(&session, event.ObservedAt)
	case lifecycle.KindSessionInactive, lifecycle.KindCommandFinish:
		markSessionInactive(&session, event.ObservedAt)
	default:
		markSessionActive(&session, event.ObservedAt)
	}
	state.Sessions[event.TerminalSessionID] = session
	applySessionPresenceEvent(state, event)
}

func sessionFromEvent(state lifecycle.State, event lifecycle.Event) lifecycle.TerminalSession {
	session := state.Sessions[event.TerminalSessionID]
	if session.ID == "" {
		session.ID = event.TerminalSessionID
		session.StartedAt = event.ObservedAt
	}
	session.ShellProcessKey = lifecycle.ProcessKey{
		PID:        event.ShellPID,
		CreateTime: event.ShellCreateTime,
	}
	session.AgentSessionID = keepID(session.AgentSessionID, event.AgentSessionID)
	session.UserSessionID = keepID(session.UserSessionID, event.UserSessionID)
	session.WindowID = keepID(session.WindowID, event.WindowID)
	session.TabID = keepID(session.TabID, event.TabID)
	session.LastSeenAt = event.ObservedAt
	return session
}

func keepID(current string, next string) string {
	if next != "" {
		return next
	}
	return current
}

func applyContextPresenceEvent(state *lifecycle.State, event lifecycle.Event) {
	applyPresenceByID(state.Tabs, event.TabID, event)
	applyPresenceByID(state.Windows, event.WindowID, event)
	applyPresenceByID(state.AgentSessions, event.AgentSessionID, event)
	applyPresenceByID(state.UserSessions, event.UserSessionID, event)
}

func applySessionPresenceEvent(state *lifecycle.State, event lifecycle.Event) {
	applyPresenceByID(state.Tabs, event.TabID, event)
	if event.Kind == lifecycle.KindSessionStop {
		return
	}
	touchPresenceByID(state.Windows, event.WindowID, event)
	touchPresenceByID(state.AgentSessions, event.AgentSessionID, event)
	touchPresenceByID(state.UserSessions, event.UserSessionID, event)
}

func applyPresenceByID(
	presences map[string]lifecycle.Presence,
	id string,
	event lifecycle.Event,
) {
	if id == "" {
		return
	}
	presence := presenceFromEvent(presences[id], id, event)
	presences[id] = presence
}

func touchPresenceByID(
	presences map[string]lifecycle.Presence,
	id string,
	event lifecycle.Event,
) {
	if id == "" {
		return
	}
	presence := touchPresence(presences[id], id, event.ObservedAt)
	presences[id] = presence
}

func touchPresence(
	presence lifecycle.Presence,
	id string,
	at time.Time,
) lifecycle.Presence {
	if presence.ID == "" {
		presence.ID = id
		presence.StartedAt = at
	}
	presence.Exists = true
	presence.Active = true
	presence.LastSeenAt = at
	return presence
}

func presenceFromEvent(
	presence lifecycle.Presence,
	id string,
	event lifecycle.Event,
) lifecycle.Presence {
	if presence.ID == "" {
		presence.ID = id
		presence.StartedAt = event.ObservedAt
	}
	presence.LastSeenAt = event.ObservedAt
	return markPresence(presence, event)
}

func markPresence(
	presence lifecycle.Presence,
	event lifecycle.Event,
) lifecycle.Presence {
	if presenceEndedKind(event.Kind) {
		return endedPresence(presence, event.ObservedAt)
	}
	if presenceInactiveKind(event.Kind) {
		return inactivePresence(presence, event.ObservedAt)
	}
	return activePresence(presence)
}

func presenceEndedKind(kind string) bool {
	switch kind {
	case lifecycle.KindSessionStop, lifecycle.KindContextStop:
		return true
	default:
		return false
	}
}

func presenceInactiveKind(kind string) bool {
	switch kind {
	case lifecycle.KindSessionInactive, lifecycle.KindCommandFinish:
		return true
	default:
		return false
	}
}

func activePresence(presence lifecycle.Presence) lifecycle.Presence {
	presence.Exists = true
	presence.Active = true
	presence.InactiveAt = time.Time{}
	presence.EndedAt = time.Time{}
	return presence
}

func inactivePresence(
	presence lifecycle.Presence,
	at time.Time,
) lifecycle.Presence {
	presence.Exists = true
	presence.Active = false
	presence.InactiveAt = at
	return presence
}

func endedPresence(
	presence lifecycle.Presence,
	at time.Time,
) lifecycle.Presence {
	presence.Exists = false
	presence.Active = false
	presence.EndedAt = at
	return presence
}

func markSessionActive(session *lifecycle.TerminalSession, at time.Time) {
	session.Exists = true
	session.Active = true
	session.InactiveAt = time.Time{}
	session.EndedAt = time.Time{}
}

func markSessionInactive(session *lifecycle.TerminalSession, at time.Time) {
	session.Exists = true
	session.Active = false
	session.InactiveAt = at
}

func markSessionEnded(session *lifecycle.TerminalSession, at time.Time) {
	session.Exists = false
	session.Active = false
	session.EndedAt = at
}

func (r *Runner) reconcile(
	ctx context.Context,
	state lifecycle.State,
	procs []process.Process,
) lifecycle.State {
	live := liveProcesses(procs)
	for _, session := range state.Sessions {
		state = r.reconcileSession(state, session, procs, live)
	}
	return r.killEligible(ctx, state, live)
}

func liveProcesses(procs []process.Process) map[string]process.Process {
	live := make(map[string]process.Process, len(procs))
	for _, proc := range procs {
		key := lifecycle.ProcessKeyString(proc.PID, proc.CreateTime)
		live[key] = proc
	}
	return live
}

func (r *Runner) reconcileSession(
	state lifecycle.State,
	session lifecycle.TerminalSession,
	procs []process.Process,
	live map[string]process.Process,
) lifecycle.State {
	if sessionEnded(session) {
		state.Sessions[session.ID] = session
		return state
	}
	root, exists := live[session.ShellProcessKey.String()]
	if !exists {
		return sessionMissing(state, session, r.now())
	}
	state.Sessions[session.ID] = liveSession(session, r.now())
	return trackDescendants(state, session.ID, root, procs, r.now())
}

func sessionEnded(session lifecycle.TerminalSession) bool {
	missing := !session.Exists
	hasEndedAt := !session.EndedAt.IsZero()
	ended := missing && hasEndedAt
	return ended
}

func sessionMissing(
	state lifecycle.State,
	session lifecycle.TerminalSession,
	now time.Time,
) lifecycle.State {
	if session.EndedAt.IsZero() {
		session.EndedAt = now
	}
	session.Exists = false
	session.Active = false
	state.Sessions[session.ID] = session
	state.LastDecision = "session missing: " + session.ID
	return state
}

func liveSession(session lifecycle.TerminalSession, now time.Time) lifecycle.TerminalSession {
	session.Exists = true
	session.LastSeenAt = now
	return session
}

func trackDescendants(
	state lifecycle.State,
	sessionID string,
	root process.Process,
	procs []process.Process,
	now time.Time,
) lifecycle.State {
	for _, proc := range processtree.Descendants(procs, root.PID) {
		state = trackProcess(state, sessionID, proc, now)
	}
	return state
}

func trackProcess(
	state lifecycle.State,
	sessionID string,
	proc process.Process,
	now time.Time,
) lifecycle.State {
	key := lifecycle.ProcessKeyString(proc.PID, proc.CreateTime)
	managed := state.Processes[key]
	if managed.FirstSeenAt.IsZero() {
		managed.FirstSeenAt = now
	}
	managed.ProcessKey = lifecycle.ProcessKey{PID: proc.PID, CreateTime: proc.CreateTime}
	managed.TerminalSessionID = sessionID
	managed.LastSeenAt = now
	managed.LastParentPID = proc.ParentPID
	managed.Name = proc.Name
	managed.Cwd = proc.Cwd
	state.Processes[key] = managed
	return state
}

func (r *Runner) killEligible(
	ctx context.Context,
	state lifecycle.State,
	live map[string]process.Process,
) lifecycle.State {
	for key, managed := range state.Processes {
		proc, ok := live[key]
		if !ok {
			delete(state.Processes, key)
			continue
		}
		state = r.killIfEligible(ctx, state, key, managed, proc)
	}
	return state
}

func (r *Runner) killIfEligible(
	ctx context.Context,
	state lifecycle.State,
	key string,
	managed lifecycle.ManagedProcess,
	proc process.Process,
) lifecycle.State {
	session := state.Sessions[managed.TerminalSessionID]
	reason, ok := r.killReason(state, session)
	if !ok {
		return state
	}
	state.LastDecision = reason + ": " + managed.TerminalSessionID
	return r.handleEligibleProcess(ctx, state, key, proc, reason)
}

func (r *Runner) handleEligibleProcess(
	ctx context.Context,
	state lifecycle.State,
	key string,
	proc process.Process,
	reason string,
) lifecycle.State {
	if r.skipProtected(proc, reason) {
		return state
	}
	err := r.kill(ctx, proc, reason)
	if err != nil {
		state.LastError = err.Error()
		return state
	}
	delete(state.Processes, key)
	return state
}

func (r *Runner) skipProtected(proc process.Process, reason string) bool {
	if !r.cfg.IsProtected(proc.Name) {
		return false
	}
	applied := false
	reasons := []string{reason, "protected-process"}
	r.recordDecision(proc, reasons, applied, "")
	return true
}

func (r *Runner) killReason(
	state lifecycle.State,
	session lifecycle.TerminalSession,
) (string, bool) {
	if reason, ok := endedContextReason(state, session); ok {
		return reason, true
	}
	return r.sessionKillReason(session)
}

func endedContextReason(
	state lifecycle.State,
	session lifecycle.TerminalSession,
) (string, bool) {
	if presenceEnded(state.Tabs[session.TabID]) {
		return "tab-ended", true
	}
	if presenceEnded(state.Windows[session.WindowID]) {
		return "window-ended", true
	}
	return endedSessionReason(state, session)
}

func endedSessionReason(
	state lifecycle.State,
	session lifecycle.TerminalSession,
) (string, bool) {
	if presenceEnded(state.AgentSessions[session.AgentSessionID]) {
		return "agent-session-ended", true
	}
	if presenceEnded(state.UserSessions[session.UserSessionID]) {
		return "user-session-ended", true
	}
	return "", false
}

func presenceEnded(presence lifecycle.Presence) bool {
	hasID := presence.ID != ""
	missing := !presence.Exists
	ended := hasID && missing
	return ended
}

func (r *Runner) sessionKillReason(session lifecycle.TerminalSession) (string, bool) {
	if !session.Exists {
		return "session-ended", true
	}
	if !staleEligibleSession(session) {
		return "", false
	}
	if session.Active {
		return "", false
	}
	if session.InactiveAt.IsZero() {
		return "", false
	}
	return staleReason(session.InactiveAt, r.cfg.StaleLimit, r.now())
}

func staleEligibleSession(session lifecycle.TerminalSession) bool {
	return session.AgentSessionID != ""
}

func staleReason(inactiveAt time.Time, limit time.Duration, now time.Time) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	if now.Sub(inactiveAt) < limit {
		return "", false
	}
	return "session-stale", true
}

func (r *Runner) kill(ctx context.Context, proc process.Process, reason string) error {
	err := r.killer.Kill(ctx, proc)
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	applied := true
	r.recordDecision(proc, []string{reason}, applied, errorText)
	return err
}

func (r *Runner) recordDecision(
	proc process.Process,
	reasons []string,
	applied bool,
	errorText string,
) {
	if r.audit == nil {
		return
	}
	input := decisionAudit{
		proc:      proc,
		reasons:   reasons,
		applied:   applied,
		errorText: errorText,
	}
	_ = r.audit.Record(auditEvent(input))
}

type decisionAudit struct {
	proc      process.Process
	reasons   []string
	applied   bool
	errorText string
}

func auditEvent(input decisionAudit) audit.Event {
	proc := input.proc
	return audit.Event{
		Command:     "daemon",
		Action:      "kill",
		TargetType:  "process",
		Applied:     input.applied,
		PID:         proc.PID,
		Name:        proc.Name,
		CommandLine: proc.CommandLine,
		Cwd:         proc.Cwd,
		Reasons:     input.reasons,
		Error:       input.errorText,
	}
}

func setDaemonState(
	state *lifecycle.State,
	pid int,
	startedAt time.Time,
	lastTickAt time.Time,
) {
	state.Daemon.PID = pid
	state.Daemon.StartedAt = startedAt
	state.Daemon.LastTickAt = lastTickAt
}

func daemonClock(now func() time.Time) func() time.Time {
	if now != nil {
		return now
	}
	return time.Now
}

func daemonStartedAt(startedAt time.Time) time.Time {
	if !startedAt.IsZero() {
		return startedAt
	}
	return time.Now()
}
