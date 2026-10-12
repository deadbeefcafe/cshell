package cshell

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type Vectors struct {
	valid bool
	argc  int
	line  string
}

var testvecs = []Vectors{
	{true, 0, `      `},
	{true, 1, `   "  "`},
	{true, 4, `this is a test`},
	{true, 1, `this\ is\ a\ test`},
	{true, 3, `this ' is a ' test`},
	{true, 1, `"this is a test"`},
	{true, 2, `cmd ""`},
	{true, 4, `\"this is a test\"`},
	{true, 1, `"this 'is a' test"`},
	{false, 1, `   "  '`},
	{false, 1, `"this 'is a' test`},
}

func TestParseLine(t *testing.T) {
	for _, v := range testvecs {
		args, err := ParseLine(v.line)
		if len(args) != v.argc {
			t.Fatalf("failed to parse %q : arg length mismatch", v.line)
		}
		if err != nil && v.valid {
			t.Fatalf("parse error: %v on valid line: %q", err, v.line)
		}
		if err == nil && !v.valid {
			t.Fatalf("no error on invalid line: %q", v.line)
		}

		//fmt.Printf("Parse(%s)\n", v.line)
		//fmt.Printf("len = %d args = %#v\n", len(args), args)
		//fmt.Printf("\n")
	}
}

func TestExecuteLineIgnoresEmptyInput(t *testing.T) {
	cli := New()
	cli.SetIO(strings.NewReader(""), ioutil.Discard)

	for _, line := range []string{"", " \t ", "@", "@ \t"} {
		if err := cli.ExecuteLine(line); err != nil {
			t.Fatalf("ExecuteLine(%q): %v", line, err)
		}
	}

	if history := cli.GetHistory(); len(history) != 0 {
		t.Fatalf("empty input was added to history: %#v", history)
	}
}

func TestExecuteLineMissingHistoryReference(t *testing.T) {
	for _, line := range []string{"!!", "!1", "!0"} {
		cli := New()
		var output strings.Builder
		cli.SetIO(strings.NewReader(""), &output)

		if err := cli.ExecuteLine(line); err != nil {
			t.Fatalf("ExecuteLine(%q): %v", line, err)
		}
		if got, want := output.String(), line+": not found\r\n"; got != want {
			t.Errorf("ExecuteLine(%q) output = %q, want %q", line, got, want)
		}
		if history := cli.GetHistory(); len(history) != 0 {
			t.Errorf("missing reference %q was added to history: %#v", line, history)
		}
	}
}

func TestDelHistoryRejectsNegativeCount(t *testing.T) {
	cli := New()
	cli.AddHistory("first")
	cli.DelHistory(-1)

	if got, want := cli.GetHistory(), []string{"first"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestLoadHistoryPreservesHistoryOnReadError(t *testing.T) {
	file, err := ioutil.TempFile("", "cshell-history")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(strings.Repeat("x", bufio.MaxScanTokenSize+1)); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cli := New()
	cli.AddHistory("existing")
	if err := cli.LoadHistory(file.Name()); err == nil {
		t.Fatal("LoadHistory succeeded despite scanner token overflow")
	}
	if got := cli.GetHistory(); len(got) != 1 || got[0] != "existing" {
		t.Fatalf("history changed after failed load: %#v", got)
	}
}

func TestInputAtCapacityDoesNotPanic(t *testing.T) {
	cli := New()
	cli.SetIO(strings.NewReader(""), ioutil.Discard)
	for i := 0; i < len(cli.buffer); i++ {
		cli.input('a')
	}
	cli.input(ASCII_ESC)
	cli.input('[')
	cli.input(VT100_LEFT)
	cli.input('b')

	if cli.len != len(cli.buffer) {
		t.Fatalf("line length = %d, want %d", cli.len, len(cli.buffer))
	}
}

func TestInputInsertsNearCapacity(t *testing.T) {
	cli := New()
	cli.SetIO(strings.NewReader(""), ioutil.Discard)
	for i := 0; i < len(cli.buffer)-1; i++ {
		cli.input('a')
	}
	cli.input(ASCII_ESC)
	cli.input('[')
	cli.input(VT100_LEFT)
	cli.input('b')

	if cli.len != len(cli.buffer) {
		t.Fatalf("line length = %d, want %d", cli.len, len(cli.buffer))
	}
	if got := string(cli.buffer[cli.len-2 : cli.len]); got != "ba" {
		t.Fatalf("inserted suffix = %q, want %q", got, "ba")
	}
}

func TestHistoryRecallTruncatesOversizedLine(t *testing.T) {
	cli := New()
	cli.SetIO(strings.NewReader(""), ioutil.Discard)
	cli.AddHistory(strings.Repeat("a", len(cli.buffer)+1))
	cli.input(ASCII_ESC)
	cli.input('[')
	cli.input(VT100_UP)

	if cli.len != len(cli.buffer) {
		t.Fatalf("recalled line length = %d, want %d", cli.len, len(cli.buffer))
	}
}

func TestCtrlCClearsInputWhenEchoDisabled(t *testing.T) {
	cli := New()
	cli.SetIO(strings.NewReader(""), ioutil.Discard)
	cli.SetEcho(false)
	cli.input('a')
	cli.input(ASCII_CTRLC)

	if cli.len != 0 || cli.pos != 0 {
		t.Fatalf("Ctrl-C left input state len=%d pos=%d", cli.len, cli.pos)
	}
}

type dataAndErrorReader struct {
	done bool
}

func (r *dataAndErrorReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return copy(p, "run\r"), io.EOF
}

func TestRunProcessesDataReturnedWithEOF(t *testing.T) {
	cli := New()
	var output bytes.Buffer
	called := false
	cli.Command("run", "", func(args []string) error {
		called = true
		return nil
	})
	cli.SetIO(&dataAndErrorReader{}, &output)
	cli.Run()

	if !called {
		t.Fatal("command returned with EOF was not executed")
	}
	if strings.Contains(output.String(), "read input error") {
		t.Fatalf("EOF was reported as an input error: %q", output.String())
	}
}

func TestRunReportsNonEOFReadErrorToShellOutput(t *testing.T) {
	cli := New()
	var output bytes.Buffer
	cli.SetIO(errorReader{}, &output)
	cli.Run()

	if !strings.Contains(output.String(), "read input error: broken reader") {
		t.Fatalf("read error missing from shell output: %q", output.String())
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("broken reader")
}

func TestCli(t *testing.T) {

	// XXX make this actually test something
	commands := "\r\rtest1 \"foo bar baz\"\r\rtest2\r!!\r\rpass\r\rhistory\rfoo\r!!\r!3\r"

	cli := New()
	cli.Command("test1", "a test command [args]", func(args []string) error {
		cli.Printf("got test 1  argc=%d args=%#v\n", len(args), args)
		return nil
	})
	cli.Command("test2", "a test command [args]", func(args []string) error {
		cli.Printf("got test 2\n")
		return nil
	})
	cli.Command("pass", "pass the tests", func(args []string) error {
		cli.Printf("got pass\n")
		return nil
	})
	cli.Command("fail", "pass the tests", func(args []string) error {
		cli.Printf("got fail\n")
		t.Fatalf("test failed")
		return nil
	})

	cli.SetIO(strings.NewReader(commands), os.Stdout)
	cli.Run()

}

func TestExecuteLineHistoryFilesDisabledByDefault(t *testing.T) {
	dir, err := ioutil.TempDir("", "cshell")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	secret := filepath.Join(dir, "secret.txt")
	if err := ioutil.WriteFile(secret, []byte("TOPSECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(dir, "victim.txt")
	if err := ioutil.WriteFile(victim, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cli := New()
	var output strings.Builder
	cli.AddHistory("some command")
	cli.SetIO(strings.NewReader(""), &output)

	cli.ExecuteLine("history save " + victim)
	data, err := ioutil.ReadFile(victim)
	if err != nil {
		t.Fatalf("victim file: %v", err)
	}
	if string(data) != "original\n" {
		t.Fatalf("history save overwrote %q while disabled: %q", victim, data)
	}

	cli.ExecuteLine("history load " + secret)
	for _, line := range cli.GetHistory() {
		if strings.Contains(line, "TOPSECRET") {
			t.Fatalf("history load read %q while disabled: %#v", secret, cli.GetHistory())
		}
	}
	if !strings.Contains(output.String(), "disabled") {
		t.Fatalf("disabled history file access reported without a hint: %q", output.String())
	}
}

func TestExecuteLineHistoryFilesOptIn(t *testing.T) {
	dir, err := ioutil.TempDir("", "cshell")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	cli := New()
	var output strings.Builder
	cli.SetIO(strings.NewReader(""), &output)
	cli.AllowHistoryFiles = true

	cli.AddHistory("saved line")
	if err := cli.ExecuteLine("history save " + filepath.Join(dir, "hist.txt")); err != nil {
		t.Fatal(err)
	}
	data, err := ioutil.ReadFile(filepath.Join(dir, "hist.txt"))
	if err != nil {
		t.Fatalf("history file was not written: %v", err)
	}
	if !strings.Contains(string(data), "saved line") {
		t.Fatalf("history file = %q", data)
	}

	// A load must replace the history in one step (see LoadHistory).
	cli.ClearHistory()
	cli.AddHistory("placeholder")
	if err := cli.ExecuteLine("history load " + filepath.Join(dir, "hist.txt")); err != nil {
		t.Fatal(err)
	}
	if h := cli.GetHistory(); len(h) == 0 || h[0] != "saved line" {
		t.Fatalf("history load gave %#v", h)
	}
}

func TestExecuteLineHistoryFileDirConfinement(t *testing.T) {
	base, err := ioutil.TempDir("", "cshell-base")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	outside, err := ioutil.TempDir("", "cshell-outside")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)
	secret := filepath.Join(outside, "secret.txt")
	if err := ioutil.WriteFile(secret, []byte("TOPSECRET\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the sandbox that points outside of it.
	if err := os.Symlink(secret, filepath.Join(base, "escape")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	for _, tc := range []struct {
		name string
		arg  string
		want bool // true when the file name should be accepted
	}{
		{"relative inside", "hist.txt", true},
		{"absolute inside", "", true},
		{"relative escape", "../outside.txt", false},
		{"deep relative escape", "a/../../outside.txt", false},
		{"absolute outside", secret, false},
		{"symlink inside", "escape", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arg := tc.arg
			if arg == "" {
				arg = filepath.Join(base, "hist.txt")
			}
			cli := New()
			var output strings.Builder
			cli.AllowHistoryFiles = true
			cli.HistoryFileDir = base
			cli.SetIO(strings.NewReader(""), &output)
			cli.AddHistory("line")

			cli.ExecuteLine("history save " + arg)
			got := !strings.Contains(output.String(), "outside of") &&
				!strings.Contains(output.String(), "history dir")
			if got != tc.want {
				t.Fatalf("save %q accepted = %v, want %v (output %q)", arg, got, tc.want, output.String())
			}
			if tc.want {
				// The file must have landed inside the sandbox, never in cwd.
				found := false
				filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
					if err == nil && info.Mode().IsRegular() && strings.Contains(path, "hist") {
						found = true
					}
					return nil
				})
				if !found {
					t.Fatalf("save %q wrote nothing inside %s", arg, base)
				}
			}
		})
	}

	// The cwd must not be polluted by a sandboxed relative save.
	if _, err := os.Stat("hist.txt"); err == nil {
		t.Fatalf("history save with HistoryFileDir set wrote to the current directory")
	}
}

func TestInputLineTerminators(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string
	}{
		{"carriage return", "foo\r", []string{"foo"}},
		{"line feed", "foo\n", []string{"foo"}},
		{"crlf pair", "foo\r\n", []string{"foo"}},
		{"crlf then lf", "foo\r\nfoo\n", []string{"foo", "foo"}},
		{"lf terminates mid-line", "foo\nfoo\r", []string{"foo", "foo"}},
		{"blank lines", "\n\r\nfoo\r", []string{"foo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli := New()
			var output strings.Builder
			var got [][]string
			cli.Command("foo", "", func(args []string) error {
				got = append(got, args)
				return nil
			})
			cli.SetIO(strings.NewReader(tc.input), &output)
			cli.Run()
			if len(got) != len(tc.want) {
				t.Fatalf("input %q ran %d commands, want %d (args %#v, output %q)", tc.input, len(got), len(tc.want), got, output.String())
			}
			for i, name := range tc.want {
				if !reflect.DeepEqual(got[i], []string{name}) {
					t.Fatalf("input %q command %d args %#v, want %#v", tc.input, i, got[i], []string{name})
				}
			}
		})
	}
}

func TestInputIgnoresUnreachableControlChars(t *testing.T) {
	cli := New()
	var output strings.Builder
	var got []string
	cli.Command("echo", "", func(args []string) error {
		got = args
		return nil
	})
	// ^A and BEL are not shell control characters and must not reach the
	// terminal, the command line, or the command args.
	cli.SetIO(strings.NewReader("echo a\x01b\x07c\r"), &output)
	cli.Run()

	want := []string{"echo", "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	if strings.ContainsAny(output.String(), "\x01\a") {
		t.Fatalf("control characters were echoed to the output: %q", output.String())
	}
}

func TestInputBackspaceDeletesCharacter(t *testing.T) {
	// Some terminals send ASCII_BS (ctrl-H) where others send ASCII_DEL.
	cli := New()
	var output strings.Builder
	var got []string
	cli.Command("echo", "", func(args []string) error {
		got = args
		return nil
	})
	cli.SetIO(strings.NewReader("echo ab\x08c\r"), &output)
	cli.Run()

	want := []string{"echo", "ac"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestInputCarriageReturnClearsPendingState(t *testing.T) {
	// A character consumed by the escape-sequence state machine must not leave a
	// pending CR state alive, otherwise the next LF would be swallowed.
	cli := New()
	var output strings.Builder
	runs := 0
	cli.Command("foo", "", func(args []string) error {
		runs++
		return nil
	})
	cli.Command("bar", "", func(args []string) error {
		runs++
		return nil
	})
	cli.SetIO(strings.NewReader("foo\r\x1bxbar\n"), &output)
	cli.Run()
	if runs != 2 {
		t.Fatalf("commands ran %d times, want 2 (output %q)", runs, output.String())
	}
}

func TestCommandNameResolution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		line  string
		ran   string
		print string // expected output fragment when nothing runs
	}{
		{"exact word", []string{"alpha", "beta"}, "alpha", "alpha", ""},
		{"abbreviated word", []string{"alphabet"}, "alph", "alphabet", ""},
		{"exact match beats an earlier abbreviation", []string{"alphabet", "alpha"}, "alpha", "alpha", ""},
		{"ambiguous abbreviation", []string{"ls", "lsof"}, "l", "", "multiple matches"},
		{"exact match beats a longer prefix match", []string{"ls", "lsof"}, "ls", "ls", ""},
		{"built-in name matched exactly", nil, "help", "", "Print commands"},
		{"built-in names are ambiguous", nil, "h", "", "multiple matches"},
		{"multiple words", []string{"dev info"}, "dev info", "dev info", ""},
		{"multiple words with arguments", []string{"dev info"}, "dev info all", "dev info", ""},
		{"longest name named in full wins", []string{"dev info", "dev info all the stuff"}, "dev info all the stuff", "dev info all the stuff", ""},
		{"abbreviated first word of a name", []string{"dev info"}, "de " + "info", "dev info", ""},
		{"partial first word is not a name match", []string{"dev info"}, "dev", "", "No such command"},
		{"matching stops at word boundaries", []string{"dev info"}, "developer info", "", "No such command"},
		{"quoted words", []string{"dev info"}, `dev "info"`, "dev info", ""},
		{"names are canonicalized", []string{" dev  info "}, "dev info", "dev info", ""},
		{"default command", []string{"foo", "*"}, "xyz arg", "*", ""},
		{"empty word matches nothing", []string{"a", "b"}, `""`, "", "No such command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli := New()
			var output strings.Builder
			ran := ""
			for _, n := range tc.names {
				name := n
				cli.Command(name, "c", func(args []string) error {
					ran = canonicalName(name)
					return nil
				})
			}
			cli.SetIO(strings.NewReader(""), &output)
			cli.ExecuteLine(tc.line)
			if ran != tc.ran {
				t.Fatalf("line %q ran %q, want %q (output %q)", tc.line, ran, tc.ran, output.String())
			}
			if tc.print != "" && !strings.Contains(output.String(), tc.print) {
				t.Fatalf("line %q output %q, want it to mention %q", tc.line, output.String(), tc.print)
			}
		})
	}
}

func TestSetFlagsAcceptsEquivalentNames(t *testing.T) {
	cli := New()
	cli.Command("dev info", "", func(args []string) error { return nil })
	if err := cli.SetFlags("dev  info", FlagHide); err != nil {
		t.Fatalf("SetFlags: %v", err)
	}
	if err := cli.SetFlags("nope", FlagHide); !errors.Is(err, ErrNoSuchCommand) {
		t.Fatalf("SetFlags(nope) = %v, want %v", err, ErrNoSuchCommand)
	}
}

func TestHistoryRecallSurvivesHistoryChanges(t *testing.T) {
	// The scrollback index is only valid while the history it was chosen from is
	// still there, so stepping through a history that changed must not panic.
	cli := New()
	var output strings.Builder
	cli.SetIO(strings.NewReader(""), &output)
	for _, line := range []string{"one", "two", "three"} {
		cli.AddHistory(line)
	}
	for i := 0; i < 3; i++ {
		cli.input(ASCII_ESC)
		cli.input('[')
		cli.input(VT100_UP)
	}
	cli.DelHistory(3)
	for i := 0; i < 3; i++ {
		cli.input(ASCII_ESC)
		cli.input('[')
		cli.input(VT100_DOWN)
		cli.input(ASCII_ESC)
		cli.input('[')
		cli.input(VT100_UP)
	}
	cli.input(ASCII_CR)
	if cli.scrollback != 0 {
		t.Fatalf("scrollback = %d after a line was run, want 0", cli.scrollback)
	}
	if cli.len != 0 || cli.pos != 0 {
		t.Fatalf("input line not cleared after run: len=%d pos=%d", cli.len, cli.pos)
	}
}

func TestMaxHistoryDropsOldestEntries(t *testing.T) {
	cli := New()
	cli.MaxHistory = 3
	for i := 1; i <= 5; i++ {
		cli.AddHistory(fmt.Sprintf("cmd%d", i))
	}
	want := []string{"cmd3", "cmd4", "cmd5"}
	if got := cli.GetHistory(); !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestMaxHistoryAppliesToLoadedHistory(t *testing.T) {
	file, err := ioutil.TempFile("", "cshell-history")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	for i := 1; i <= 5; i++ {
		if _, err := file.WriteString(fmt.Sprintf("cmd%d\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	cli := New()
	cli.MaxHistory = 2
	if err := cli.LoadHistory(file.Name()); err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd4", "cmd5"}
	if got := cli.GetHistory(); !reflect.DeepEqual(got, want) {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestNewLimitsHistoryByDefault(t *testing.T) {
	cli := New()
	var output strings.Builder
	cli.Command("cmd", "", func(args []string) error { return nil })
	cli.SetIO(strings.NewReader(""), &output)
	for i := 0; i < 2*defaultMaxHistory+5; i++ {
		cli.ExecuteLine("cmd " + strconv.Itoa(i))
	}
	if got := len(cli.GetHistory()); got > defaultMaxHistory {
		t.Fatalf("history holds %d entries, want at most %d", got, defaultMaxHistory)
	}
}

func TestMaxHistoryZeroKeepsEverything(t *testing.T) {
	cli := New()
	cli.MaxHistory = 0
	for i := 0; i < 3*defaultMaxHistory; i++ {
		cli.AddHistory("cmd" + strconv.Itoa(i))
	}
	if got, want := len(cli.GetHistory()), 3*defaultMaxHistory; got != want {
		t.Fatalf("history holds %d entries, want %d", got, want)
	}
}

func TestShellWithoutStreamsDoesNotPanic(t *testing.T) {
	// A shell that was never given streams must stay quiet rather than crash the
	// program that embedded it.
	cli := New()
	cli.ExecuteLine("help")
	cli.Printf("%d", 1)
	cli.Print("x")
	cli.Println("y")
	cli.HexDump([]byte{1})
	cli.Run()
	cli2 := New()
	cli2.SetIO(strings.NewReader(""), nil)
	cli2.ExecuteLine("help")
	cli2.Run()
}

func TestCommandWithoutHandlerDoesNotPanic(t *testing.T) {
	cli := New()
	var output strings.Builder
	cli.Command("bad", "no handler", nil)
	cli.SetIO(strings.NewReader(""), &output)
	if err := cli.ExecuteLine("bad"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "no handler") {
		t.Fatalf("missing handler was not reported: %q", output.String())
	}
}

type silentReader struct {
	remaining int
	rest      string
}

func (r *silentReader) Read(p []byte) (int, error) {
	if r.remaining > 0 {
		r.remaining--
		return 0, nil
	}
	if r.rest == "" {
		return 0, io.EOF
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

type alwaysSilentReader struct{}

func (alwaysSilentReader) Read([]byte) (int, error) { return 0, nil }

func TestRunGivesUpOnSilentInput(t *testing.T) {
	// A reader that returns nothing without an error used to leave Run spinning
	// at full speed.
	cli := New()
	var output bytes.Buffer
	cli.SetIO(alwaysSilentReader{}, &output)
	done := make(chan struct{})
	go func() {
		cli.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return to a reader that never returns data")
	}
	if !strings.Contains(output.String(), "no data") {
		t.Fatalf("silent input was not reported: %q", output.String())
	}
}

func TestRunToleratesASilentRead(t *testing.T) {
	cli := New()
	var output bytes.Buffer
	ran := false
	cli.Command("foo", "", func(args []string) error {
		ran = true
		return nil
	})
	cli.SetIO(&silentReader{remaining: 1, rest: "foo\r"}, &output)
	cli.Run()
	if !ran {
		t.Fatalf("command after a single silent read did not run (output %q)", output.String())
	}
}

func TestControlSequencesDoNotLeakIntoTheLine(t *testing.T) {
	for _, tc := range []string{
		"\x1b[3~foo\r",   // Delete
		"\x1b[15~foo\r",  // F5
		"\x1b[?25lfoo\r", // hide cursor
		"\x1b[1;3Hfoo\r", // Home with a modifier
	} {
		cli := New()
		var output bytes.Buffer
		var got []string
		cli.Command("foo", "", func(args []string) error {
			got = args
			return nil
		})
		cli.SetIO(strings.NewReader(tc), &output)
		cli.Run()
		if !reflect.DeepEqual(got, []string{"foo"}) {
			t.Fatalf("input %q ran %#v, want [foo] (output %q)", tc, got, output.String())
		}
	}
}

func TestTerminatorInsideControlSequence(t *testing.T) {
	// A half finished control sequence must not eat the line terminator, that
	// would leave the command line stuck.
	for _, tc := range []string{"foo\x1b[\r", "foo\x1b\n"} {
		cli := New()
		var output bytes.Buffer
		ran := false
		cli.Command("foo", "", func(args []string) error {
			ran = true
			return nil
		})
		cli.SetIO(strings.NewReader(tc), &output)
		cli.Run()
		if !ran {
			t.Fatalf("input %q did not run the command (output %q)", tc, output.String())
		}
	}
}

func TestUnboundEscapeSequenceConsumesBothBytes(t *testing.T) {
	// ESC followed by something this shell does not bind is a meta sequence, so
	// neither byte reaches the command line, while a plain escape still does not
	// swallow the whole rest of the line.
	cli := New()
	var output bytes.Buffer
	var got []string
	cli.Command("oo", "", func(args []string) error {
		got = args
		return nil
	})
	cli.SetIO(strings.NewReader("\x1bfoo\r"), &output)
	cli.Run()
	if !reflect.DeepEqual(got, []string{"oo"}) {
		t.Fatalf("ran %#v, want [oo] (output %q)", got, output.String())
	}
}

func TestHistoryEntriesStayOnOneLine(t *testing.T) {
	// History is displayed one entry per line and saved to a line oriented file.
	cli := New()
	cli.AddHistory("first\nsecond\rthird")
	for _, line := range cli.GetHistory() {
		if strings.ContainsAny(line, "\r\n") {
			t.Fatalf("history entry spans lines: %q", line)
		}
	}

	file, err := ioutil.TempFile("", "cshell-history")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if err := cli.SaveHistory(file.Name()); err != nil {
		t.Fatal(err)
	}
	loaded := New()
	if err := loaded.LoadHistory(file.Name()); err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.GetHistory(), cli.GetHistory(); !reflect.DeepEqual(got, want) {
		t.Fatalf("history did not round trip: %#v, want %#v", got, want)
	}
}

// XXX more test coverage
