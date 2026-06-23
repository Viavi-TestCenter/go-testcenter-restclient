// Usage: go run ./examples/systeminfo <server_addr>
package main

import (
	"flag"
	"fmt"
	"os"

	tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

const (
	sessionName = "extest"
	userName    = "someuser"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/systeminfo <server_addr>")
		os.Exit(1)
	}
	server := flag.Arg(0)
	sessionID := sessionName + " - " + userName

	fmt.Printf("==> server:  %s\n==> session: %s\n\n", server, sessionID)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := client.JoinSession(sessionID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	info, err := client.SystemInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("system info:")
	for k, v := range info {
		fmt.Printf("  %s: %v\n", k, v)
	}
}
