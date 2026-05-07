// Usage: go run ./examples/listsessions <server_addr>
package main

import (
	"flag"
	"fmt"
	"os"

	tc "github.com/Spirent-STC/go-testcenter-restclient"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/listsessions <server_addr>")
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
	fmt.Println("sessions:")
	for _, s := range sessions {
		fmt.Printf("  %s\n", s)
	}
}
