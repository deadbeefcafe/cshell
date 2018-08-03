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
	sh.Run()

	tty.Restore()
	tty.Close()

}
