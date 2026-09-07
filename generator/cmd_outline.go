package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"sightkick/generator/internal/gen"
)

// jsonFailure is the --json failure envelope, matching sightmap's `sightmap
// stats --json` contract (go/cmd/sightmap/cmd_stats.go): --json consumers
// parse stdout unconditionally, so a bare human error on stderr would leave
// them parsing nothing. Every --json run prints exactly one JSON object on
// stdout; a present "error" key (never present on success) means the run
// failed. Diagnostics ride along so a --json caller sees why, the same
// information Format(diags) would have put on stderr for a human.
type jsonFailure struct {
	Error       string           `json:"error"`
	Diagnostics []jsonDiagnostic `json:"diagnostics,omitempty"`
}

type jsonDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Where    string `json:"where,omitempty"`
}

func toJSONDiagnostics(diags []gen.Diagnostic) []jsonDiagnostic {
	out := make([]jsonDiagnostic, len(diags))
	for i, d := range diags {
		out[i] = jsonDiagnostic{Severity: d.Severity, Code: d.Code, Message: d.Message, Where: d.Where}
	}
	return out
}

// writeJSON marshals v with sightmap's stats convention: indented, trailing
// newline, nothing else on the stream.
func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// errPrinted signals "already reported, just exit non-zero" — main.go prints
// every command's returned error as "✗ "+err.Error() to stderr, which would
// otherwise duplicate (in a worse, non-JSON form) the failure already written
// to stdout as the jsonFailure envelope.
var errPrinted = errors.New("see the JSON error above")

// parseTargetArgs applies the argument shape both outline and explain share
// (and that `call` and `browser` already use): `<app-dir>` is positional and
// must come first, everything after it is flags. A flag.ErrHelp return means
// usage was printed and the caller should exit 0.
func parseTargetArgs(fset *flag.FlagSet, args []string) (string, error) {
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return "", flag.ErrHelp
	}
	if len(args) < 1 {
		fset.Usage()
		return "", errors.New("missing <app-dir>")
	}
	target := args[0]
	if strings.HasPrefix(target, "-") {
		fset.Usage()
		return "", fmt.Errorf("first argument must be the app dir, got flag %q", target)
	}
	if err := fset.Parse(args[1:]); err != nil {
		return "", err
	}
	return target, nil
}

// buildOutline runs gen.BuildOutline and applies the two failure conventions
// shared by outline and explain: on a hard load error or a compile error, a
// --json caller gets the jsonFailure envelope on out (exit 1 either way); a
// human caller gets diagnostics on stderr via gen.Format, matching `build`.
// ok is false when the caller should return (without its own additional error
// text — one of the two failure paths already reported it).
func buildOutline(out io.Writer, target string, asJSON bool) (o gen.Outline, ok bool, err error) {
	fail := func(msg string, diags []gen.Diagnostic) (gen.Outline, bool, error) {
		if asJSON {
			writeJSON(out, jsonFailure{Error: msg, Diagnostics: toJSONDiagnostics(diags)})
			return gen.Outline{}, false, errPrinted
		}
		return gen.Outline{}, false, errors.New(msg)
	}

	o, diags, lerr := gen.BuildOutline(target)
	if lerr != nil {
		return fail(lerr.Error(), diags)
	}
	if len(diags) > 0 && !asJSON {
		fmt.Fprintln(os.Stderr, gen.Format(diags))
	}
	if gen.HasErrors(diags) {
		return fail(fmt.Sprintf("%d error(s) compiling the tool layer", gen.CountErrors(diags)), diags)
	}
	return o, true, nil
}

// runOutline is the orientation pass: journeys, views, and every tool's
// one-line summary, grouped by ensure_view. This is `docs/scenario-testing.md`
// §6's "read every tool's description/params/ensure_view/returns" made cheap
// to start from — the full detail for a subset comes from `explain`.
func runOutline(args []string) error {
	fset := flag.NewFlagSet("outline", flag.ContinueOnError)
	asJSON := fset.Bool("json", false, "Print the orientation pass as one JSON object instead of text.")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick outline <app-dir> [--json]")
		fmt.Fprintln(os.Stderr, "\njourneys + every tool's one-line summary, grouped by view. The cheap first")
		fmt.Fprintln(os.Stderr, "read for a plan-time agent — see `sightkick explain` for a filtered subset's")
		fmt.Fprintln(os.Stderr, "full detail.\n\nFlags:")
		fset.PrintDefaults()
	}
	target, err := parseTargetArgs(fset, args)
	if err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}

	o, ok, err := buildOutline(os.Stdout, target, *asJSON)
	if !ok {
		return err
	}
	brief := o.Brief()
	if *asJSON {
		return writeJSON(os.Stdout, brief)
	}
	renderOutline(os.Stdout, brief, time.Now())
	return nil
}
