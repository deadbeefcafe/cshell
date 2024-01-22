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
		sh.Printf("FOO args= %#v\r\n", args)
		return nil
	})

	sh.Command("dev info", "do foo bar", func(args []string) error {
		sh.Printf("DEV INFO args= %#v\r\n", args)
		return nil
	})

	sh.Command("dev info all the stuff", "big long command", func(args []string) error {
		sh.Printf("DEV INFO args= %#v\r\n", args)
		return nil
	})

	sh.Command("bar", "do bar", func(args []string) error {
		sh.Printf("BAR args= %#v\r\n", args)
		return nil
	})

	sh.Command("exit", "the end", func(args []string) error {
		sh.Terminate()
		return nil
	})

	sh.CommandOneArg("baz", "a one arg command", func(args []string) error {
		sh.Printf("BAZ #args=%d args=%#v\r\n", len(args), args)
		return nil
	})

	sh.Run()

	tty.Restore()
	tty.Close()

}
