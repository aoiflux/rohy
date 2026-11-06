//go:build integration && race

package integration

// raceEnabled is true when the test binary is built with -race, whose shadow memory makes heap
// measurements meaningless.
const raceEnabled = true
