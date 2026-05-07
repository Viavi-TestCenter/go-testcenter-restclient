// Usage: go run ./examples/downloadall [-debug] <server_addr> [<dst_dir>]
package main

import (
	"flag"
	"fmt"
	"os"

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
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/downloadall [-debug] <server_addr> [<dst_dir>]")
		os.Exit(1)
	}
	server := flag.Arg(0)
	dstDir := ""
	if flag.NArg() >= 2 {
		dstDir = flag.Arg(1)
	}
	sessionID := sessionName + " - " + userName

	fmt.Printf("==> server:  %s\n==> session: %s\n==> dstDir:  %s\n\n", server, sessionID, dstDir)

	client, err := tc.NewClient(tc.Options{Server: server, Debug: *debug})
	if err != nil {
		die(err)
	}
	if _, err := client.JoinSession(sessionID); err != nil {
		die(err)
	}

	// --- List session files ---
	fmt.Println("=== Files ===")
	files, err := client.Files()
	if err != nil {
		die(err)
	}
	if len(files) == 0 {
		fmt.Println("no files in session; nothing to download")
		return
	}
	fmt.Println("count:       ", len(files))
	for _, f := range files {
		fmt.Println("  ", f)
	}

	// --- Download a single file ---
	fmt.Println("\n=== Download (single) ===")
	first := files[0]
	fmt.Println("file:        ", first)
	saveName, n, err := client.Download(first, "")
	if err != nil {
		die(err)
	}
	fmt.Println("saved to:    ", saveName)
	fmt.Println("bytes:       ", n)
	fmt.Println("download:     complete")

	// --- Download all files ---
	fmt.Println("\n=== DownloadAll ===")
	saved, err := client.DownloadAll(dstDir)
	if err != nil {
		die(err)
	}
	fmt.Println("count:       ", len(saved))
	for path, size := range saved {
		fmt.Printf("  %s: %d bytes\n", path, size)
	}
	fmt.Println("downloadall:  complete")
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
