package main

import (
	"github.com/deadbeefcafe/cshell"
	"github.com/pkg/term"
)

func main() {

	tty, _ := term.Open("/dev/tty")
	term.RawMode(tty)

	sh := cshell.New()
	sh.SetIO(tty, tty)
	sh.SetPrompt("mysh> ")

	sh.Command("foo", "do foo", func(args []string) error {
		sh.Printf("XXXX doing foo\r\n")
		return nil
	})

	sh.Command("bar", "do bar", func(args []string) error {
		sh.Printf("YYYY doing bar\r\n")
		return nil
	})

	sh.Command("exit", "the end", func(args []string) error {
		sh.Terminate()
		return nil
	})

	sh.CommandOneArg("baz", "a one arg command", func(args []string) error {
		sh.Printf("len(args) = %d\r\n", len(args))
		sh.Printf("args=%#v\r\n", args)
		return nil
	})

	sh.Run()

	tty.Restore()
	tty.Close()

}
