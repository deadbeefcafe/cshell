package main

import (
	"log"
	"net"
	"os"

	"github.com/deadbeefcafe/cshell"
	"github.com/gliderlabs/ssh"
)

// TEST:
// ssh  -oHostKeyAlgorithms=+ssh-rsa -p 2222 someuser@127.0.0.1

func main() {
	ssh.Handle(func(s ssh.Session) {
		rawcmd := s.RawCommand()
		if rawcmd != "" {
			log.Printf("RAWCMD: %v", rawcmd)
			return
		}

		sh := cshell.New()
		sh.SetIO(s, s)
		sh.SetPrompt("ssh shell> ")

		sh.Printf("RawCMD: %v\r\n", s.RawCommand())
		sh.Printf("Remote: %v\r\n", s.RemoteAddr())
		sh.Printf("Local:  %v\r\n", s.LocalAddr())
		sh.Printf("Subsys: %v\r\n", s.Subsystem())
		sh.Printf("User:   %v\r\n", s.User())

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
	})

	/*
		publicKeyOption := ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			return true // allow all keys, or use ssh.KeysEqual() to compare against known keys
		})

	*/

	connCallback := ssh.WrapConn(ssh.ConnCallback(func(ctx ssh.Context, conn net.Conn) net.Conn {
		log.Printf("ConnCallback: conn: %#v\n", conn)
		return conn
	}))
	/*

		publicKeyOption := ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			//return true // allow all keys, or use ssh.KeysEqual() to compare against known keys
			//return ssh.KeysEqual()
			// out , comment , options , rest , err  := ssh.ParseAuthorizedKey(in)
			return true
		})
	*/

	log.Println("starting ssh server on port 2222...")
	//log.Fatal(ssh.ListenAndServe(":2222", nil, ssh.HostKeyFile(".ssh/id_rsa")))
	homedir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("os.UserHomeDir: %v", err)
	}
	log.Fatal(ssh.ListenAndServe(":2222", nil, ssh.HostKeyFile(homedir+"/.ssh/id_rsa"), connCallback))
}
