package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"rohy/backend/consts"
	"rohy/backend/eventproof"

	"github.com/aoiflux/graphene/merkle"
)

// --- verify-proof ---
//
// The recipient's side of an event proof: a file from rohy's event drawer and a
// fingerprint the recipient kept, and nothing else — no case, no application, no store.
//
// 🔒 The root comes from --root and from nowhere else. Not from the file, which names the snapshot
// it was built against only so an analyst can find the right fingerprint; and not from a default
// or the environment. A proof checked against a root its author supplied proves nothing.

func cmdVerifyProof(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verify-proof", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var rootHex string
	fs.StringVar(&rootHex, "root", "", "the case fingerprint you kept, as 64 hex characters (required)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if rootHex == "" {
		fmt.Fprintln(stderr, "verify-proof: --root is required. A proof checked against the root "+
			"inside it proves nothing: supply the case fingerprint you kept outside the case.")
		return exitUsage
	}
	root, err := parseRoot(rootHex)
	if err != nil {
		fmt.Fprintf(stderr, "verify-proof: --root: %v\n", err)
		return exitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "verify-proof: give exactly one proof file, after the flags: "+
			"rohyctl verify-proof --root <hex> <file>")
		return exitUsage
	}
	path := fs.Arg(0)
	data, err := readBounded(path, consts.EventProofMaxBytes)
	if err != nil {
		fmt.Fprintf(stderr, "verify-proof: %v\n", err)
		return exitUsage
	}

	fmt.Fprintf(stdout, "proof         %s\n", printable(path))
	fmt.Fprintf(stdout, "checked with  the root you supplied, %x\n\n", root)

	rep, err := eventproof.Verify(root, data)
	if err != nil {
		what := "does not verify"
		if errors.Is(err, eventproof.ErrMalformed) {
			what = "is not a readable event proof"
		}
		fmt.Fprintf(stdout, "FAILED: this file %s.\n  %s\n", what, printable(err.Error()))
		return exitProblem
	}

	s := rep.Subject
	fmt.Fprintf(stdout, "event         %s on %s at %s (node %d)\n",
		printable(s.EventID), printable(s.Computer), printable(s.Timestamp), s.Node)
	fmt.Fprintf(stdout, "recorded by   %s, %s channel\n", printable(s.Provider), printable(s.Channel))
	if s.SourceIdentifier != "" {
		fmt.Fprintf(stdout, "read from     %s\n", printable(s.SourceIdentifier))
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  ok  the inclusion proof resolves to the root you supplied")
	fmt.Fprintln(stdout, "  ok  the stored record in this file is the one the proof commits to")
	if rep.RawCarried {
		fmt.Fprintln(stdout, "  ok  the raw record in this file matches the hash in the stored record")
	} else {
		fmt.Fprintln(stdout, "  --  this file carries no raw record")
	}
	fmt.Fprintln(stdout, "\nVERIFIED")
	fmt.Fprintln(stdout, "\nThis establishes:")
	for _, line := range rep.Establishes {
		fmt.Fprintf(stdout, "  - %s\n", line)
	}
	fmt.Fprintln(stdout, "\nIt does not establish:")
	for _, line := range rep.DoesNotEstablish {
		fmt.Fprintf(stdout, "  - %s\n", line)
	}
	return exitOK
}

// printable escapes what a terminal would act on rather than show.
//
// 🔒 Everything the subject lines print came from the logs, and log content is attacker-influenced:
// a computer name is whatever the machine that wrote the record said it was. C0 and C1 controls
// (escape sequences, carriage returns, bells) and Unicode format characters (bidi overrides,
// zero-width marks) are shown as escapes, so the recipient sees what was there instead of having
// it rewrite the screen — or reorder a line until FAILED reads as VERIFIED.
func printable(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
			b.WriteString(`�`)
		case r < 0x100 && unicode.IsControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseRoot reads a snapshot root as hex, tolerating the surrounding whitespace a copy and paste
// brings with it, and nothing else.
func parseRoot(s string) (merkle.Hash, error) {
	var h merkle.Hash
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != merkle.Size {
		return h, fmt.Errorf("want %d hex characters, the case fingerprint exactly as exported", 2*merkle.Size)
	}
	copy(h[:], raw)
	return h, nil
}

// readBounded reads at most limit+1 bytes, so an oversized file is refused by the verifier as too
// large rather than read whole into memory first.
func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(limit)+1))
}
