package scan

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yowainwright/pk/internal/config"
	"github.com/yowainwright/pk/internal/process"
	"github.com/yowainwright/pk/internal/processtree"
)

type Action string

const (
	ActionKill   Action = "kill"
	ActionReport Action = "report"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type Report struct {
	Process     process.Process
	Descendants []process.Process
	Action      Action
	Confidence  Confidence
	Reasons     []string
}

type Scanner struct {
	cfg    *config.Config
	lister process.Lister
}

func New(cfg *config.Config, lister process.Lister) *Scanner {
	return &Scanner{cfg: cfg, lister: lister}
}

func (s *Scanner) Scan(ctx context.Context) ([]Report, error) {
	procs, err := s.lister.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}
	return Reports(s.cfg, procs), nil
}

func Reports(cfg *config.Config, procs []process.Process) []Report {
	reports := make([]Report, 0, len(procs))
	for _, proc := range procs {
		report, ok := reportForProcess(cfg, proc, procs)
		if ok {
			reports = append(reports, report)
		}
	}
	sortReports(reports)
	return reports
}

func WriteReports(w io.Writer, reports []Report) error {
	if len(reports) == 0 {
		_, err := fmt.Fprintln(w, "No matching processes found.")
		return err
	}

	if _, err := fmt.Fprintln(w, "PID\tACTION\tCONFIDENCE\tNAME\tREASONS"); err != nil {
		return err
	}
	return writeReportRows(w, reports)
}

func reportForProcess(
	cfg *config.Config,
	proc process.Process,
	procs []process.Process,
) (Report, bool) {
	reasons := reasonsForProcess(cfg, proc, procs)
	if len(reasons) == 0 {
		return Report{}, false
	}

	confidence := confidenceForReasons(reasons)
	action := actionForConfidence(confidence)
	descendants := reportDescendants(cfg, proc, procs)
	report := newReport(proc, descendants, action, confidence, reasons)
	return report, true
}

func reportDescendants(
	cfg *config.Config,
	proc process.Process,
	procs []process.Process,
) []process.Process {
	descendants := processtree.Descendants(procs, proc.PID)
	filtered := make([]process.Process, 0, len(descendants))
	for _, descendant := range descendants {
		if !cfg.IsProtected(descendant.Name) {
			filtered = append(filtered, descendant)
		}
	}
	return filtered
}

func newReport(
	proc process.Process,
	descendants []process.Process,
	action Action,
	confidence Confidence,
	reasons []string,
) Report {
	return Report{
		Process:     proc,
		Descendants: descendants,
		Action:      action,
		Confidence:  confidence,
		Reasons:     reasons,
	}
}

func protectedReasons(cfg *config.Config, proc process.Process) []string {
	thresholds := thresholdReasons(cfg, proc)
	if len(thresholds) == 0 {
		return nil
	}

	reasons := []string{"protected-process"}
	reasons = append(reasons, thresholds...)
	return reasons
}

func metadataReasons(proc process.Process) []string {
	reasons := make([]string, 0, 2)
	if proc.CommandLine == "" {
		reasons = append(reasons, "command-unavailable")
	}
	if proc.Cwd == "" {
		reasons = append(reasons, "cwd-unavailable")
	}
	return reasons
}

func commandReasons(proc process.Process) []string {
	command := strings.ToLower(proc.CommandLine)
	name := strings.ToLower(proc.Name)
	if isRestartableCommand(name, command) {
		return []string{"restartable-command"}
	}
	return nil
}

func locationReasons(proc process.Process) []string {
	if isDevPath(proc.Cwd) {
		return []string{"dev-cwd"}
	}
	return nil
}

func thresholdReasons(cfg *config.Config, proc process.Process) []string {
	reasons := make([]string, 0, 2)
	if proc.CPUPercent > cfg.CPUThreshold {
		reasons = append(reasons, "high-cpu")
	}
	if proc.MemoryMB > cfg.MemoryThreshold {
		reasons = append(reasons, "high-memory")
	}
	return reasons
}

func confidenceForReasons(reasons []string) Confidence {
	lowConfidence := hasMissingMetadata(reasons) || hasReason(reasons, "protected-process")
	if lowConfidence {
		return ConfidenceLow
	}
	if hasHighConfidenceReasons(reasons) {
		return ConfidenceHigh
	}
	if hasReason(reasons, "restartable-command") {
		return ConfidenceMedium
	}
	return ConfidenceLow
}

func reasonsForProcess(
	cfg *config.Config,
	proc process.Process,
	procs []process.Process,
) []string {
	if cfg.IsProtected(proc.Name) {
		return protectedReasons(cfg, proc)
	}

	reasons := cleanupReasons(cfg, proc, procs)
	if len(reasons) == 0 {
		return nil
	}
	reasons = append(metadataReasons(proc), reasons...)
	return reasons
}

func cleanupReasons(
	cfg *config.Config,
	proc process.Process,
	procs []process.Process,
) []string {
	reasons := commandReasons(proc)
	reasons = append(reasons, ownershipReasons(proc, procs)...)
	reasons = append(reasons, locationReasons(proc)...)
	reasons = append(reasons, thresholdReasons(cfg, proc)...)
	if hasOnlyLocationReason(reasons) {
		return nil
	}
	if hasOnlyCommandReason(reasons) {
		return nil
	}
	if hasOnlyOwnershipReason(reasons) {
		return nil
	}
	return reasons
}

func ownershipReasons(proc process.Process, procs []process.Process) []string {
	if hasAncestor(proc, procs, isAgentProcess) {
		return []string{"agent-owned"}
	}
	if proc.Cwd == "" {
		return nil
	}
	if hasAncestor(proc, procs, isSessionRoot) {
		return []string{"session-owned"}
	}
	return nil
}

func hasOnlyLocationReason(reasons []string) bool {
	hasOneReason := len(reasons) == 1
	hasLocation := hasReason(reasons, "dev-cwd")
	return hasOneReason && hasLocation
}

func hasOnlyCommandReason(reasons []string) bool {
	hasOneReason := len(reasons) == 1
	hasCommand := hasReason(reasons, "restartable-command")
	return hasOneReason && hasCommand
}

func hasOnlyOwnershipReason(reasons []string) bool {
	hasOneReason := len(reasons) == 1
	hasOwnership := hasReason(reasons, "agent-owned") || hasReason(reasons, "session-owned")
	return hasOneReason && hasOwnership
}

func hasHighConfidenceReasons(reasons []string) bool {
	hasCommand := hasReason(reasons, "restartable-command")
	hasLocation := hasReason(reasons, "dev-cwd")
	hasOwnership := hasReason(reasons, "agent-owned") || hasReason(reasons, "session-owned")
	hasRequiredReasons := hasCommand && (hasLocation || hasOwnership)
	return hasRequiredReasons
}

func actionForConfidence(confidence Confidence) Action {
	if confidence == ConfidenceHigh {
		return ActionKill
	}
	return ActionReport
}

func hasMissingMetadata(reasons []string) bool {
	commandMissing := hasReason(reasons, "command-unavailable")
	cwdMissing := hasReason(reasons, "cwd-unavailable")
	return commandMissing || cwdMissing
}

func hasReason(reasons []string, reason string) bool {
	for _, current := range reasons {
		if current == reason {
			return true
		}
	}
	return false
}

func isRestartableCommand(name string, command string) bool {
	commands := []string{
		"node", "bun", "npm", "pnpm", "yarn",
		"go", "python", "python3", "pytest",
		"uvicorn", "rails", "vite", "next",
	}
	return matchesAnyCommand(name, command, commands)
}

func isAgentProcess(proc process.Process) bool {
	commands := []string{"codex", "claude", "aider", "opencode", "goose"}
	return matchesAnyCommand(proc.Name, proc.CommandLine, commands)
}

func isSessionRoot(proc process.Process) bool {
	commands := []string{
		"terminal", "iterm2", "ghostty", "code", "cursor", "zed",
		"tmux", "bash", "zsh", "fish", "codex", "claude",
	}
	return matchesAnyCommand(proc.Name, proc.CommandLine, commands)
}

func hasAncestor(
	proc process.Process,
	procs []process.Process,
	matches func(process.Process) bool,
) bool {
	walker := ancestorWalker{
		byPID: processesByPID(procs),
		seen:  map[int32]bool{proc.PID: true},
	}
	return walker.hasMatch(proc.ParentPID, matches)
}

type ancestorWalker struct {
	byPID map[int32]process.Process
	seen  map[int32]bool
}

func (w *ancestorWalker) hasMatch(
	parentPID int32,
	matches func(process.Process) bool,
) bool {
	for parentPID != 0 {
		ancestor, ok := w.next(parentPID)
		if !ok {
			return false
		}
		if matches(ancestor) {
			return true
		}
		parentPID = ancestor.ParentPID
	}
	return false
}

func (w *ancestorWalker) next(pid int32) (process.Process, bool) {
	ancestor, ok := w.byPID[pid]
	shouldStop := !ok || w.seen[pid]
	if shouldStop {
		return process.Process{}, false
	}
	w.seen[pid] = true
	return ancestor, true
}

func processesByPID(procs []process.Process) map[int32]process.Process {
	byPID := make(map[int32]process.Process, len(procs))
	for _, proc := range procs {
		byPID[proc.PID] = proc
	}
	return byPID
}

func matchesAnyCommand(name string, command string, commands []string) bool {
	candidates := executableCandidates(name, command)
	for _, current := range commands {
		if candidates[current] {
			return true
		}
	}
	return false
}

func executableCandidates(name string, command string) map[string]bool {
	candidates := make(map[string]bool, 2)
	addExecutable(candidates, name)
	fields := strings.Fields(command)
	if len(fields) > 0 {
		addExecutable(candidates, fields[0])
	}
	return candidates
}

func addExecutable(candidates map[string]bool, value string) {
	executable := executableName(value)
	if executable == "" {
		return
	}
	candidates[executable] = true
}

func executableName(value string) string {
	value = strings.Trim(value, `"'`)
	value = strings.ToLower(value)
	return filepath.Base(value)
}

func isDevPath(path string) bool {
	cleanPath := filepath.Clean(path)
	devParts := []string{"/code", "/src", "/Developer", "/workspace"}
	for _, part := range devParts {
		if cleanPath == part {
			return true
		}
		if strings.Contains(cleanPath, part+"/") {
			return true
		}
	}
	return false
}

func sortReports(reports []Report) {
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Process.PID < reports[j].Process.PID
	})
}

func writeReportRows(w io.Writer, reports []Report) error {
	for _, report := range reports {
		if err := writeReportRow(w, report); err != nil {
			return err
		}
	}
	return nil
}

func writeReportRow(w io.Writer, report Report) error {
	_, err := fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
		report.Process.PID,
		report.Action,
		report.Confidence,
		report.Process.Name,
		strings.Join(report.Reasons, ","),
	)
	return err
}
