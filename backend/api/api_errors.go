package api

import (
	"encoding/json"
	"errors"

	"rohy/backend/consts"
)

// FormatError is the app's Wails ErrorFormatter: how a failed binding call reaches the frontend.
//
// Without it Wails sends err.Error(), so the code an ErrorEvent carries — case_read_only,
// case_busy, query_error — was dropped at the boundary, and every screen could say only what went
// wrong, never whose fault it was.
//
// 🔒 It returns a STRING. The runtime rejects with `x instanceof Error ? x : new Error(x)`, so an
// object would reach every handler as "[object Object]". The string is the JSON of an ErrorEvent;
// frontend/src/lib/api/errors.js decodes it in call(), the one place the frontend calls the
// backend, so a handler reading .message reads the message, as before.
//
// The message is the OUTER error's, so wrapping context survives. An uncoded error is
// internal_error. An empty message becomes the code: an empty error field makes the runtime resolve
// the call instead of rejecting it.
//
// It sees the error only, never the call's arguments, so it cannot write a passphrase anywhere the
// SafeLogger does not already guard.
func FormatError(err error) any {
	if err == nil {
		err = errors.New(consts.ErrCodeInternal)
	}
	out := ErrorEvent{Code: consts.ErrCodeInternal, Message: err.Error()}
	var coded ErrorEvent
	if errors.As(err, &coded) && coded.Code != "" {
		out.Code = coded.Code
	}
	if out.Message == "" {
		out.Message = out.Code
	}
	data, merr := json.Marshal(out)
	if merr != nil { // two strings cannot fail to encode; never cross as success if they do
		return out.Code
	}
	return string(data)
}
