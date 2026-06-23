// Usage: go run ./examples/multichassis [-debug] <server_addr> <chassis1> <chassis2> [chassis3 ...]
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

	if flag.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/multichassis [-debug] <server_addr> <chassis1> <chassis2> [chassis3 ...]")
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

	// --- Connect (multi) ---
	fmt.Println("=== Connect (multi) ===")
	fmt.Println("chassis:     ", chassisArgs)
	ret, err := client.Connect(chassisArgs)
	if err != nil {
		die(err)
	}
	fmt.Println("result:      ", ret)

	for _, c := range chassisArgs {
		isConn, err := client.IsConnected(c)
		if err != nil {
			die(err)
		}
		ci, err := client.ChassisInfo(c)
		if err != nil {
			die(err)
		}
		fmt.Printf("  %-20s connected=%v info=%v\n", c, isConn, ci)
	}

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

	// --- Disconnect (multi) ---
	fmt.Println("\n=== Disconnect (multi) ===")
	fmt.Println("chassis:     ", chassisArgs)
	if err := client.Disconnect(chassisArgs); err != nil {
		die(err)
	}
	fmt.Println("disconnect:   complete")

	conns, err = client.Connections()
	if err != nil {
		die(err)
	}
	fmt.Println("connections: ", conns)

	// --- ConnectAll ---
	fmt.Println("\n=== ConnectAll ===")
	if err := client.ConnectAll(); err != nil {
		die(err)
	}
	fmt.Println("connect:      complete")

	conns, err = client.Connections()
	if err != nil {
		die(err)
	}
	fmt.Println("connections: ", conns)
	// --- DisconnectAll ---
	fmt.Println("\n=== DisconnectAll ===")
	if err := client.DisconnectAll(); err != nil {
		die(err)
	}
	fmt.Println("disconnect:   complete")
	
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
