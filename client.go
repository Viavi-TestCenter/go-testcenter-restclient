// Package testcenterrestclient is a Go client for the VIAVI TestCenter REST
// API. It provides two top-level constructors:
//
//   - NewClient: connects directly to a classic LabServer or standalone
//     stcweb instance.
//   - NewAionClient: connects to a LabServer hosted on the AION platform,
//     handling IAM login, endpoint discovery, and bearer-token refresh.
//
// Both constructors return clients that share the same operation surface
// (sessions, objects, configs, files, commands).
package testcenterrestclient


import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Default ports used when Options.Port is unset and TC_SERVER_PORT is empty.
const (
	HTTPDefaultPort  = 80
	HTTPSDefaultPort = 443
)

// EndMode controls how EndSession tears down a session.
type EndMode int

// EndSession dispositions; see EndSession for the full contract.
const (
	// EndDetach stops using the session locally without contacting the server.
	EndDetach EndMode = iota
	// EndRemove ends the local controller but leaves the test session running on the server.
	EndRemove
	// EndDelete ends the controller and terminates the test session (most common).
	EndDelete
	// EndKill forcefully terminates the test session process.
	EndKill
)

// Options configures a NewClient call. All fields are optional except as noted.
type Options struct {
	// Server is the TestCenter REST API host. Required; falls back to the
	// TC_SERVER_ADDRESS environment variable.
	Server string
	// Port is the HTTP(S) port. Zero falls back to TC_SERVER_PORT, then to
	// HTTPDefaultPort (80) or HTTPSDefaultPort (443).
	Port int
	// UseHTTPS selects HTTPS instead of HTTP.
	UseHTTPS bool
	// CACertFile is the PEM bundle used to verify HTTPS. Required when
	// UseHTTPS is true.
	CACertFile string
	// Debug enables HTTP request/response tracing via LogWriter.
	Debug bool
	// Timeout is the per-request timeout. Zero disables timeouts.
	Timeout time.Duration
	// Token, if non-empty, is sent as the initial Bearer token. Most callers
	// should leave this empty; it is used internally by NewAionClient.
	Token string
	// LogWriter is the destination for debug output. Nil defaults to
	// os.Stdout; pass io.Discard to silence.
	LogWriter io.Writer
}

// Client is a TestCenter REST API client. Construct with NewClient. All
// session and object methods below are defined on *Client (and inherited by
// *AionClient via embedding).
type Client struct {
	rest  *restHttp
	sid   string
	debug bool
}

// NewClient constructs a Client configured by opts. See Options for the
// available fields and their environment-variable fallbacks.
//
// The returned client has no active session; call NewSession or JoinSession
// next. NewClient probes the server's /sessions endpoint at construction
// time and returns an error if the server is unreachable or required
// configuration is missing (no server address, or HTTPS without a CA bundle).
func NewClient(opts Options) (*Client, error) {
	defaultPort := HTTPDefaultPort
	proto := "http"
	if opts.UseHTTPS {
		defaultPort = HTTPSDefaultPort
		proto = "https"
	}

	server := opts.Server
	if server == "" {
		server = os.Getenv("TC_SERVER_ADDRESS")
	}
	if server == "" {
		return nil, errors.New("TC_SERVER_ADDRESS not set")
	}
	port := opts.Port
	if port == 0 {
		if p := os.Getenv("TC_SERVER_PORT"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				port = v
			}
		}
	}
	if port == 0 {
		port = defaultPort
	}

	if opts.UseHTTPS && opts.CACertFile == "" {
		return nil, errors.New("CACertFile is required when UseHTTPS is true")
	}

	base, err := buildURL(proto, server, port, "stcapi")
	if err != nil {
		return nil, err
	}
	rest, err := newRestHttp(base, restOptions{
		CACertFile: opts.CACertFile,
		Debug:      opts.Debug,
		Timeout:    opts.Timeout,
		LogWriter:  opts.LogWriter,
	})
	if err != nil {
		return nil, err
	}
	if opts.Token != "" {
		rest.addHeader("Authorization", "Bearer "+opts.Token)
	}

	if _, _, err := rest.getRequest("sessions", "", nil, ""); err != nil {
		return nil, fmt.Errorf("cannot connect to STC server: %s:%d: %w", server, port, err)
	}

	return &Client{rest: rest, debug: opts.Debug}, nil
}

// SessionID returns the current session ID, or "" if no session has been
// started or joined.
func (s *Client) SessionID() string { return s.sid }

// Started reports whether NewSession or JoinSession has succeeded and the
// session has not been ended.
func (s *Client) Started() bool { return s.sid != "" }

// Debug reports whether HTTP request/response tracing is being written to
// LogWriter.
func (s *Client) Debug() bool { return s.debug }

// EnableDebug turns on HTTP request/response tracing to LogWriter.
func (s *Client) EnableDebug() { s.debug = true; s.rest.debug = true }

// DisableDebug turns off HTTP request/response tracing.
func (s *Client) DisableDebug() { s.debug = false; s.rest.debug = false }

// Timeout returns the current per-request timeout. Zero means no timeout.
func (s *Client) Timeout() time.Duration { return s.rest.timeout }

// SetTimeout updates the per-request timeout. A zero value disables
// timeouts (requests wait forever).
func (s *Client) SetTimeout(d time.Duration) {
	s.rest.timeout = d
	s.rest.client.Timeout = d
}

// -----------------------------------------------------------------------------
// Session lifecycle

// NewSession creates a new test session identified by userName and an
// optional sessionName (the server assigns one if empty). If killExisting
// is true and a session with the same name already exists, NewSession
// terminates it first and retries.
//
// On success it returns the session ID, formatted as "<sessionName> - <userName>".
//
// If this client already has an active session, NewSession is a no-op and
// returns ("", nil) — callers that need to know whether a new session was
// actually created should check Started() first, or compare against the
// returned ID.
func (s *Client) NewSession(userName, sessionName string, killExisting bool) (string, error) {
	if s.Started() {
		return "", nil
	}
	params := map[string]interface{}{
		"userid":      strings.TrimSpace(userName),
		"sessionname": strings.TrimSpace(sessionName),
	}

	_, data, err := s.rest.postRequest("sessions", "", params, "")
	if err != nil {
		var httpErr *RestHttpError
		if killExisting && errors.As(err, &httpErr) && strings.Contains(err.Error(), "already exists") {
			if _, endErr := s.EndSession(EndKill, sessionName+" - "+userName, 30*time.Second); endErr != nil {
				return "", fmt.Errorf("failed to create session: %w", endErr)
			}
			_, data, err = s.rest.postRequest("sessions", "", params, "")
			if err != nil {
				return "", fmt.Errorf("failed to create session: %w", err)
			}
		} else {
			return "", fmt.Errorf("failed to create session: %w", err)
		}
	}
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response body: %T", data)
	}
	sid, ok := m["session_id"].(string)
	if !ok || sid == "" {
		return "", fmt.Errorf("response missing session_id: %v", m)
	}

	s.rest.addHeader("X-STC-API-Session", sid)
	s.sid = sid
	return sid, nil
}

// JoinSession attaches to an existing session by ID (formatted as
// "<sessionName> - <userName>"). It returns the TestCenter
// version reported by the joined session, or an error if the session does
// not exist or cannot be accessed.
func (s *Client) JoinSession(sid string) (string, error) {
	s.rest.addHeader("X-STC-API-Session", sid)
	s.sid = sid
	_, data, err := s.rest.getRequest("objects", "system1", []string{"version", "name"}, "")
	if err != nil {
		s.rest.delHeader("X-STC-API-Session")
		s.sid = ""
		return "", fmt.Errorf("failed to join session %q: %w", sid, err)
	}
	m, _ := data.(map[string]interface{})
	ver, _ := m["version"].(string)
	return ver, nil
}

// EndSession terminates a test session. mode selects the disposition; see
// the EndMode constants. Pass "" for sid to target this client's current
// session.
//
// For EndDelete and EndKill, timeout bounds how long EndSession waits for
// the server to release the session: zero returns immediately after
// issuing the DELETE, a positive duration polls until the session is gone
// or the deadline is hit, and a negative duration waits indefinitely.
// timeout is ignored for EndDetach and EndRemove.
//
// Returns true if the session ended (or detached) successfully, false if
// no session was active. Returns an error if the server rejects the
// request or the wait times out.
func (s *Client) EndSession(mode EndMode, sid string, timeout time.Duration) (bool, error) {
	if sid == "" || sid == s.sid {
		if !s.Started() {
			return false, nil
		}
		sid = s.sid
		s.sid = ""
		s.rest.delHeader("X-STC-API-Session")
	}
	switch mode {
	case EndDetach:
		return true, nil
	case EndKill:
		if _, _, err := s.rest.deleteRequest("sessions", sid, []string{"kill"}, ""); err != nil {
			return false, fmt.Errorf("failed to end session: %w", err)
		}
	case EndDelete:
		if _, _, err := s.rest.deleteRequest("sessions", sid, nil, ""); err != nil {
			return false, fmt.Errorf("failed to end session: %w", err)
		}
	case EndRemove:
		if _, _, err := s.rest.deleteRequest("sessions", sid, []string{"false"}, ""); err != nil {
			return false, fmt.Errorf("failed to end session: %w", err)
		}
		return true, nil
	}

	if mode == EndKill || mode == EndDelete {
		if timeout == 0 {
			return true, nil
		}
		var deadline time.Time
		if timeout > 0 {
			deadline = time.Now().Add(timeout)
		}
		for {
			time.Sleep(5 * time.Second)
			list, err := s.Sessions()
			if err != nil {
				return false, err
			}
			found := false
			for _, id := range list {
				if id == sid {
					found = true
					break
				}
			}
			if !found {
				return true, nil
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return false, errors.New("timeout waiting for session to stop")
			}
		}
	}
	return true, nil
}

// Sessions returns the IDs of all active sessions registered on the server.
func (s *Client) Sessions() ([]string, error) {
	_, data, err := s.rest.getRequest("sessions", "", nil, "")
	if err != nil {
		return nil, err
	}
	return toStringSlice(data), nil
}

// SessionURLs returns one full URL per active session, in the same order
// as Sessions.
func (s *Client) SessionURLs() ([]string, error) {
	ids, err := s.Sessions()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.rest.makeURL("sessions", id, nil))
	}
	return out, nil
}

// SessionInfo returns server-side metadata for the given session.
// Pass "" for sessionID to query this client's current session; 
// if no session is active in that case, SessionInfo returns (nil, nil).
func (s *Client) SessionInfo(sessionID string) (map[string]interface{}, error) {
	if sessionID == "" {
		if !s.Started() {
			return nil, nil
		}
		sessionID = s.sid
	}
	_, data, err := s.rest.getRequest("sessions", sessionID, nil, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// -----------------------------------------------------------------------------
// System info

// BllVersion returns the BLL (back-end library) version for the current
// session, or "" if no session is started.
func (s *Client) BllVersion() (string, error) {
	if !s.Started() {
		return "", nil
	}
	_, data, err := s.rest.getRequest("objects", "system1", []string{"version", "name"}, "")
	if err != nil {
		return "", err
	}
	m, _ := data.(map[string]interface{})
	v, _ := m["version"].(string)
	return v, nil
}

// SystemInfo returns the server's /system payload (TestCenter version, API
// version, and related metadata).
func (s *Client) SystemInfo() (map[string]interface{}, error) {
	_, data, err := s.rest.getRequest("system", "", nil, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// ServerInfo returns the full attribute set of the system1 root object
// (hostname, version, children, etc.). Requires an active session.
func (s *Client) ServerInfo() (map[string]interface{}, error) {
	_, data, err := s.rest.getRequest("objects", "system1", nil, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// -----------------------------------------------------------------------------
// Core object API

// Apply pushes the current configuration to the connected chassis.
// Requires an active session. Returns an error if the server rejects the
// apply or chassis connectivity prevents it.
func (s *Client) Apply() error {
	if err := s.checkSession(); err != nil {
		return err
	}
	_, _, err := s.rest.putRequest("", "apply", nil, "")
	return err
}

// Get reads attributes or relations from an automation object identified
// by handle (e.g. "project1", "port1") or a DDN path.
//
// The concrete type of the returned interface{} depends on the number of
// attrs requested:
//
//   - none: map[string]interface{} containing every attribute on the object.
//   - one:  the bare value of that attribute (usually a string).
//   - many: map[string]interface{} keyed by attribute name.
//
// Callers must type-assert the return value to match what they requested.
// Returns an error if the handle is unknown or the request fails.
func (s *Client) Get(handle string, attrs ...string) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	var q interface{}
	if len(attrs) > 0 {
		q = attrs
	}
	_, data, err := s.rest.getRequest("objects", handle, q, "")
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Create creates a new automation object of the given objectType (e.g.
// "project", "port", "streamBlock") under the parent identified by under.
// Pass "" for under to create a top-level object. attributes is an
// optional set of initial attributes. Returns the handle of the new
// object.
func (s *Client) Create(objectType, under string, attributes map[string]interface{}) (string, error) {
	data, err := s.CreateX(objectType, under, attributes)
	if err != nil {
		return "", err
	}
	h, _ := data["handle"].(string)
	return h, nil
}

// CreateX is like Create but returns the full server response map (which
// always contains "handle" and may include extra server-assigned fields)
// instead of just the handle.
func (s *Client) CreateX(objectType, under string, attributes map[string]interface{}) (map[string]interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	params := map[string]interface{}{"object_type": objectType}
	if under != "" {
		params["under"] = under
	}
	for k, v := range attributes {
		params[k] = v
	}
	_, data, err := s.rest.postRequest("objects", "", params, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// Delete removes the automation object identified by handle.
func (s *Client) Delete(handle string) error {
	if err := s.checkSession(); err != nil {
		return err
	}
	_, _, err := s.rest.deleteRequest("objects", handle, nil, "")
	return err
}

// Perform executes the named TestCenter command (e.g. "GetObjectsCommand",
// "AttachPortsCommand") with the given params. The "command" key is injected
// automatically into params. Returns the server's response map including
// "status" and any command-specific output fields.
func (s *Client) Perform(command string, params map[string]interface{}) (map[string]interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	params["command"] = command
	_, data, err := s.rest.postRequest("perform", "", params, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// Config sets one or more attributes or relations on the object identified
// by handle. attributes is a map of attribute/relation name to value.
func (s *Client) Config(handle string, attributes map[string]interface{}) error {
	if err := s.checkSession(); err != nil {
		return err
	}
	_, _, err := s.rest.putRequest("objects", handle, attributes, "")
	return err
}

// -----------------------------------------------------------------------------
// Chassis / connections

// Chassis lists the chassis addresses known to this session (typically
// referenced via port locations).
func (s *Client) Chassis() ([]string, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.getRequest("chassis", "", nil, "")
	if err != nil {
		return nil, err
	}
	return toStringSlice(data), nil
}

// ChassisInfo returns detailed information (Hostname, Model, firmware
// version, module inventory, etc.) for the chassis at the given address
// (IP or hostname). chassis must be non-empty.
func (s *Client) ChassisInfo(chassis string) (map[string]interface{}, error) {
	if chassis == "" {
		return nil, errors.New("missing chassis address")
	}
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.getRequest("chassis", chassis, nil, "")
	if err != nil {
		return nil, err
	}
	m, _ := data.(map[string]interface{})
	return m, nil
}

// Connections returns a map of chassis address to connected flag, covering
// every chassis known to this session.
func (s *Client) Connections() (map[string]bool, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.getRequest("connections", "", nil, "")
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if m, ok := data.(map[string]interface{}); ok {
		for k, v := range m {
			b, _ := v.(bool)
			out[k] = b
		}
	}
	return out, nil
}

// IsConnected reports whether the chassis at the given address (IP or
// hostname) is currently connected to this session. A 404 "Connection Not
// Found" is treated as not-connected and returns (false, nil); any other
// transport or server error is returned as-is.
func (s *Client) IsConnected(chassis string) (bool, error) {
	if err := s.checkSession(); err != nil {
		return false, err
	}
	_, data, err := s.rest.getRequest("connections", chassis, nil, "")
	if err != nil {
		var httpErr *RestHttpError
		if errors.As(err, &httpErr) && httpErr.HTTPStatus == 404 {
			return false, nil
		}
		return false, err
	}
	if m, ok := data.(map[string]interface{}); ok {
		if b, ok := m["IsConnected"].(bool); ok {
			return b, nil
		}
	}
	return false, nil
}

// Connect establishes connections to one or more chassis. chassisList must
// be non-empty. A single entry is sent as PUT /connections/<chassis>; two
// or more are batched into POST /connections/ with action=connect. Returns
// the per-chassis connection records reported by the server.
func (s *Client) Connect(chassisList []string) ([]interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	if len(chassisList) == 1 {
		_, data, err := s.rest.putRequest("connections", chassisList[0], nil, "")
		if err != nil {
			return nil, err
		}
		return []interface{}{data}, nil
	}
	params := map[string]interface{}{"action": "connect"}
	for _, c := range chassisList {
		params[c] = true
	}
	_, data, err := s.rest.postRequest("connections", "", params, "")
	if err != nil {
		return nil, err
	}
	if arr, ok := data.([]interface{}); ok {
		return arr, nil
	}
	return []interface{}{data}, nil
}

// Disconnect drops connections to one or more chassis. chassisList must be
// non-empty. A single entry uses DELETE /connections/<chassis>; two or
// more are batched into POST /connections/ with action=disconnect.
func (s *Client) Disconnect(chassisList []string) error {
	if err := s.checkSession(); err != nil {
		return err
	}
	if len(chassisList) == 1 {
		_, _, err := s.rest.deleteRequest("connections", chassisList[0], nil, "")
		return err
	}
	params := map[string]interface{}{"action": "disconnect"}
	for _, c := range chassisList {
		params[c] = true
	}
	_, _, err := s.rest.postRequest("connections", "", params, "")
	return err
}

// ConnectAll connects every chassis referenced by this session (typically
// via port locations).
func (s *Client) ConnectAll() error {
	if err := s.checkSession(); err != nil {
		return err
	}
	_, _, err := s.rest.postRequest("connections", "", map[string]interface{}{"action": "connectall"}, "")
	return err
}

// DisconnectAll disconnects every chassis connected by this session.
func (s *Client) DisconnectAll() error {
	if err := s.checkSession(); err != nil {
		return err
	}
	_, _, err := s.rest.postRequest("connections", "", map[string]interface{}{"action": "disconnectall"}, "")
	return err
}

// -----------------------------------------------------------------------------
// Help / Log

// Help returns API documentation from the server. Pass "" for an overview,
// "commands" for the command list, or a command name, object type, or
// handle for specifics. args carries optional additional query terms.
// Object-specific help requires an active session.
func (s *Client) Help(subject string, args []string) (string, error) {
	if subject != "" {
		switch subject {
		case "commands", "create", "config", "get", "delete", "perform",
			"connect", "connectall", "disconnect", "disconnectall",
			"apply", "log", "help":
		default:
			if err := s.checkSession(); err != nil {
				return "", err
			}
		}
		var q interface{}
		if len(args) > 0 {
			q = args
		}
		_, data, err := s.rest.getRequest("help", subject, q, "")
		if err != nil {
			return "", err
		}
		return helpToString(data), nil
	}
	_, data, err := s.rest.getRequest("help", "", nil, "")
	if err != nil {
		return "", err
	}
	return helpToString(data), nil
}

// Log writes a diagnostic message to the TestCenter server log at the
// given level (one of "INFO", "WARN", "ERROR", "FATAL", case-insensitive).
func (s *Client) Log(level, msg string) error {
	if err := s.checkSession(); err != nil {
		return err
	}
	level = strings.ToUpper(level)
	switch level {
	case "INFO", "WARN", "ERROR", "FATAL":
	default:
		return errors.New("level must be one of: INFO, WARN, ERROR, FATAL")
	}
	_, _, err := s.rest.postRequest("log", "", map[string]interface{}{"log_level": level, "message": msg}, "")
	return err
}

// -----------------------------------------------------------------------------
// Files

// Files lists the files available for this session. The returned paths are
// relative to the session's working directory on the server (e.g.
// "bll.log", "diagnostics.tgz").
func (s *Client) Files() ([]string, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.getRequest("files", "", nil, "")
	if err != nil {
		return nil, err
	}
	return toStringSlice(data), nil
}

// FileURLs returns one full URL per file in this session, in the same
// order as Files.
func (s *Client) FileURLs() ([]string, error) {
	files, err := s.Files()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, s.rest.makeURL("files", f, nil))
	}
	return out, nil
}

// Download fetches the server-side file fileName and writes it locally.
// If saveAs is empty, the remote filename is used in the current working
// directory; otherwise saveAs is the destination path and any missing
// parent directories are created. Returns the local path written and the
// number of bytes received.
func (s *Client) Download(fileName, saveAs string) (string, int64, error) {
	if err := s.checkSession(); err != nil {
		return "", 0, err
	}
	if saveAs != "" {
		saveAs = filepath.Clean(saveAs)
		dir := filepath.Dir(saveAs)
		if dir != "" && dir != "." {
			if info, err := os.Stat(dir); err != nil {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", 0, err
				}
			} else if !info.IsDir() {
				return "", 0, fmt.Errorf("%s is not a directory", dir)
			}
		}
	}
	_, path, n, err := s.rest.downloadFile("files", fileName, saveAs, "application/octet-stream", nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to download %q: %w", fileName, err)
	}
	return path, n, nil
}

// DownloadAll downloads every file in this session into dstDir (the
// current working directory if empty). Returns a map of local path to
// bytes-written for each file saved. On partial failure the returned map
// contains the files downloaded before the error.
func (s *Client) DownloadAll(dstDir string) (map[string]int64, error) {
	files, err := s.Files()
	if err != nil {
		return nil, err
	}
	saved := make(map[string]int64, len(files))
	for _, f := range files {
		var saveAs string
		if dstDir != "" {
			parts := strings.Split(f, "/")
			saveAs = filepath.Join(dstDir, parts[len(parts)-1])
		}
		name, n, err := s.Download(f, saveAs)
		if err != nil {
			return saved, err
		}
		saved[name] = n
	}
	return saved, nil
}

// Upload sends the local file at srcPath to the server. If dstName is
// empty, the base name of srcPath is used as the remote filename. Returns
// the server's upload-metadata response.
func (s *Client) Upload(srcPath, dstName string) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.uploadFile("files", srcPath, dstName, true, "")
	return data, err
}

// -----------------------------------------------------------------------------
// Sequencer

// WaitUntilComplete polls the sequencer until it enters PAUSE or IDLE
// state, then returns its testState attribute (e.g. "PASSED", "FAILED").
// timeout bounds the wait; zero or negative disables the time limit
// (waits indefinitely).
func (s *Client) WaitUntilComplete(timeout time.Duration) (string, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	sys, err := s.Get("system1", "children-sequencer")
	if err != nil {
		return "", err
	}
	sequencer, _ := sys.(string)
	if sequencer == "" {
		return "", errors.New("could not locate sequencer")
	}
	for {
		res, err := s.Get(sequencer, "state")
		if err != nil {
			return "", err
		}
		state, _ := res.(string)
		if strings.Contains(state, "PAUSE") || strings.Contains(state, "IDLE") {
			break
		}
		time.Sleep(2 * time.Second)
		if !deadline.IsZero() && time.Now().After(deadline) {
			return "", fmt.Errorf("wait_until_complete timed out after %s", timeout)
		}
	}
	res, err := s.Get(sequencer, "testState")
	if err != nil {
		return "", err
	}
	v, _ := res.(string)
	return v, nil
}

// -----------------------------------------------------------------------------
// Bulk API

// BulkConfig applies attributes to every object matched by an STC
// XPath-like location expression (e.g. "port[@name^='p']"). The caller
// type-asserts the returned interface{} (typically map[string]interface{}).
func (s *Client) BulkConfig(locations string, attributes map[string]interface{}) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(attributes)
	if err != nil {
		return nil, err
	}
	_, data, err := s.rest.bulkPutRequest("bulk/objects", locations, body, "")
	return data, err
}

// BulkCreate creates a new object of the given objectType via the bulk
// endpoint. attributes may include "under" to specify the parent. The
// caller type-asserts the returned interface{}.
func (s *Client) BulkCreate(objectType string, attributes map[string]interface{}) (interface{}, error) {
	return s.bulkCreateEx(objectType, "", attributes, nil)
}

// BulkCreateList creates multiple objects of objectType under a shared
// parent (handle under) in one request. listAttrs holds the per-object
// attribute maps; under is injected into any item that doesn't supply
// its own "under". The caller type-asserts the returned interface{}.
func (s *Client) BulkCreateList(objectType, under string, listAttrs []map[string]interface{}) (interface{}, error) {
	return s.bulkCreateEx(objectType, under, nil, listAttrs)
}

// BulkCreateEx creates a single object under the parent identified by
// under (may be empty) via the bulk endpoint. attributes must carry
// "object_type" or any per-attribute type info the server requires. The
// caller type-asserts the returned interface{}.
func (s *Client) BulkCreateEx(under string, attributes map[string]interface{}) (interface{}, error) {
	return s.bulkCreateEx("", under, attributes, nil)
}

func (s *Client) bulkCreateEx(objectType, under string, attributes map[string]interface{}, list []map[string]interface{}) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	params := map[string]interface{}{}
	if objectType != "" {
		params["object_type"] = objectType
	}
	for k, v := range attributes {
		params[k] = v
	}
	if list != nil {
		// The server expects `under` per-item inside bulklist when a list
		// is supplied; a top-level `under` alongside bulklist triggers a
		// server-side "cannot access local variable 'parent'" error.
		if under != "" {
			for _, item := range list {
				if _, ok := item["under"]; !ok {
					item["under"] = under
				}
			}
		}
		params["bulklist"] = list
	} else if under != "" {
		params["under"] = under
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	_, data, err := s.rest.bulkPostRequest("bulk/objects", "", body, "")
	return data, err
}

// BulkGet reads attributes from every object matched by an STC location
// expression (e.g. "port[@name^='p']"). attrs lists the attributes to
// read; empty reads everything. depth controls how many levels of
// children to include (0 = just the matched objects). The caller
// type-asserts the returned interface{}.
func (s *Client) BulkGet(locations string, attrs []string, depth int) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	var q interface{}
	if len(attrs) > 0 {
		q = attrs
	}
	_, data, err := s.rest.bulkGetRequest("bulk/objects", locations, q, depth, "")
	return data, err
}

// BulkPerform executes the named command via the bulk endpoint. The
// "command" key is injected automatically into params. The caller
// type-asserts the returned interface{}.
func (s *Client) BulkPerform(command string, params map[string]interface{}) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	if params == nil {
		params = map[string]interface{}{}
	}
	params["command"] = command
	_, data, err := s.rest.postRequest("bulk/perform", "", params, "")
	return data, err
}

// BulkDelete deletes every object matched by an STC bulk location or
// handle expression (e.g. "port[@name^='bulk']"). The caller type-asserts
// the returned interface{}.
func (s *Client) BulkDelete(handles string) (interface{}, error) {
	if err := s.checkSession(); err != nil {
		return nil, err
	}
	_, data, err := s.rest.deleteRequest("bulk/objects", handles, nil, "")
	return data, err
}

// -----------------------------------------------------------------------------
// internals

func (s *Client) checkSession() error {
	if !s.Started() {
		return errors.New("must first join session")
	}
	return nil
}

func toStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		} else {
			out = append(out, fmt.Sprintf("%v", item))
		}
	}
	return out
}

func helpToString(data interface{}) string {
	switch v := data.(type) {
	case []interface{}:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(parts, " ")
	case map[string]interface{}:
		if m, ok := v["message"].(string); ok {
			return m
		}
		b, _ := json.Marshal(v)
		return string(b)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
