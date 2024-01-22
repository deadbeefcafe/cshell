package main

import (
	"github.com/deadbeefcafe/cshell"
	"github.com/deadbeefcafe/telnet"
)

func main() {
	telnet := telnet.New(":2323")
	telnet.Motd = `
   __  __       _ _   _   ____  _          _ _  __
  |  \/  |_   _| | |_(_) / ___|| |__   ___| | | \ \
  | |\/| | | | | | __| | \___ \| '_ \ / _ \ | |  \ \
  | |  | | |_| | | |_| |  ___) | | | |  __/ | |  / /
  |_|  |_|\__,_|_|\__|_| |____/|_| |_|\___|_|_| /_/


`
	telnet.CtrlDclose = true

	sh := cshell.New()
	sh.SetIO(telnet, telnet)
	sh.SetPrompt("telnet shell% ")

	sh.Command("foo", "do foo", func(args []string) error {
		sh.Printf("XXXX doing foo\r\n")
		return nil
	})

	sh.Command("bar", "do bar", func(args []string) error {
		sh.Printf("YYYY doing bar\r\n")
		return nil
	})

	sh.CommandOneArg("baz", "a one arg command", func(args []string) error {
		sh.Printf("len(args) = %d\r\n", len(args))
		sh.Printf("args=%#v\r\n", args)
		return nil
	})

	sh.Command("exit", "the end", func(args []string) error {
		sh.Terminate()
		return nil
	})

	sh.Run()
}
