package main

import (
	"log"

	"github.com/deadbeefcafe/cshell"
	"github.com/gliderlabs/ssh"
)

func main() {
	ssh.Handle(func(s ssh.Session) {
		sh := cshell.New()
		sh.SetIO(s, s)
		sh.SetPrompt("ssh shell> ")
		sh.Run()
	})

	log.Println("starting ssh server on port 2222...")
	log.Fatal(ssh.ListenAndServe(":2222", nil))
}
