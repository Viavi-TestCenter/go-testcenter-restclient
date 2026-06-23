// Usage: go run ./examples/configports <server_addr>
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
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/configports <server_addr>")
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
	configPorts(client)
}

func configPorts(client *tc.Client) {
	port1 := "port1"
	port2 := "port2"
	chassisAddr := "10.100.20.60"
	slot := 2
	p1 := 1
	p2 := 2

	fmt.Println("=== Configure Locations ===")
	loc := fmt.Sprintf("//%s/%d/%d", chassisAddr, slot, p1)
	fmt.Println("port 1 set:", loc)
	if err := client.Config(port1, map[string]interface{}{"location": loc}); err != nil {
		die(err)
	}

	loc = fmt.Sprintf("//%s/%d/%d", chassisAddr, slot, p2)
	fmt.Println("port 2 set:", loc)
	if err := client.Config(port2, map[string]interface{}{"location": loc}); err != nil {
		die(err)
	}

	fmt.Println("\n=== Verify Locations ===")
	v, err := client.Get(port1, "location")
	if err != nil {
		die(err)
	}
	fmt.Println("port 1 get:", v)

	v, err = client.Get(port2, "location")
	if err != nil {
		die(err)
	}
	fmt.Println("port 2 get:", v)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
