package api

import (
	"os"
	"reflect"
	"regexp"
	"testing"
)

// boundTypes are the structs main registers as Wails services. Wails binds every exported method
// of each, so every such method is callable from the page.
var boundTypes = []any{
	&EventsAPI{}, &GraphAPI{}, &RulesAPI{}, &BuildAPI{}, &FindingsAPI{},
	&SystemAPI{}, &MaintenanceAPI{}, &SnapshotAPI{}, &AnnotateAPI{}, &QueryAPI{},
}

// 🔒 No bound method takes what only Go can supply: an interface, a function, a channel, or the
// session. Such a method is the app's wiring, and bound it would let the page rewire the app; it
// belongs in a package function (Start, AttachSession) or an unexported method instead.
func TestNoBoundMethodTakesTheAppsWiring(t *testing.T) {
	session := reflect.TypeFor[*Session]()
	for _, v := range boundTypes {
		rt := reflect.TypeOf(v)
		for m := range rt.Methods() {
			for in := range m.Type.Ins() { // the receiver too, which is never any of these
				switch {
				case in == session, in.Kind() == reflect.Interface, in.Kind() == reflect.Func, in.Kind() == reflect.Chan:
					t.Errorf("%s.%s takes %s: the page could call it", rt.Elem().Name(), m.Name, in)
				}
			}
		}
	}
}

// The lifecycle Wails calls is the one it never binds: ServiceStartup and ServiceShutdown.
// A Startup or Shutdown of rohy's own would be bound and callable from the page.
func TestNoBoundMethodIsALifecycleHook(t *testing.T) {
	for _, v := range boundTypes {
		rt := reflect.TypeOf(v)
		for _, name := range []string{"Startup", "Shutdown", "Stage", "Ready", "Failed"} {
			if _, ok := rt.MethodByName(name); ok {
				t.Errorf("%s.%s is bound: the page could call it", rt.Elem().Name(), name)
			}
		}
	}
}

// The frontend calls a bound method by its name (frontend/src/lib/api/index.js), so a renamed or
// removed method would fail only when the page called it. Every name it calls must be one Wails
// binds, with as many parameters as the call passes.
func TestEveryMethodTheFrontendCallsIsBound(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/lib/api/index.js")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`const PKG = 'rohy/backend/api';`).Match(src) {
		t.Fatal("index.js no longer names this package as the one it calls")
	}
	byName := map[string]reflect.Type{}
	for _, v := range boundTypes {
		rt := reflect.TypeOf(v)
		byName[rt.Elem().Name()] = rt
	}
	structs := map[string]reflect.Type{}
	for _, m := range regexp.MustCompile(`(?m)^const (\w+) = '(\w+API)';`).FindAllSubmatch(src, -1) {
		rt, ok := byName[string(m[2])]
		if !ok {
			t.Errorf("index.js calls %s, which main does not bind", m[2])
			continue
		}
		structs[string(m[1])] = rt
	}
	calls := regexp.MustCompile(`call\((\w+), '(\w+)'([^)]*)\)`).FindAllSubmatch(src, -1)
	if len(calls) < 100 {
		t.Fatalf("found only %d calls in index.js: the pattern no longer matches it", len(calls))
	}
	for _, c := range calls {
		rt, ok := structs[string(c[1])]
		if !ok {
			t.Errorf("call(%s, '%s'): %s names no bound struct", c[1], c[2], c[1])
			continue
		}
		m, ok := rt.MethodByName(string(c[2]))
		if !ok {
			t.Errorf("index.js calls %s.%s, which does not exist", rt.Elem().Name(), c[2])
			continue
		}
		args := topLevelCommas(c[3])
		if want := m.Type.NumIn() - 1; args != want && !m.Type.IsVariadic() {
			t.Errorf("index.js calls %s.%s with %d arguments, it takes %d", rt.Elem().Name(), c[2], args, want)
		}
	}
}

// topLevelCommas counts the commas outside any bracket: the arguments after a call's method name.
func topLevelCommas(s []byte) int {
	depth, n := 0, 0
	for _, c := range s {
		switch c {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				n++
			}
		}
	}
	return n
}
