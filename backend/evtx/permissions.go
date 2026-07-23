// Package evtx owns EVTX/Windows event-log ingestion for rohy: privilege
// checks, streaming file parsing, live event-log access, and normalization into
// the persistence layer's Event model. This file holds the platform-agnostic
// permission logic; platform-specific detection lives in permissions_windows.go
// and permissions_other.go.
package evtx

import (
	"fmt"
	"strings"

	"rohy/backend/consts"
)

// PermissionStatus reports the current process privilege state. It is JSON-tagged
// because it is surfaced to the frontend permissions store via the API layer.
type PermissionStatus struct {
	Platform      string `json:"platform"`
	Elevated      bool   `json:"elevated"`
	Administrator bool   `json:"administrator"`
}

// AccessDecision is the result of evaluating whether a set of requested channels
// can be read under a given permission status. When Needed is true the caller
// should surface Message via the Material snackbar and fall back to file ingest.
type AccessDecision struct {
	Needed          bool     `json:"needed"`
	BlockedChannels []string `json:"blocked_channels"`
	Message         string   `json:"message"`
}

// RequiresElevation reports whether reading the named channel needs administrator
// rights. Matching is case-insensitive.
func RequiresElevation(channel string) bool {
	for _, c := range consts.ElevatedChannels {
		if strings.EqualFold(c, channel) {
			return true
		}
	}
	return false
}

// EvaluateAccess determines whether any requested channel is blocked under status
// and, if so, builds the warning message. Channels that do not require elevation,
// or all channels when the process is elevated, produce a zero-value decision
// (Needed == false).
func EvaluateAccess(channels []string, status PermissionStatus) AccessDecision {
	var blocked []string
	for _, ch := range channels {
		if RequiresElevation(ch) && !status.Elevated {
			blocked = append(blocked, ch)
		}
	}
	if len(blocked) == 0 {
		return AccessDecision{}
	}
	return AccessDecision{
		Needed:          true,
		BlockedChannels: blocked,
		Message:         fmt.Sprintf(consts.MsgElevationRequired, strings.Join(blocked, ", ")),
	}
}
