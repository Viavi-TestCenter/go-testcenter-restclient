// Usage: go run ./examples/configtraffic <server_addr>
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/configtraffic <server_addr>")
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
	configTraffic(client)
}

func configTraffic(client *tc.Client) {
	port1 := "port1"
	port2 := "port2"

	fmt.Println("=== Create StreamBlock ===")
	sb1, err := client.Create("streamBlock", port1, nil)
	if err != nil {
		die(err)
	}
	fmt.Println("port 1 streamblock:", sb1)

	fmt.Println("\n=== Children ===")
	gen, err := client.Get(port1, "children-generator")
	if err != nil {
		die(err)
	}
	fmt.Println("port 1 generator:", joinVal(gen))

	ana, err := client.Get(port2, "children-analyzer")
	if err != nil {
		die(err)
	}
	fmt.Println("port 2 analyzer: ", joinVal(ana))
}

func joinVal(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(v)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
