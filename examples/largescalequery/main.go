// Usage: go run ./examples/largescalequery <server_addr>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

const (
	sessionName          = "extest"
	userName             = "someuser"
	portNumber           = 2
	deviceNumberPerPort  = 2000
)

func main() {
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/largescalequery <server_addr>")
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

	createBgpv4(client)
	fmt.Println("setup:    bgp configurations finished")
	getAllBgpv4ViaLoop(client)
	getAllBgpv4ViaCmd(client)
	getSpecifiedBgpv4ViaRootList(client)
	getSpecifiedBgpv4ViaCondition(client)
}

func createBgpv4(client *tc.Client) {
	for i := 1; i <= portNumber; i++ {
		portHdl, err := client.Create("port", "project1", map[string]interface{}{
			"name": fmt.Sprintf("myport_%d", i),
		})
		if err != nil {
			die(err)
		}
		retData, err := client.Perform("DeviceCreateCommand", map[string]interface{}{
			"DeviceType":  "EmulatedDevice",
			"ParentList":  "project1",
			"CreateCount": deviceNumberPerPort,
			"Port":        portHdl,
			"IfStack":     "Ipv4If VlanIf EthIIIf Ipv6If",
			"IfCount":     "1 1 1 1",
		})
		if err != nil {
			die(err)
		}
		rl, _ := retData["ReturnList"].(string)
		devhdlList := strings.Fields(rl)
		j := 1
		for _, devhdl := range devhdlList {
			bgpcfgHdl, err := client.Create("BgpRouterConfig", devhdl, map[string]interface{}{
				"AsNum":    1111,
				"DutAsNum": 2222,
				"name":     fmt.Sprintf("myBGP_R_%d_%d", i, j),
			})
			if err != nil {
				die(err)
			}
			if _, err := client.Create("BgpIpv4RouteConfig", bgpcfgHdl, map[string]interface{}{
				"AsPath": fmt.Sprintf("11%d%d", i, j),
				"name":   fmt.Sprintf("myBGPV4_%d_%d", i, j),
			}); err != nil {
				die(err)
			}
			j++
		}
	}
}

func getAllBgpv4ViaLoop(client *tc.Client) {
	start := time.Now()

	ports, err := client.Get("project1", "children-port")
	if err != nil {
		die(err)
	}
	for _, port := range fieldsOf(ports) {
		devs, err := client.Get(port, "affiliationport-Sources")
		if err != nil {
			die(err)
		}
		for _, device := range fieldsOf(devs) {
			bgpconfig, err := client.Get(device, "children-BgpRouterConfig")
			if err != nil {
				die(err)
			}
			if bgpconfig == nil {
				continue
			}
			bgpStr := strings.TrimSpace(fmt.Sprint(bgpconfig))
			if bgpStr == "" {
				continue
			}
			bgpv4, err := client.Get(bgpStr, "children-bgpipv4routeconfig")
			if err != nil {
				die(err)
			}
			for _, bgproute := range fieldsOf(bgpv4) {
				if _, err := client.Get(bgproute, "Name"); err != nil {
					die(err)
				}
				if _, err := client.Get(bgproute+".ipv4networkblock", "StartIpList"); err != nil {
					die(err)
				}
			}
		}
	}
	fmt.Println("\n=== Get All via client.Get loop ===")
	fmt.Printf("time taken: %.3fs\n", time.Since(start).Seconds())
}

func getAllBgpv4ViaCmd(client *tc.Client) {
	start := time.Now()
	result, err := client.Perform("GetObjectsCommand", map[string]interface{}{
		"ClassName":    "BgpIpv4RouteConfig",
		"PropertyList": "Name ipv4networkblock.StartIpList",
	})
	if err != nil {
		die(err)
	}
	pv, _ := result["PropertyValues"].(string)
	var parsed interface{}
	_ = json.Unmarshal([]byte(pv), &parsed)
	fmt.Println("\n=== Get All via GetObjectsCommand ===")
	fmt.Printf("time taken: %.3fs\n", time.Since(start).Seconds())
}

func getSpecifiedBgpv4ViaRootList(client *tc.Client) {
	result, err := client.Perform("GetObjectsCommand", map[string]interface{}{
		"ClassName":    "BgpIpv4RouteConfig",
		"RootList":     "emulateddevice1 emulateddevice2",
		"PropertyList": "Name ipv4networkblock.StartIpList",
	})
	if err != nil {
		die(err)
	}
	fmt.Println("\n=== Get via RootList ===")
	fmt.Println("values:", result["PropertyValues"])
}

func getSpecifiedBgpv4ViaCondition(client *tc.Client) {
	result, err := client.Perform("GetObjectsCommand", map[string]interface{}{
		"ClassName":    "BgpIpv4RouteConfig",
		"Condition":    "AsPath='1114' OR AsPath='1123'",
		"PropertyList": "Name ipv4networkblock.StartIpList",
	})
	if err != nil {
		die(err)
	}
	fmt.Println("\n=== Get via Condition ===")
	fmt.Println("values:", result["PropertyValues"])
}

func fieldsOf(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return strings.Fields(t)
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if e == nil {
				continue
			}
			out = append(out, fmt.Sprint(e))
		}
		return out
	default:
		return strings.Fields(fmt.Sprint(v))
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
