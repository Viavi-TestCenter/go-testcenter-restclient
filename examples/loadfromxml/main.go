// Usage: go run ./examples/loadfromxml <server_addr>
//
// Looks up config.xml in the working directory first, then next to this
// source file, so it runs from either the repo root or the example dir.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	tc "github.com/Spirent-STC/go-testcenter-restclient"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/loadfromxml <server_addr>")
		os.Exit(1)
	}

	server := flag.Arg(0)

	configFile, err := resolveDataFile("config.xml")
	if err != nil {
		fmt.Println("missing file: config.xml")
		os.Exit(1)
	}

	fmt.Printf("==> server: %s\n==> config: %s\n\n", server, configFile)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("=== Create Session ===")
	if _, err := client.NewSession("someuser", "xmltest", false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("session: created")

	fmt.Println("\n=== Upload Config ===")
	if _, err := client.Upload(configFile, ""); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("upload:  complete")

	fmt.Println("\n=== Load From XML ===")
	data, err := client.Perform("LoadFromXml", map[string]interface{}{"filename": filepath.Base(configFile)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("result:", data)
}

func resolveDataFile(name string) (string, error) {
	if info, err := os.Stat(name); err == nil && !info.IsDir() {
		return name, nil
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidate := filepath.Join(filepath.Dir(file), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
