package api

import (
	"context"
	"log/slog"
)

// SafeLogHandler keeps call arguments and results out of the log.
//
// 🔒 Wails logs every binding call at Debug with its arguments and its result, each as an
// attribute ("Binding call complete:" ... "args", "result"), and every other runtime call with
// its arguments ("Runtime call:" ... "args"). A passphrase crosses a call as an argument, so those
// attributes are the one place it could be written down. This handler withholds them at every
// level, so turning the log up for a bug report cannot turn the passphrase on with it.
//
// It keeps the attribute's key with a placeholder, so a line still says that something was
// withheld rather than looking complete.
type SafeLogHandler struct{ inner slog.Handler }

// NewSafeLogHandler wraps inner.
func NewSafeLogHandler(inner slog.Handler) *SafeLogHandler { return &SafeLogHandler{inner: inner} }

// withheldKeys are the attributes Wails puts call data in. A test in package main pins them to
// the Wails source, so an upgrade that renames one fails there instead of leaking here.
var withheldKeys = map[string]bool{"args": true, "result": true}

const withheldValue = "(withheld)"

func withhold(a slog.Attr) slog.Attr {
	if withheldKeys[a.Key] {
		return slog.String(a.Key, withheldValue)
	}
	if a.Value.Kind() == slog.KindGroup {
		attrs := a.Value.Group()
		safe := make([]any, len(attrs))
		for i, g := range attrs {
			safe[i] = withhold(g)
		}
		return slog.Group(a.Key, safe...)
	}
	return a
}

func (h *SafeLogHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *SafeLogHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(withhold(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *SafeLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	safe := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		safe[i] = withhold(a)
	}
	return &SafeLogHandler{inner: h.inner.WithAttrs(safe)}
}

func (h *SafeLogHandler) WithGroup(name string) slog.Handler {
	return &SafeLogHandler{inner: h.inner.WithGroup(name)}
}
