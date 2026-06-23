// Usage: go run ./examples/createsession [-port N] <server_addr>
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
	port := flag.Int("port", 0, "HTTP(S) port (0 uses TC_SERVER_PORT or the protocol default)")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/createsession [-port N] <server_addr>")
		os.Exit(1)
	}

	server := flag.Arg(0)
	fmt.Printf("==> server: %s\n", server)
	if *port != 0 {
		fmt.Printf("==> port:   %d\n", *port)
	}
	fmt.Println()

	client, err := tc.NewClient(tc.Options{Server: server, Port: *port, Debug: *debug})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sid, err := client.NewSession(userName, sessionName, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("session:", sid)
}
