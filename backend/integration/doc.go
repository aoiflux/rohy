// Package integration runs rohy's real pipeline end to end, on the disk backend, over the committed
// Security.evtx fixture: ingest, query, every built-in rule, rule export and import, rebuilds,
// re-ingests, backfill, recovery from an interrupted build, and a build-side stress test.
//
// Every test is behind the integration build tag, so a plain `go test ./...` skips them:
//
//	go test -tags=integration ./backend/integration/ -count=1
//	go test -tags=integration ./backend/integration/ -run TestTheBuildStaysBounded -count=1 -v
//	go test -tags=integration ./backend/integration/ -run TestTheRealPipeline -update
//
// -update rewrites testdata/security-golden.json from the run; review the diff before keeping it.
//
// This file carries no tag, so the package exists in every build and its tests are only switched
// off, never missing.
package integration
