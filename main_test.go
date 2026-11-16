package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 🔒 The app is the first service, so Wails stops it last: the store closes only after
// EventsAPI.ServiceShutdown has drained a running ingest.
func TestTheAppStopsAfterIngestionDrains(t *testing.T) {
	a := &App{}
	list := services(a)
	if len(list) == 0 || list[0].Instance() != any(a) {
		t.Fatalf("the app is not the first service: Wails would close the store before ingestion drains")
	}
	seen := map[any]bool{}
	for _, s := range list[1:] {
		if s.Instance() == any(a) || seen[s.Instance()] {
			t.Fatalf("service %T is registered twice", s.Instance())
		}
		seen[s.Instance()] = true
	}
}

// Wails binds every exported method of a service but its lifecycle. The app's are its lifecycle
// alone, so the page can call nothing on it.
func TestTheAppBindsNothing(t *testing.T) {
	rt := reflect.TypeFor[*App]()
	for m := range rt.Methods() {
		if m.Name != "ServiceStartup" && m.Name != "ServiceShutdown" {
			t.Errorf("App.%s would be callable from the page", m.Name)
		}
	}
}

// A cancelled Windows dialog is no selection, not a failure.
func TestACancelledDialogIsNoSelection(t *testing.T) {
	got, err := picked([]string{"x"}, errorString(errDialogCancelled))
	if err != nil || got != nil {
		t.Fatalf("cancel: got %v, %v", got, err)
	}
	if _, err := picked("", errorString("disk gone")); err == nil {
		t.Fatal("a real failure was swallowed")
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// wailsSource reads a file of the Wails module this build uses.
func wailsSource(t *testing.T, rel string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/wailsapp/wails/v3").Output()
	if err != nil {
		t.Fatalf("locating the Wails module: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Tripwires: what rohy relies on in Wails but cannot import. An upgrade that changes one fails
// here instead of leaking a passphrase or reporting a cancel as an error.
func TestWhatRohyAssumesOfWailsStillHolds(t *testing.T) {
	// SafeLogHandler withholds the attributes Wails logs a call's data under.
	call := wailsSource(t, "pkg/application/messageprocessor_call.go")
	if !strings.Contains(call, `"args", string(jsonArgs), "result", string(jsonResult)`) {
		t.Error(`Wails no longer logs a binding call's data as "args" and "result": update api.withheldKeys`)
	}
	proc := wailsSource(t, "pkg/application/messageprocessor.go")
	if !strings.Contains(proc, `"args", req.Args.String()`) {
		t.Error(`Wails no longer logs a runtime call's arguments as "args": update api.withheldKeys`)
	}
	// desktop.picked recognises a cancelled dialog by its text.
	cfd := wailsSource(t, "internal/go-common-file-dialog/cfd/errors.go")
	if !strings.Contains(cfd, `ErrorCancelled = errors.New("`+errDialogCancelled+`")`) {
		t.Error("Wails' cancelled-dialog error changed: update errDialogCancelled")
	}
}
