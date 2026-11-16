package api

import (
	"os"
	"strconv"
	"strings"

	"rohy/backend/consts"
)

// HandoffPID is the process id of the rohy that relaunched this one elevated, read from args (the
// command line without the program), or 0 when there is none. RelaunchAsAdmin passes it, and the
// new rohy waits for that process to exit before it opens the case (AwaitExit).
//
// A value that is not a positive process id, or is this process, is not a handoff: waiting on it
// could only delay the start.
func HandoffPID(args []string) int {
	for _, arg := range args {
		v, ok := strings.CutPrefix(arg, consts.HandoffFlag)
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(v)
		if err != nil || pid <= 0 || pid == os.Getpid() {
			return 0
		}
		return pid
	}
	return 0
}
