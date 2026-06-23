# Go TestCenter REST API Client: go-testcenter-restclient

The `testcenterrestclient` package provides a Go client for the VIAVI TestCenter REST API: create or join sessions, read and write objects, apply configs, upload and download files, and perform commands.

The package provides two clients:

- **`Client`** — use this when connecting to a **classic LabServer** or **standalone stcweb**. It connects directly to the server over HTTP/HTTPS using a configured server address. Construct with `NewClient(Options{...})`.

- **`AionClient`** — use this when connecting to a **LabServer hosted on the AION platform** (deployed as either **TC LabServer** or **TestCenter+**). It logs in to AION, discovers the target product instance, and transparently refreshes Bearer tokens for the lifetime of the session. Construct with `NewAionClient(AionOptions{...})`.

**API documentation:** <https://pkg.go.dev/github.com/Viavi-TestCenter/go-testcenter-restclient>

## Topics

- [Quick Start](#quick-start)
- [Installation](#installation)
- [Client](#client)
- [AionClient](#aionclient)
- [Debug Logging](#debug-logging)
- [API Reference](#api-reference)
- [Automation API to REST API Quick Reference](#automation-api-to-rest-api-quick-reference)
- [Support](#support)
- [License](#license)

## Quick Start

- Install the module:

  ```sh
  go get github.com/Viavi-TestCenter/go-testcenter-restclient
  ```

- Write Go code to talk with a TestCenter REST API server:

  ```go
  package main

  import (
      "fmt"
      "log"
      "time"

      tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
  )

  func main() {
      client, err := tc.NewClient(tc.Options{
          Server:  "server.somewhere.com",
          Timeout: 300 * time.Second,
      })
      if err != nil {
          log.Fatal(err)
      }
      sid, err := client.NewSession("JoeUser", "ExampleTest", false)
      if err != nil {
          log.Fatal(err)
      }
      info, _ := client.SystemInfo()
      fmt.Println(sid, info)
  }
  ```

- Browse the [examples](examples) directory for ready-to-run programs.

## Installation

Install (or upgrade) the latest published version:

```sh
go get -u github.com/Viavi-TestCenter/go-testcenter-restclient@latest
```

Requires Go 1.20+.

To build from source, clone the repository and run `go build ./...` from the repo root. Every `examples/<name>` subdirectory is an independent `main` package:

```sh
git clone https://github.com/Viavi-TestCenter/go-testcenter-restclient.git
cd go-testcenter-restclient
go build ./...
go run ./examples/createsession 10.0.0.5
```

## Client

`NewClient` returns a `*Client` that talks directly to a TestCenter REST API server over HTTP or HTTPS, using the server address you supply. It exposes the full TestCenter automation surface — sessions, objects, configs, files, and commands.

### Parameters (`Options`)

| Field | Type | Default / Env | Description |
|---|---|---|---|
| `Server`     | `string`        | `TC_SERVER_ADDRESS` (required) | TestCenter REST API host. |
| `Port`       | `int`           | `TC_SERVER_PORT`, else `80`/`443` | HTTP(S) port. |
| `UseHTTPS`   | `bool`          | `false` | Select HTTPS over HTTP. |
| `CACertFile` | `string`        | — | PEM bundle used to verify HTTPS. Required when `UseHTTPS` is true. |
| `Debug`      | `bool`          | `false` | Log request/response traces. |
| `Timeout`    | `time.Duration` | no timeout | Per-request timeout. |
| `LogWriter`  | `io.Writer`     | `os.Stdout` | Where debug output goes. Use `io.Discard` to silence. |

### Walkthrough

The example below walks through a typical test session end-to-end — opening a client, creating a session, building a small configuration, connecting to a chassis, applying it, running the sequencer, transferring files, and tearing the session down. 

```go
package main

import (
    "fmt"
    "log"
    "time"

    tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

func main() {
    client, err := tc.NewClient(tc.Options{
        Server: "server.somewhere.com",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Per-request timeout. Zero disables timeouts.
    // client.SetTimeout(100 * time.Second)

    // Create and join a new session.
    sid, err := client.NewSession("JoeUser", "ExampleTest", false)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("session:", sid)

    // Or: join an already existing session.
    // _, _ = client.JoinSession("OtherTest - JoeUser")

    // Get system information.
    sysInfo, _ := client.SystemInfo()
    fmt.Println(sysInfo)

    // Create a Project.
    project, _ := client.Create("project", "", nil)

    // Create a Port under the project.
    port, _ := client.Create("port", project, nil)

    // Connect to a chassis.
    _, _ = client.Connect([]string{"172.16.23.54"})

    // Configure the port location.
    _ = client.Config(port, map[string]interface{}{"location": "//172.16.23.54/1/1"})

    // Create a StreamBlock under the port.
    _, _ = client.Create("streamBlock", port, nil)

    // Apply config.
    _ = client.Apply()

    // Wait for the sequencer to finish (zero/negative disables the time limit).
    _, _ = client.WaitUntilComplete(30 * time.Second)

    // Write a message to the server log file.
    _ = client.Log("INFO", "Done with my test")

    // List available files.
    files, _ := client.Files()
    fmt.Println(files)

    // Download diagnostics.tgz.
    path, size, _ := client.Download("diagnostics.tgz", "")
    fmt.Println(path, size)

    // ...Or download every file to a directory.
    _, _ = client.DownloadAll(".")

    // Upload a config file.
    _, _ = client.Upload("config.xml", "")

    // Load TestCenter config from that file.
    _, _ = client.Perform("LoadFromXml", map[string]interface{}{"filename": "config.xml"})

    // Detach and delete the session.
    _, _ = client.EndSession(tc.EndDelete, "", 30*time.Second)
}
```

## AionClient

`NewAionClient` returns an `*AionClient` that authenticates with AION, discovers the target LabServer endpoint from the AION inventory, and injects a Bearer token into every HTTP request. Tokens are refreshed proactively (at 80% of their lifetime) and reactively (on a 401 with application code 4002 — `AUTH_TOKEN_INVALID`), so long-running sessions remain authenticated without any extra code.

### Parameters (`AionOptions`)

| Field | Type | Default / Env | Description |
|---|---|---|---|
| `AionURL`       | `string`        | `AION_URL` (required)      | Base URL of the AION platform, e.g. `https://aion.example.com`. |
| `Username`      | `string`        | `AION_USERNAME` (required) | AION username. |
| `Password`      | `string`        | `AION_PASSWORD` (required) | AION user password. |
| `NodeName`      | `string`        | —                          | AION node name hosting the target product instance, e.g. `10.109.120.117`. Use together with `UIPort` to uniquely identify an instance when multiple instances are present. |
| `UIPort`        | `int`           | —                          | UI port of the target product instance — the port number visible in AION's Product Manager page (e.g. `64006`). Use together with `NodeName`. Omit when only one instance is present. |
| `ProductCACert` | `string`        | —                          | Path to CA certificate file used to verify the discovered stcapi HTTPS endpoint. Required when stcapi uses HTTPS. |
| `AionCACert`    | `string`        | —                          | Path to CA certificate file used to verify the AION platform HTTPS endpoint. Required when AION uses HTTPS. |
| `Debug`         | `bool`          | `false`                    | Log request/response traces. |
| `Timeout`       | `time.Duration` | no timeout                 | Per-request timeout. |
| `LogWriter`     | `io.Writer`     | `os.Stdout`                | Where debug output goes. Use `io.Discard` to silence. |

`AionURL`, `Username`, and `Password` may be supplied as fields on `AionOptions` or via the matching environment variables. The explicit option takes precedence when both are set. `NewAionClient` returns an error at construction time if any of the three is missing from both sources.

### Basic Usage

```go
package main

import (
    "log"

    tc "github.com/Viavi-TestCenter/go-testcenter-restclient"
)

func main() {
    client, err := tc.NewAionClient(tc.AionOptions{
        AionURL:       "https://aion.example.com",
        Username:      "user@example.com",
        Password:      "secret",
        ProductCACert: "/path/to/product_ca_cert.pem",
    })
    if err != nil {
        log.Fatal(err)
    }

    if _, err := client.NewSession("myuser", "mysession", false); err != nil {
        log.Fatal(err)
    }
    if _, err := client.Create("port", "project1", map[string]interface{}{"name": "myport"}); err != nil {
        log.Fatal(err)
    }
    _, _ = client.EndSession(tc.EndDelete, "", 0)
}
```

### Using Environment Variables

Credentials can be supplied entirely via environment variables, keeping them out of source code:

```bash
export AION_URL=https://aion.example.com
export AION_USERNAME=user@example.com
export AION_PASSWORD=secret
```

```go
// No credentials in code.
client, err := tc.NewAionClient(tc.AionOptions{
    ProductCACert: "/path/to/product_ca_cert.pem",
})
```

### Obtaining the AION Cluster Certificate for `AionCACert`

If the AION platform uses a self-signed certificate, point `AionCACert` at a local PEM file. Retrieve it from the cluster with `openssl`:

```bash
echo | openssl s_client -connect 10.109.120.117:443 2>/dev/null | openssl x509 > aion_cluster.pem
```

> **Windows users:** `openssl` is not on PATH by default. It ships with Git for Windows (`C:\Program Files\Git\usr\bin\openssl.exe`) or can be installed standalone via `winget install ShiningLight.OpenSSL`.

Replace `10.109.120.117` with the address of your AION cluster, then pass the file path:

```go
client, err := tc.NewAionClient(tc.AionOptions{
    AionURL:    "https://10.109.120.117",
    Username:   "user@example.com",
    Password:   "secret",
    AionCACert: "aion_cluster.pem",
})
```

### Selecting a Specific Instance

When multiple LabServer instances are running across AION nodes, combine `NodeName` and `UIPort` to uniquely identify the target instance. Both are visible in AION's Product Manager web page. The matching stcapi port is discovered automatically.

```go
client, err := tc.NewAionClient(tc.AionOptions{
    AionURL:       "https://aion.example.com",
    Username:      "user@example.com",
    Password:      "secret",
    NodeName:      "10.109.120.117",
    UIPort:        64006,
    ProductCACert: "/path/to/product_ca_cert.pem",
})
```

## Debug Logging

Set `Debug: true` on `Options` / `AionOptions` to print request/response traces. Output goes to `os.Stdout` by default. Route it anywhere with `LogWriter io.Writer`:

```go
// Log to a file.
f, _ := os.OpenFile("client.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
defer f.Close()

client, _ := tc.NewClient(tc.Options{
    Server:    "10.0.0.5",
    Debug:     true,
    LogWriter: f,
})

// Silence debug output entirely.
client, _ = tc.NewClient(tc.Options{
    Server:    "10.0.0.5",
    Debug:     true,
    LogWriter: io.Discard,
})
```

`LogWriter` accepts any `io.Writer`, so structured loggers (`slog`, `zerolog`, `zap`) can be adapted by wrapping them in a writer.

## API Reference

All methods documented below are defined on `*Client` (returned by `NewClient`) or `*AionClient` (returned by `NewAionClient`).

### Client Lifecycle & State

| Signature | Purpose |
|---|---|
| `SessionID() string`                 | Current session ID, or `""` if not in a session. |
| `Started() bool`                     | Reports whether a session is active. |
| `Debug() bool`                       | Reports whether debug printing is enabled. |
| `EnableDebug()` / `DisableDebug()`   | Toggle request/response tracing. |
| `Timeout() time.Duration`            | Current per-request timeout. |
| `SetTimeout(d time.Duration)`        | Update per-request timeout. Zero disables timeouts. |

### Session Lifecycle

| Signature | Purpose |
|---|---|
| `NewSession(userName, sessionName string, killExisting bool) (string, error)` | Create a new test session. Empty names let the server assign defaults. |
| `JoinSession(sid string) (string, error)`                                  | Attach to an existing session; returns BLL version. |
| `EndSession(mode EndMode, sid string, timeout time.Duration) (bool, error)`| Terminate a session. `mode` is `EndDetach` / `EndRemove` / `EndDelete` / `EndKill`. |
| `Sessions() ([]string, error)`                                             | IDs of all active sessions on the server. |
| `SessionURLs() ([]string, error)`                                          | One URL per active session. |
| `SessionInfo(sessionID string) (map[string]interface{}, error)`            | Server metadata for a session. Pass `""` for the current session. |

#### EndSession Modes

`EndSession(mode EndMode, sid string, timeout time.Duration)` controls what happens to the remote session. The `EndMode` constants choose the disposition:

| `EndMode`   | Behavior |
|---|---|
| `EndDetach` | Stop using the session locally; do not contact the server. |
| `EndRemove` | End the client controller but leave the test session on the server. |
| `EndDelete` | End the client controller and terminate the test session (most common). |
| `EndKill`   | Forcibly kill the session process. |

### System Info

| Signature | Purpose |
|---|---|
| `BllVersion() (string, error)`                  | BLL version for the current session, or `""` if not started. |
| `SystemInfo() (map[string]interface{}, error)`  | `/system` endpoint payload. |
| `ServerInfo() (map[string]interface{}, error)`  | Attributes of the `system1` object. |

### Core Object API

| Signature | Purpose |
|---|---|
| `Apply() error`                                                                         | Push the current test configuration to the chassis. |
| `Get(handle string, attrs ...string) (interface{}, error)`                              | Read attributes/relations on an object. Return shape depends on the number of attrs requested — see note below. |
| `Create(objectType, under string, attributes map[string]interface{}) (string, error)`   | Create an object and return its handle. |
| `CreateX(objectType, under string, attributes map[string]interface{}) (map[string]interface{}, error)` | Like `Create`, but returns the full response. |
| `Config(handle string, attributes map[string]interface{}) error`                        | Set attributes/relations on an object. |
| `Delete(handle string) error`                                                           | Remove an object by handle. |
| `Perform(command string, params map[string]interface{}) (map[string]interface{}, error)`| Execute a named TestCenter command. |

**`Get` return shape.** The interface{} returned by `Get` varies with the number of attributes requested. The caller is responsible for the appropriate type assertion.

| Attrs requested | Returns |
|---|---|
| None              | `map[string]interface{}` containing every attribute on the object. |
| Exactly one       | The bare value of that attribute (usually `string`). |
| Two or more       | `map[string]interface{}` keyed by attribute name. |

### Chassis / Connections

| Signature | Purpose |
|---|---|
| `Chassis() ([]string, error)`                                  | Known chassis in the session. |
| `ChassisInfo(chassis string) (map[string]interface{}, error)`  | Info for a single chassis. |
| `Connections() (map[string]bool, error)`                       | Map of chassis address to connected flag. |
| `IsConnected(chassis string) (bool, error)`                    | Whether a chassis is currently connected. |
| `Connect(chassisList []string) ([]interface{}, error)`         | Establish connections; returns per-chassis result. |
| `Disconnect(chassisList []string) error`                       | Drop connections to one or more chassis. |
| `ConnectAll() error` / `DisconnectAll() error`                 | Connect/disconnect every chassis in this session. |

### Files

| Signature | Purpose |
|---|---|
| `Files() ([]string, error)`                                             | List files available for this session. |
| `FileURLs() ([]string, error)`                                          | One URL per file in this session. |
| `Download(fileName, saveAs string) (string, int64, error)`              | Fetch a file. `saveAs` may be empty to use the server-provided name. Returns `(localPath, bytesWritten, err)`. |
| `DownloadAll(dstDir string) (map[string]int64, error)`                  | Download every file in this session into `dstDir`. |
| `Upload(srcPath, dstName string) (interface{}, error)`                  | Send a single file to the server. |

### Sequencer

| Signature | Purpose |
|---|---|
| `WaitUntilComplete(timeout time.Duration) (string, error)` | Block until the sequencer enters `PAUSE` or `IDLE`. Zero/negative timeout disables the time limit. |

### Help & Logging

| Signature | Purpose |
|---|---|
| `Help(subject string, args []string) (string, error)` | API documentation. Pass `""` for an overview or `"commands"` for the command list. |
| `Log(level, msg string) error`                        | Write a diagnostic message to the TestCenter REST API server log. `level` ∈ `INFO`/`WARN`/`ERROR`/`FATAL`. |

### Errors

The package exposes four error types that callers can match with `errors.As` for richer handling than a bare string check:

| Type                 | When it's returned                                                                                          |
|----------------------|-------------------------------------------------------------------------------------------------------------|
| `*RestHttpError`     | Any non-2xx response from the stcapi server. Carries `HTTPStatus`, `HTTPReason`, `Code`, and `Msg`. The `Status()` method returns `HTTPStatus` (useful when matching against an interface). |
| `*TokenExpiredError` | An HTTP 401 with application code `4002` (`AUTH_TOKEN_INVALID`). Wraps `*RestHttpError` (so `errors.As` against `*RestHttpError` also matches). On `AionClient`, the reactive-refresh hook handles this transparently — callers usually never see it. |
| `*ConnectionError`   | Low-level transport failures (DNS, TCP, TLS).                                                               |
| `*AionError`         | Failures from the AION IAM or inventory endpoints (login, refresh, organization lookup, product-instance discovery). Returned by `NewAionClient` and the internal token-refresh paths. Carries `Message` and `HTTPStatus` (0 if the failure occurred before a response). |

```go
_, err := client.SystemInfo()
var httpErr *tc.RestHttpError
var connErr *tc.ConnectionError
var aionErr *tc.AionError
switch {
case errors.As(err, &httpErr):
    log.Printf("server returned %d %s (code %d)", httpErr.HTTPStatus, httpErr.HTTPReason, httpErr.Code)
case errors.As(err, &connErr):
    log.Printf("transport failure: %s", connErr.Msg)
case errors.As(err, &aionErr):
    log.Printf("AION error (HTTP %d): %s", aionErr.HTTPStatus, aionErr.Message)
case err != nil:
    log.Printf("other error: %v", err)
}
```

## Automation API to REST API Quick Reference

| Automation API    | Client API                         | REST Equivalent                                            |
| ----------------- | ----------------------------------- | ---------------------------------------------------------- |
| apply             | `Apply()`                           | PUT http://host.domain/stcapi/apply                        |
| config            | `Config(obj, attrs)`                | PUT http://host.domain/stcapi/objects/{object}             |
| connect           | `Connect([]string{chassis, ..})`    | PUT http://host.domain/stcapi/connections/{chassis}        |
| create            | `Create(objType, under, attrs)`     | POST http://host.domain/stcapi/objects/                    |
| delete            | `Delete(obj)`                       | DELETE http://host.domain/stcapi/objects/{object}          |
| disconnect        | `Disconnect([]string{chassis, ..})` | DELETE http://host.domain/stcapi/connections/{chassis}     |
| get               | `Get(obj, attrs...)`                | GET http://host.domain/stcapi/objects/{object}             |
| help              | `Help(subject, nil)`                | GET http://host.domain/stcapi/help/{subject}               |
| help list         | `Help("list", args)`                | GET http://host.domain/stcapi/help/list?{search_info}      |
| log               | `Log(level, msg)`                   | POST http://host.domain/stcapi/system/log/                 |
| perform           | `Perform(command, params)`          | POST http://host.domain/stcapi/perform/{command}           |
| release           | `Perform("releasePort", params)`    | See perform                                                |
| reserve           | `Perform("reservePort", params)`    | See perform                                                |
| sleep             | N/A                                 | NOT SUPPORTED — client must implement                      |
| subscribe         | `Perform("ResultsSubscribe", ...)`   | See perform                                               |
| unsubscribe       | `Perform("ResultDataSetUnsubscribe", ...)` | See perform                                         |
| waitUntilComplete | `WaitUntilComplete(timeout)`        | Polls sequencer state                                      |

## Support

For bug reports, feature requests, or questions, contact **VIAVI Solutions** at <hse.support@viavisolutions.com>, or file an issue at <https://github.com/Viavi-TestCenter/go-testcenter-restclient/issues>.

## License

Released under the MIT License — see [LICENSE](LICENSE).
