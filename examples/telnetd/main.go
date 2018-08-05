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
	sh.Run()
}
