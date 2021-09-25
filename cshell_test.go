package cshell

import (
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

func TestCli(t *testing.T) {

	// XXX make this actually test something
	commands := "\r\rtest1 \"foo bar baz\"\r\rtest2\r\rpass\r\r"

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
