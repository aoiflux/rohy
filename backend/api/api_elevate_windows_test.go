//go:build windows

package api

import (
	"flag"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestAwaitExitHelperProcess is the process the AwaitExit tests wait on: run with the argument
// "linger" it stays alive for a while, as the unelevated rohy does while it closes the case.
func TestAwaitExitHelperProcess(t *testing.T) {
	if flag.Arg(0) != "linger" {
		t.Skip("run by TestAwaitExit* only")
	}
	time.Sleep(10 * time.Second)
}

func startLingering(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAwaitExitHelperProcess$", "--", "linger")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// The elevated rohy opens the case only once the old one has exited, and no sooner.
func TestAwaitExitReturnsWhenTheProcessExits(t *testing.T) {
	cmd := startLingering(t)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = cmd.Process.Kill()
	}()
	start := time.Now()
	if !AwaitExit(cmd.Process.Pid, 5*time.Second) {
		t.Fatal("AwaitExit reported the process still running after it was killed")
	}
	if took := time.Since(start); took < 150*time.Millisecond || took > 4*time.Second {
		t.Fatalf("AwaitExit returned after %v, want it to return when the process exits (~200ms)", took)
	}
}

// An old rohy that never exits delays the elevated one by the wait and no more; the case's lock
// then refuses the open with a reason.
func TestAwaitExitGivesUpAtTheDeadline(t *testing.T) {
	cmd := startLingering(t)
	start := time.Now()
	if AwaitExit(cmd.Process.Pid, 300*time.Millisecond) {
		t.Fatal("AwaitExit reported a running process as exited")
	}
	if took := time.Since(start); took < 250*time.Millisecond || took > 3*time.Second {
		t.Fatalf("AwaitExit gave up after %v, want ~300ms", took)
	}
}

// An old rohy that is already gone holds nothing to wait for.
func TestAwaitExitOfAGoneProcessReturnsAtOnce(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if !AwaitExit(cmd.Process.Pid, 5*time.Second) {
		t.Fatal("AwaitExit reported an exited process as running")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("AwaitExit of a gone process took %v", took)
	}
}
