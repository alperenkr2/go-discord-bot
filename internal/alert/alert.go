// Package alert delivers out-of-band notifications (Telegram, Discord channel
// mention, logs) when the bot needs the operator's attention — most importantly
// when OwO shows a captcha.
package alert

import (
	"context"
	"log/slog"
)

// Level describes how urgent a notification is.
type Level int

const (
	Info Level = iota
	Warn
	Critical
)

func (l Level) String() string {
	switch l {
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	case Critical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// Notifier delivers a message somewhere the operator will see it. Implementations
// are best-effort: they handle and log their own errors rather than returning
// them, so callers never have to care whether an alert got through.
type Notifier interface {
	Notify(ctx context.Context, level Level, text string)
}

// Multi fans a notification out to several notifiers. A failure in one does not
// stop the others.
type Multi struct {
	notifiers []Notifier
}

// NewMulti builds a Multi, dropping any nil notifiers.
func NewMulti(notifiers ...Notifier) *Multi {
	out := make([]Notifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n != nil {
			out = append(out, n)
		}
	}
	return &Multi{notifiers: out}
}

func (m *Multi) Notify(ctx context.Context, level Level, text string) {
	for _, n := range m.notifiers {
		n.Notify(ctx, level, text)
	}
}

// Log is a Notifier that writes alerts to the structured logger. It is always
// included so that every alert is recorded even if external channels fail.
type Log struct {
	logger *slog.Logger
}

func NewLog(logger *slog.Logger) *Log { return &Log{logger: logger} }

func (l *Log) Notify(_ context.Context, level Level, text string) {
	switch level {
	case Critical:
		l.logger.Error("alert", "level", level.String(), "text", text)
	case Warn:
		l.logger.Warn("alert", "level", level.String(), "text", text)
	default:
		l.logger.Info("alert", "level", level.String(), "text", text)
	}
}
