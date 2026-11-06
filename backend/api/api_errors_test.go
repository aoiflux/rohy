package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"rohy/backend/consts"
)

// formatted runs FormatError and decodes what the frontend will receive. Wails puts the formatter's
// value in the callback's `error` field and the runtime turns it into `new Error(value)`, so only a
// non-empty string survives the crossing.
func formatted(t *testing.T, err error) ErrorEvent {
	t.Helper()
	s, ok := FormatError(err).(string)
	if !ok {
		t.Fatalf("FormatError returned %T, want a string: the runtime turns anything else into %q", FormatError(err), "[object Object]")
	}
	if s == "" {
		t.Fatal("FormatError returned an empty string: the runtime would resolve the call instead of rejecting it")
	}
	var ev ErrorEvent
	if err := json.Unmarshal([]byte(s), &ev); err != nil {
		t.Fatalf("FormatError returned %q, not JSON: %v", s, err)
	}
	return ev
}

func TestEveryBindingErrorCrossesWithItsCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want ErrorEvent
	}{
		{"coded", AsError(consts.ErrCodeReadOnly, errors.New("the signing key is locked")),
			ErrorEvent{consts.ErrCodeReadOnly, "the signing key is locked"}},
		{"wrapped keeps the code and the context", fmt.Errorf("opening the case: %w", AsError(consts.ErrCodeIO, errors.New("disk gone"))),
			ErrorEvent{consts.ErrCodeIO, "opening the case: disk gone"}},
		{"uncoded is internal", errors.New("something broke"),
			ErrorEvent{consts.ErrCodeInternal, "something broke"}},
		{"an empty message becomes the code", AsError(consts.ErrCodePersistence, errors.New("")),
			ErrorEvent{consts.ErrCodePersistence, consts.ErrCodePersistence}},
		{"an empty code is internal", ErrorEvent{Message: "no code"},
			ErrorEvent{consts.ErrCodeInternal, "no code"}},
		{"quotes and newlines survive", AsError(consts.ErrCodeQuery, errors.New("field \"user\"\nis used twice\t{x}")),
			ErrorEvent{consts.ErrCodeQuery, "field \"user\"\nis used twice\t{x}"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatted(t, tc.err); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Wails calls the formatter only with a failure, but a nil must still not cross as success.
func TestANilErrorStillCrossesAsAFailure(t *testing.T) {
	if got := formatted(t, nil); got.Code != consts.ErrCodeInternal || got.Message == "" {
		t.Errorf("got %+v", got)
	}
}
