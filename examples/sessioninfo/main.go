// Usage: go run ./examples/sessioninfo <server_addr>
package main

import (
	"flag"
	"fmt"
	"os"

	tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/sessioninfo <server_addr>")
		os.Exit(1)
	}

	server := flag.Arg(0)
	fmt.Printf("==> server: %s\n\n", server)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sessions, err := client.Sessions()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, session := range sessions {
		fmt.Println("session:", session)
		info, err := client.SessionInfo(session)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for k, v := range info {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
}
