package cshell

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

// XXX more test coverage
