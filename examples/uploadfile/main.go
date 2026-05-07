// Usage: go run ./examples/uploadfile <server_addr>
//
// Looks up sample.tcc in the working directory first, then next to this
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

const (
	sessionName = "extest"
	userName    = "someuser"
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/uploadfile <server_addr>")
		os.Exit(1)
	}
	server := flag.Arg(0)
	sessionID := sessionName + " - " + userName

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		die(err)
	}
	if _, err := client.JoinSession(sessionID); err != nil {
		die(err)
	}

	fileName, err := resolveDataFile("sample.tcc")
	if err != nil {
		die(err)
	}

	fmt.Printf("==> server:  %s\n==> session: %s\n==> file:    %s\n\n", server, sessionID, fileName)

	data, err := client.Upload(fileName, "")
	if err != nil {
		die(err)
	}
	fmt.Println("uploaded:", filepath.Base(fileName))
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			fmt.Printf("  %s: %v\n", k, v)
		}
	} else {
		fmt.Printf("  %v\n", data)
	}
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

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
