package api

import (
	"os"
	"strconv"
	"testing"

	"rohy/backend/consts"
)

// The elevated rohy finds the old one's process id among its arguments, and nothing else is
// taken for one: a value that names no process this rohy could wait on starts it at once.
func TestHandoffPIDReadsOnlyAProcessToWaitOn(t *testing.T) {
	self := strconv.Itoa(os.Getpid())
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, 0},
		{"the flag", []string{consts.HandoffFlag + "4242"}, 4242},
		{"among others", []string{"-x", consts.HandoffFlag + "7", "y"}, 7},
		{"no value", []string{consts.HandoffFlag}, 0},
		{"not a number", []string{consts.HandoffFlag + "abc"}, 0},
		{"zero", []string{consts.HandoffFlag + "0"}, 0},
		{"negative", []string{consts.HandoffFlag + "-5"}, 0},
		{"this process", []string{consts.HandoffFlag + self}, 0},
		{"a different flag", []string{"--after-pidx=9"}, 0},
	}
	for _, c := range cases {
		if got := HandoffPID(c.args); got != c.want {
			t.Errorf("%s: HandoffPID(%q) = %d, want %d", c.name, c.args, got, c.want)
		}
	}
}
