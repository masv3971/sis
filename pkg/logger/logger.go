// Package logger is a small wrapper over log/slog that adds the vc-style named
// sub-logger: New("child") nests under its parent so every record carries a
// dotted "logger" path (e.g. "sis.httpserver.request"). slog has no WithName,
// so the name is rebuilt from a base handler each time rather than stacked as
// repeated attributes. Structure mirrors SUNET/vc's pkg/logger, reimplemented
// for sis on slog.
package logger

import (
	"io"
	"log/slog"
	"os"
)

// nameKey is the attribute under which the dotted logger path is recorded.
const nameKey = "logger"

// Log wraps a *slog.Logger and remembers the pieces needed to derive a child:
// the base handler (without name or attrs) and the current name and attrs. The
// embedded *slog.Logger provides Info/Warn/Error/Debug and their Context forms.
type Log struct {
	*slog.Logger

	handler slog.Handler
	name    string
	attrs   []any
}

// build assembles a Log from a base handler, a name and accumulated attrs. The
// name and attrs are applied to a fresh logger off the base handler, so a child
// never inherits a duplicate "logger" attribute from its parent.
func build(handler slog.Handler, name string, attrs []any) *Log {
	lg := slog.New(handler)
	if name != "" {
		lg = lg.With(nameKey, name)
	}
	if len(attrs) > 0 {
		lg = lg.With(attrs...)
	}
	return &Log{Logger: lg, handler: handler, name: name, attrs: attrs}
}

// New creates a root logger named name, writing to stdout. production selects a
// JSON handler (for log pipelines); otherwise a text handler (human-readable).
func New(name string, production bool) *Log {
	return NewWithWriter(name, production, os.Stdout)
}

// NewWithWriter is New with an explicit destination, useful in tests.
func NewWithWriter(name string, production bool, w io.Writer) *Log {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var h slog.Handler
	if production {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return build(h, name, nil)
}

// NewSimple wraps the default slog logger under name, for quick or standalone use.
func NewSimple(name string) *Log {
	return build(slog.Default().Handler(), name, nil)
}

// New returns a sub-logger whose name is the parent's name plus path, dotted —
// the slog analogue of vc's WithName. Attributes bound with With are carried over.
func (l *Log) New(path string) *Log {
	name := path
	if l.name != "" {
		name = l.name + "." + path
	}
	return build(l.handler, name, l.attrs)
}

// With returns a logger that adds the given key/value attributes to every record,
// keeping the name. It shadows the embedded slog.Logger.With so the result stays
// a *Log and can still spawn named children.
func (l *Log) With(args ...any) *Log {
	next := make([]any, 0, len(l.attrs)+len(args))
	next = append(next, l.attrs...)
	next = append(next, args...)
	return build(l.handler, l.name, next)
}
