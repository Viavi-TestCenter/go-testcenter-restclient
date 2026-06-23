// Usage: go run ./examples/chassis [-debug] <server_addr> <chassis1> [chassis2 ...]
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

	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/chassis [-debug] <server_addr> <chassis1> [chassis2 ...]")
		os.Exit(1)
	}
	server := flag.Arg(0)
	chassisArgs := flag.Args()[1:]

	fmt.Printf("==> server:  %s\n==> chassis: %v\n\n", server, chassisArgs)

	sessionID := sessionName + " - " + userName

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		die(err)
	}

	if _, err := client.JoinSession(sessionID); err != nil {
		die(err)
	}

	chassis := chassisArgs[0]

	// --- Connect ---
	fmt.Println("=== Connect ===")
	fmt.Println("chassis:     ", chassis)
	ret, err := client.Connect([]string{chassis})
	if err != nil {
		die(err)
	}
	fmt.Println("result:      ", ret)

	isConn, err := client.IsConnected(chassis)
	if err != nil {
		die(err)
	}
	fmt.Println("connected:   ", isConn)

	ci, err := client.ChassisInfo(chassis)
	if err != nil {
		die(err)
	}
	fmt.Println("info:        ", ci)

	list, err := client.Chassis()
	if err != nil {
		die(err)
	}
	fmt.Println("chassis list:", list)

	conns, err := client.Connections()
	if err != nil {
		die(err)
	}
	fmt.Println("connections: ", conns)

	// --- Disconnect ---
	fmt.Println("\n=== Disconnect ===")
	fmt.Println("chassis:     ", chassis)
	if err := client.Disconnect([]string{chassis}); err != nil {
		die(err)
	}
	fmt.Println("disconnect:   complete")

	ci, err = client.ChassisInfo(chassis)
	if err != nil {
		die(err)
	}
	fmt.Println("info:        ", ci)

	list, err = client.Chassis()
	if err != nil {
		die(err)
	}
	fmt.Println("chassis list:", list)

	conns, err = client.Connections()
	if err != nil {
		die(err)
	}
	fmt.Println("connections: ", conns)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
