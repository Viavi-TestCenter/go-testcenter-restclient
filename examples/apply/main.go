// Usage: go run ./examples/apply <server_addr>
package main

import (
	"flag"
	"fmt"
	"os"

	tc "github.com/Spirent-STC/go-testcenter-restclient"
)

const (
	sessionName = "extest"
	userName    = "someuser"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/apply <server_addr>")
		os.Exit(1)
	}
	server := flag.Arg(0)
	sessionID := sessionName + " - " + userName

	fmt.Printf("==> server:  %s\n==> session: %s\n\n", server, sessionID)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		die(err)
	}
	if _, err := client.JoinSession(sessionID); err != nil {
		die(err)
	}

	if err := client.Apply(); err != nil {
		die(err)
	}
	fmt.Println("apply: accepted")
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
