package api

import (
	"strings"

	"github.com/wailsapp/wails/v2/pkg/logger"
)

// SafeLogger keeps call arguments out of the log (item 41).
//
// 🔒 Wails logs the raw call message — arguments included — when a call's arguments fail to parse
// or a bound method panics (dispatcher.go: "process message error: <message> -> <err>"). A
// passphrase crosses a call as an argument, so that line is the one place it could be written
// down. This wrapper drops the message and keeps the error.
type SafeLogger struct{ inner logger.Logger }

// NewSafeLogger wraps inner.
func NewSafeLogger(inner logger.Logger) SafeLogger { return SafeLogger{inner: inner} }

const callErrorPrefix = "process message error: "

// scrub withholds everything before the last separator Wails adds: the call itself.
func scrub(m string) string {
	i := strings.Index(m, callErrorPrefix)
	if i < 0 {
		return m
	}
	head := m[:i+len(callErrorPrefix)]
	if j := strings.LastIndex(m, " -> "); j >= i {
		return head + "(call withheld) -> " + m[j+len(" -> "):]
	}
	return head + "(call withheld)"
}

func (l SafeLogger) Print(m string)   { l.inner.Print(scrub(m)) }
func (l SafeLogger) Trace(m string)   { l.inner.Trace(scrub(m)) }
func (l SafeLogger) Debug(m string)   { l.inner.Debug(scrub(m)) }
func (l SafeLogger) Info(m string)    { l.inner.Info(scrub(m)) }
func (l SafeLogger) Warning(m string) { l.inner.Warning(scrub(m)) }
func (l SafeLogger) Error(m string)   { l.inner.Error(scrub(m)) }
func (l SafeLogger) Fatal(m string)   { l.inner.Fatal(scrub(m)) }
