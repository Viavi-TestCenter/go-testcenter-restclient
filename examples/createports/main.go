// Usage: go run ./examples/createports <server_addr>
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
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/createports <server_addr>")
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

	fmt.Println("=== Create Project ===")
	project, err := client.Create("project", "", nil)
	if err != nil {
		die(err)
	}
	fmt.Println("project handle:", project)

	projectName, err := client.Get(project, "name")
	if err != nil {
		die(err)
	}
	fmt.Println("project name:  ", projectName)

	fmt.Println("\n=== Create Ports ===")
	port1, err := client.Create("port", project, nil)
	if err != nil {
		die(err)
	}
	fmt.Println("port 1 handle:", port1)

	port2, err := client.Create("port", project, nil)
	if err != nil {
		die(err)
	}
	fmt.Println("port 2 handle:", port2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
