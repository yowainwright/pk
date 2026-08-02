package dx

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

func (u *UI) Logger() *slog.Logger {
	return slog.New(&logHandler{ui: u})
}

type logHandler struct {
	ui     *UI
	attrs  []slog.Attr
	groups []string
}

func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *logHandler) Handle(_ context.Context, record slog.Record) error {
	parts := make([]string, 0, record.NumAttrs()+len(h.attrs))
	parts = appendAttributes(parts, h.groups, h.attrs)
	record.Attrs(func(attr slog.Attr) bool {
		parts = appendAttribute(parts, h.groups, attr)
		return true
	})
	line := h.formatRecord(record, parts)
	return h.ui.writeLog(line)
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

func (h *logHandler) formatRecord(record slog.Record, attrs []string) string {
	parts := make([]string, 0, len(attrs)+3)
	if h.ui.timestamps {
		parts = append(parts, style(h.ui.color, Muted, record.Time.Format("15:04:05")))
	}
	label := style(h.ui.color, logKind(record.Level), logLabel(record.Level))
	parts = append(parts, label, record.Message)
	parts = append(parts, attrs...)
	return strings.Join(parts, " ")
}

func (u *UI) writeLog(line string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := fmt.Fprintln(u.err, line)
	if err != nil {
		return fmt.Errorf("writing log: %w", err)
	}
	return nil
}

func appendAttributes(parts []string, groups []string, attrs []slog.Attr) []string {
	for _, attr := range attrs {
		parts = appendAttribute(parts, groups, attr)
	}
	return parts
}

func appendAttribute(parts []string, groups []string, attr slog.Attr) []string {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return parts
	}
	if attr.Value.Kind() == slog.KindGroup {
		return appendGroup(parts, groups, attr)
	}
	key := attributeKey(groups, attr.Key)
	value := formatValue(attr.Value)
	attribute := key + "=" + value
	return append(parts, attribute)
}

func appendGroup(parts []string, groups []string, attr slog.Attr) []string {
	if attr.Key != "" {
		groups = append(append([]string(nil), groups...), attr.Key)
	}
	return appendAttributes(parts, groups, attr.Value.Group())
}

func attributeKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	return strings.Join(append(append([]string(nil), groups...), key), ".")
}

func formatValue(value slog.Value) string {
	if value.Kind() == slog.KindString {
		return quoteIfNeeded(value.String())
	}
	return quoteIfNeeded(fmt.Sprint(value.Any()))
}

func quoteIfNeeded(value string) string {
	isEmpty := value == ""
	hasReservedCharacter := strings.ContainsAny(value, " \t\n\r=\"")
	shouldQuote := isEmpty || hasReservedCharacter
	if shouldQuote {
		return strconv.Quote(value)
	}
	return value
}

func logLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERR"
	case level >= slog.LevelWarn:
		return "WRN"
	case level >= slog.LevelInfo:
		return "INF"
	default:
		return "DBG"
	}
}

func logKind(level slog.Level) Kind {
	switch {
	case level >= slog.LevelError:
		return Failure
	case level >= slog.LevelWarn:
		return Warning
	case level >= slog.LevelInfo:
		return Accent
	default:
		return Muted
	}
}
