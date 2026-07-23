//go:build race

package evtx

// raceEnabled is true when the test binary is built with -race. The race detector's
// shadow-memory overhead makes absolute heap-bound assertions meaningless, so the
// bounded-memory scale test skips in that mode.
const raceEnabled = true
