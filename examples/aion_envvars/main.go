// Connect to the Aion platform using environment variables for credentials.
//
// Usage:
//
//	export AION_URL=https://aion.example.com
//	export AION_USERNAME=user@example.com
//	export AION_PASSWORD=secret
//	go run ./examples/aion_envvars [<aion_ca> [<product_ca>]]
//

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

const (
	sessionName = "aionex"
	userName    = "aionuser"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	aionCA := ""
	if flag.NArg() >= 1 {
		aionCA = flag.Arg(0)
	}
	productCA := ""
	if flag.NArg() >= 2 {
		productCA = flag.Arg(1)
	}

	if aionCA != "" {
		fmt.Printf("==> aion ca:     %s\n", aionCA)
	}
	if productCA != "" {
		fmt.Printf("==> product ca:  %s\n", productCA)
	}

	// AionURL/Username/Password read from AION_URL, AION_USERNAME,
	// AION_PASSWORD environment variables.
	client, err := tc.NewAionClient(tc.AionOptions{
		AionCACert:    aionCA,
		ProductCACert: productCA,
		Debug:         *debug,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	sessionID := sessionName + " - " + userName

	if _, err := client.JoinSession(sessionID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	info, err := client.SystemInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("system info:", info)

	if _, err := client.EndSession(tc.EndDelete, "", 30*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("session ended: complete")
}
