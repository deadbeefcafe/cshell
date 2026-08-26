package cshell

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
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

// XXX more test coverage
