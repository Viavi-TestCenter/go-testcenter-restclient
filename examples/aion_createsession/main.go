// Usage: go run ./examples/aion_createsession <aion_url> <username> <password> [<aion_ca> [<product_ca>]]
package main

import (
	"flag"
	"fmt"
	"os"

	tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

const (
	sessionName = "aionex"
	userName    = "aionuser"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/aion_createsession <aion_url> <username> <password> [<aion_ca> [<product_ca>]]")
		os.Exit(1)
	}

	aionCA := ""
	if flag.NArg() >= 4 {
		aionCA = flag.Arg(3)
	}
	productCA := ""
	if flag.NArg() >= 5 {
		productCA = flag.Arg(4)
	}

	fmt.Printf("==> aion url:    %s\n==> username:    %s\n", flag.Arg(0), flag.Arg(1))
	if aionCA != "" {
		fmt.Printf("==> aion ca:     %s\n", aionCA)
	}
	if productCA != "" {
		fmt.Printf("==> product ca:  %s\n", productCA)
	}
	fmt.Println()

	client, err := tc.NewAionClient(tc.AionOptions{
		AionURL:       flag.Arg(0),
		Username:      flag.Arg(1),
		Password:      flag.Arg(2),
		AionCACert:    aionCA,
		ProductCACert: productCA,
		Debug:         *debug,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sid, err := client.NewSession(userName, sessionName, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("session:       ", sid)

	info, err := client.SystemInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("system info:   ", info)

	project, err := client.Create("project", "", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("project handle:", project)

	port1, err := client.Create("port", project, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("port 1 handle: ", port1)

	port2, err := client.Create("port", project, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("port 2 handle: ", port2)
}
