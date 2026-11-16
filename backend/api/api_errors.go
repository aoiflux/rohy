package api

import (
	"encoding/json"
	"errors"

	"rohy/backend/consts"
)

// MarshalError is the app's Wails MarshalError: how a failed binding call reaches the frontend.
//
// Without it the page gets only err.Error(), so the code an ErrorEvent carries — case_read_only,
// case_busy, query_error — would be dropped at the boundary, and every screen could say only what
// went wrong, never whose fault it was.
//
// It returns the JSON of an ErrorEvent. The runtime rejects the call with a RuntimeError whose
// cause is that JSON, decoded; frontend/src/lib/api/errors.js reads it in call(), the one place the
// frontend calls the backend, so a handler reading .message reads the message, as before.
//
// The message is the OUTER error's, so wrapping context survives. An uncoded error is
// internal_error. An empty message becomes the code, so a page always has something to show.
//
// It sees the error only, never the call's arguments, so it cannot write a passphrase anywhere.
func MarshalError(err error) []byte {
	if err == nil {
		err = errors.New(consts.ErrCodeInternal)
	}
	out := ErrorEvent{Code: consts.ErrCodeInternal, Message: describe(err)}
	var coded ErrorEvent
	if errors.As(err, &coded) && coded.Code != "" {
		out.Code = coded.Code
	}
	if out.Message == "" {
		out.Message = out.Code
	}
	data, merr := json.Marshal(out)
	if merr != nil { // two strings cannot fail to encode; Wails must never get invalid JSON if they do
		data, _ = json.Marshal(ErrorEvent{Code: out.Code, Message: out.Code})
	}
	return data
}
