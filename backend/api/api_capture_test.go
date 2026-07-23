package api

import (
	"testing"

	"rohy/backend/capture"
	"rohy/backend/consts"
	"rohy/backend/graphene"
)

func TestCaptureStatusIdleAndPositions(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	positions, err := capture.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewEventsAPI(store, positions, mustFindings(t))

	status := api.CaptureStatus()
	if status.Active {
		t.Errorf("a fresh binding should report no active capture")
	}
	if status.Positions == nil {
		t.Errorf("positions map should always be present, even when empty")
	}

	// A position recorded by a previous session is what a resumed capture continues from.
	if err := positions.SetPosition(consts.ChannelSecurity, 900); err != nil {
		t.Fatal(err)
	}
	if got := api.CaptureStatus().Positions[consts.ChannelSecurity]; got != 900 {
		t.Errorf("reported position = %d, want 900", got)
	}
}

func TestResetCapturePositions(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	positions, err := capture.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := NewEventsAPI(store, positions, mustFindings(t))
	_ = positions.SetPosition(consts.ChannelSecurity, 10)
	_ = positions.SetPosition(consts.ChannelSystem, 20)

	if err := api.ResetCapturePositions(consts.ChannelSecurity); err != nil {
		t.Fatalf("reset one: %v", err)
	}
	got := api.CaptureStatus().Positions
	if got[consts.ChannelSecurity] != 0 || got[consts.ChannelSystem] != 20 {
		t.Errorf("per-channel reset hit the wrong channel: %+v", got)
	}

	if err := api.ResetCapturePositions(""); err != nil {
		t.Fatalf("reset all: %v", err)
	}
	if len(api.CaptureStatus().Positions) != 0 {
		t.Errorf("reset-all left %+v", api.CaptureStatus().Positions)
	}
}

func TestCaptureStatusToleratesNoPositionStore(t *testing.T) {
	store := graphene.OpenInMemory()
	defer store.Close()
	api := NewEventsAPI(store, nil, mustFindings(t))

	status := api.CaptureStatus()
	if status.Active || len(status.Positions) != 0 {
		t.Errorf("status without a bookmark store = %+v", status)
	}
	if err := api.ResetCapturePositions(""); err != nil {
		t.Errorf("reset without a bookmark store should be a no-op, got %v", err)
	}
}
