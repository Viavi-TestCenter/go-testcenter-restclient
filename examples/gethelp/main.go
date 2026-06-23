// Usage: go run ./examples/gethelp <server_addr>
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
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/gethelp <server_addr>")
		os.Exit(1)
	}

	server := flag.Arg(0)
	fmt.Printf("==> server: %s\n\n", server)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("=== Usage Help ===")
	out, err := client.Help("", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(out)

	fmt.Println("\n=== Commands Help ===")
	out, err = client.Help("commands", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(out)

	fmt.Println("\n=== Command \"perform\" Help ===")
	out, err = client.Help("perform", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(out)
}
