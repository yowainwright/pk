package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/yowainwright/pk/internal/audit"
	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/process"
)

type StateStore interface {
	TakeEvents() ([]Event, error)
	AcknowledgeEvents() error
	LoadState() (State, error)
	SaveState(State) error
}

type Audit interface {
	Record(audit.Event) error
}

type RunnerOptions struct {
	Now            func() time.Time
	StartedAt      time.Time
	ReadCreateTime func(context.Context, int32) (int64, error)
}

type Runner struct {
	cfg            *config.Config
	lister         process.Lister
	killer         process.Killer
	store          StateStore
	audit          Audit
	now            func() time.Time
	startedAt      time.Time
	readCreateTime func(context.Context, int32) (int64, error)
}

const maxAppliedEventIDs = 2048

func NewRunner(
	cfg *config.Config,
	lister process.Lister,
	k process.Killer,
	store StateStore,
	audit Audit,
	options RunnerOptions,
) *Runner {
	return &Runner{
		cfg:            cfg,
		lister:         lister,
		killer:         k,
		store:          store,
		audit:          audit,
		now:            daemonClock(options.Now),
		startedAt:      daemonStartedAt(options.StartedAt),
		readCreateTime: daemonCreateTimeReader(options.ReadCreateTime),
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
	if err := r.cfg.Reload(); err != nil {
		return r.saveError(state, err)
	}
	state = r.applyCurrentEvents(state, events)
	procs, err := r.lister.List(ctx)
	if err != nil {
		return r.saveError(state, err)
	}
	state, err = r.reconcile(ctx, state, procs)
	if err != nil {
		return r.saveError(state, err)
	}
	return r.finishTick(state, events)
}

func (r *Runner) applyCurrentEvents(
	state State,
	events []Event,
) State {
	if r.cfg.SessionSince == 0 {
		return applyEvents(state, events)
	}
	r.removeEarlierSessions(&state)
	current := make([]Event, 0, len(events))
	for _, event := range events {
		if r.currentEvent(event) {
			current = append(current, event)
		}
	}
	return applyEvents(state, current)
}

func (r *Runner) currentEvent(event Event) bool {
	if requiresTerminalSession(event.Kind) {
		return event.ShellCreateTime >= r.cfg.SessionSince
	}
	return event.ObservedAt.UnixMilli() >= r.cfg.SessionSince
}

func (r *Runner) removeEarlierSessions(state *State) {
	for id, session := range state.Sessions {
		if session.ShellProcessKey.CreateTime < r.cfg.SessionSince {
			delete(state.Sessions, id)
		}
	}
	for key, proc := range state.Processes {
		if _, exists := state.Sessions[proc.TerminalSessionID]; !exists {
			delete(state.Processes, key)
		}
	}
}

func (r *Runner) finishTick(state State, events []Event) error {
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

func (r *Runner) load() (State, []Event, error) {
	state, err := r.store.LoadState()
	if err != nil {
		return State{}, nil, fmt.Errorf("loading lifecycle state: %w", err)
	}
	events, err := r.store.TakeEvents()
	if err != nil {
		return State{}, nil, fmt.Errorf("taking lifecycle events: %w", err)
	}
	state.Ensure()
	return state, events, nil
}

func (r *Runner) saveError(state State, cause error) error {
	state.LastError = cause.Error()
	state.Daemon.LastError = cause.Error()
	setDaemonState(&state, os.Getpid(), r.startedAt, r.now())
	if err := r.store.SaveState(state); err != nil {
		return fmt.Errorf("%w; saving lifecycle error: %v", cause, err)
	}
	return cause
}

func applyEvents(state State, events []Event) State {
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

func retainAppliedEvents(state *State, events []Event) {
	if len(state.AppliedEvents) <= maxAppliedEventIDs {
		return
	}
	current := currentEventIDs(events)
	pruneAppliedEvents(state.AppliedEvents, current)
}

func currentEventIDs(events []Event) map[string]bool {
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

func applyEvent(state State, event Event) State {
	if requiresTerminalSession(event.Kind) {
		applySessionEvent(&state, event)
		return state
	}
	applyContextPresenceEvent(&state, event)
	state.LastDecision = "context event: " + event.Kind
	return state
}

func applySessionEvent(state *State, event Event) {
	session := sessionFromEvent(*state, event)
	switch event.Kind {
	case KindSessionStop:
		markSessionEnded(&session, event.ObservedAt)
	case KindSessionInactive, KindCommandFinish:
		markSessionInactive(&session, event.ObservedAt)
	default:
		markSessionActive(&session, event.ObservedAt)
	}
	state.Sessions[event.TerminalSessionID] = session
	applySessionPresenceEvent(state, event)
}

func sessionFromEvent(state State, event Event) TerminalSession {
	session := state.Sessions[event.TerminalSessionID]
	if session.ID == "" {
		session.ID = event.TerminalSessionID
		session.StartedAt = event.ObservedAt
	}
	session.ShellProcessKey = ProcessKey{
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

func applyContextPresenceEvent(state *State, event Event) {
	applyPresenceByID(state.Tabs, event.TabID, event)
	applyPresenceByID(state.Windows, event.WindowID, event)
	applyPresenceByID(state.AgentSessions, event.AgentSessionID, event)
	applyPresenceByID(state.UserSessions, event.UserSessionID, event)
}

func applySessionPresenceEvent(state *State, event Event) {
	applyPresenceByID(state.Tabs, event.TabID, event)
	if event.Kind == KindSessionStop {
		return
	}
	touchPresenceByID(state.Windows, event.WindowID, event)
	touchPresenceByID(state.AgentSessions, event.AgentSessionID, event)
	touchPresenceByID(state.UserSessions, event.UserSessionID, event)
}

func applyPresenceByID(
	presences map[string]Presence,
	id string,
	event Event,
) {
	if id == "" {
		return
	}
	presence := presenceFromEvent(presences[id], id, event)
	presences[id] = presence
}

func touchPresenceByID(
	presences map[string]Presence,
	id string,
	event Event,
) {
	if id == "" {
		return
	}
	presence := touchPresence(presences[id], id, event.ObservedAt)
	presences[id] = presence
}

func touchPresence(
	presence Presence,
	id string,
	at time.Time,
) Presence {
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
	presence Presence,
	id string,
	event Event,
) Presence {
	if presence.ID == "" {
		presence.ID = id
		presence.StartedAt = event.ObservedAt
	}
	presence.LastSeenAt = event.ObservedAt
	return markPresence(presence, event)
}

func markPresence(
	presence Presence,
	event Event,
) Presence {
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
	case KindSessionStop, KindContextStop:
		return true
	default:
		return false
	}
}

func presenceInactiveKind(kind string) bool {
	switch kind {
	case KindSessionInactive, KindCommandFinish:
		return true
	default:
		return false
	}
}

func activePresence(presence Presence) Presence {
	presence.Exists = true
	presence.Active = true
	presence.InactiveAt = time.Time{}
	presence.EndedAt = time.Time{}
	return presence
}

func inactivePresence(
	presence Presence,
	at time.Time,
) Presence {
	presence.Exists = true
	presence.Active = false
	presence.InactiveAt = at
	return presence
}

func endedPresence(
	presence Presence,
	at time.Time,
) Presence {
	presence.Exists = false
	presence.Active = false
	presence.EndedAt = at
	return presence
}

func markSessionActive(session *TerminalSession, at time.Time) {
	session.Exists = true
	session.Active = true
	session.InactiveAt = time.Time{}
	session.EndedAt = time.Time{}
}

func markSessionInactive(session *TerminalSession, at time.Time) {
	session.Exists = true
	session.Active = false
	session.InactiveAt = at
}

func markSessionEnded(session *TerminalSession, at time.Time) {
	session.Exists = false
	session.Active = false
	session.EndedAt = at
}

func (r *Runner) reconcile(
	ctx context.Context,
	state State,
	procs []process.Process,
) (State, error) {
	live := liveProcesses(procs)
	state, deferred := r.reconcileSessions(ctx, state, procs, live)
	if err := ctx.Err(); err != nil {
		return state, err
	}
	return r.killEligible(ctx, state, live, deferred), nil
}

func (r *Runner) reconcileSessions(
	ctx context.Context,
	state State,
	procs []process.Process,
	live map[string]process.Process,
) (State, map[string]bool) {
	deferred := make(map[string]bool)
	tree := process.NewIndex(procs)
	for _, session := range state.Sessions {
		var err error
		state, err = r.reconcileSession(ctx, state, session, tree, live)
		if err == nil {
			continue
		}
		deferred[session.ID] = true
		state.LastError = err.Error()
		state.Daemon.LastError = err.Error()
	}
	return state, deferred
}

func liveProcesses(procs []process.Process) map[string]process.Process {
	live := make(map[string]process.Process, len(procs))
	for _, proc := range procs {
		key := ProcessKeyString(proc.PID, proc.CreateTime)
		live[key] = proc
	}
	return live
}

func (r *Runner) reconcileSession(
	ctx context.Context,
	state State,
	session TerminalSession,
	tree *process.Index,
	live map[string]process.Process,
) (State, error) {
	if sessionEnded(session) {
		state.Sessions[session.ID] = session
		return state, nil
	}
	return r.reconcileLiveSession(ctx, state, session, tree, live)
}

func (r *Runner) reconcileLiveSession(
	ctx context.Context,
	state State,
	session TerminalSession,
	tree *process.Index,
	live map[string]process.Process,
) (State, error) {
	exists, err := r.processExists(ctx, session.ShellProcessKey, live)
	if err != nil {
		return state, fmt.Errorf("checking session %s: %w", session.ID, err)
	}
	if !exists {
		return sessionMissing(state, session, r.now()), nil
	}
	state.Sessions[session.ID] = liveSession(session, r.now())
	return trackDescendants(state, session.ID, session.ShellProcessKey.PID, tree, r.now()), nil
}

func (r *Runner) processExists(
	ctx context.Context,
	key ProcessKey,
	live map[string]process.Process,
) (bool, error) {
	if _, exists := live[key.String()]; exists {
		return true, nil
	}
	createTime, err := r.readCreateTime(ctx, key.PID)
	if process.IsGone(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if createTime <= 0 {
		return false, fmt.Errorf("process %d has no creation time", key.PID)
	}
	return createTime == key.CreateTime, nil
}

func sessionEnded(session TerminalSession) bool {
	missing := !session.Exists
	hasEndedAt := !session.EndedAt.IsZero()
	ended := missing && hasEndedAt
	return ended
}

func sessionMissing(
	state State,
	session TerminalSession,
	now time.Time,
) State {
	if session.EndedAt.IsZero() {
		session.EndedAt = now
	}
	session.Exists = false
	session.Active = false
	state.Sessions[session.ID] = session
	state.LastDecision = "session missing: " + session.ID
	return state
}

func liveSession(session TerminalSession, now time.Time) TerminalSession {
	session.Exists = true
	session.LastSeenAt = now
	return session
}

func trackDescendants(
	state State,
	sessionID string,
	rootPID int32,
	tree *process.Index,
	now time.Time,
) State {
	for _, proc := range tree.Descendants(rootPID) {
		state = trackProcess(state, sessionID, proc, now)
	}
	return state
}

func trackProcess(
	state State,
	sessionID string,
	proc process.Process,
	now time.Time,
) State {
	key := ProcessKeyString(proc.PID, proc.CreateTime)
	managed := state.Processes[key]
	if managed.FirstSeenAt.IsZero() {
		managed.FirstSeenAt = now
	}
	managed.ProcessKey = ProcessKey{PID: proc.PID, CreateTime: proc.CreateTime}
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
	state State,
	live map[string]process.Process,
	deferred map[string]bool,
) State {
	for key, managed := range state.Processes {
		if deferred[managed.TerminalSessionID] {
			continue
		}
		proc, ok := live[key]
		if !ok {
			state = r.pruneMissingProcess(ctx, state, managed.ProcessKey)
			continue
		}
		state = r.killIfEligible(ctx, state, key, managed, proc)
	}
	return state
}

func (r *Runner) pruneMissingProcess(
	ctx context.Context,
	state State,
	key ProcessKey,
) State {
	exists, err := r.processExists(ctx, key, nil)
	if err != nil {
		state.LastError = fmt.Sprintf("checking managed process %s: %v", key.String(), err)
		state.Daemon.LastError = state.LastError
		return state
	}
	if !exists {
		delete(state.Processes, key.String())
	}
	return state
}

func (r *Runner) killIfEligible(
	ctx context.Context,
	state State,
	key string,
	managed ManagedProcess,
	proc process.Process,
) State {
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
	state State,
	key string,
	proc process.Process,
	reason string,
) State {
	protected, err := r.skipProtected(proc, reason)
	if err != nil {
		state.LastError = err.Error()
	}
	if protected {
		return state
	}
	return r.killManagedProcess(ctx, state, key, proc, reason)
}

func (r *Runner) killManagedProcess(
	ctx context.Context,
	state State,
	key string,
	proc process.Process,
	reason string,
) State {
	err := r.kill(ctx, proc, reason)
	if err != nil {
		state.LastError = err.Error()
		return state
	}
	delete(state.Processes, key)
	return state
}

func (r *Runner) skipProtected(proc process.Process, reason string) (bool, error) {
	if !r.cfg.IsProtected(proc.Name) {
		return false, nil
	}
	applied := false
	reasons := []string{reason, "protected-process"}
	return true, r.recordDecision(proc, reasons, applied, "")
}

func (r *Runner) killReason(
	state State,
	session TerminalSession,
) (string, bool) {
	if reason, ok := endedContextReason(state, session); ok {
		return reason, true
	}
	return r.sessionKillReason(session)
}

func endedContextReason(
	state State,
	session TerminalSession,
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
	state State,
	session TerminalSession,
) (string, bool) {
	if presenceEnded(state.AgentSessions[session.AgentSessionID]) {
		return "agent-session-ended", true
	}
	if presenceEnded(state.UserSessions[session.UserSessionID]) {
		return "user-session-ended", true
	}
	return "", false
}

func presenceEnded(presence Presence) bool {
	hasID := presence.ID != ""
	missing := !presence.Exists
	ended := hasID && missing
	return ended
}

func (r *Runner) sessionKillReason(session TerminalSession) (string, bool) {
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

func staleEligibleSession(session TerminalSession) bool {
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
	auditErr := r.recordDecision(proc, []string{reason}, applied, errorText)
	return errors.Join(err, auditErr)
}

func (r *Runner) recordDecision(
	proc process.Process,
	reasons []string,
	applied bool,
	errorText string,
) error {
	if r.audit == nil {
		return nil
	}
	input := decisionAudit{
		proc:      proc,
		reasons:   reasons,
		applied:   applied,
		errorText: errorText,
	}
	if err := r.audit.Record(auditEvent(input)); err != nil {
		return fmt.Errorf("recording cleanup audit: %w", err)
	}
	return nil
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
	state *State,
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

func daemonCreateTimeReader(
	read func(context.Context, int32) (int64, error),
) func(context.Context, int32) (int64, error) {
	if read != nil {
		return read
	}
	return process.CreateTime
}

func daemonStartedAt(startedAt time.Time) time.Time {
	if !startedAt.IsZero() {
		return startedAt
	}
	return time.Now()
}
