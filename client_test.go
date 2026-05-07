package testcenterrestclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// mockSTC is a minimal STC REST API mock.
type mockSTC struct {
	sessions map[string]bool
	requests []*http.Request
}

func newMockSTC() *mockSTC {
	return &mockSTC{sessions: map[string]bool{}}
}

func (m *mockSTC) server(t *testing.T) *httptest.Server {
	t.Helper()
	h := http.NewServeMux()

	// GET /stcapi/sessions/ — list session ids
	h.HandleFunc("/stcapi/sessions/", func(w http.ResponseWriter, r *http.Request) {
		m.requests = append(m.requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			ids := make([]string, 0, len(m.sessions))
			for id := range m.sessions {
				ids = append(ids, id)
			}
			_ = json.NewEncoder(w).Encode(ids)
		case http.MethodPost:
			_ = r.ParseForm()
			user := r.PostFormValue("userid")
			name := r.PostFormValue("sessionname")
			sid := name + " - " + user
			m.sessions[sid] = true
			_ = json.NewEncoder(w).Encode(map[string]string{"session_id": sid})
		}
	})

	// /stcapi/objects/... — Get/Create/Delete
	h.HandleFunc("/stcapi/objects/", func(w http.ResponseWriter, r *http.Request) {
		m.requests = append(m.requests, r)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = r.ParseForm()
			ot := r.PostFormValue("object_type")
			_ = json.NewEncoder(w).Encode(map[string]string{"handle": ot + "1"})
		case http.MethodGet:
			handle := strings.TrimPrefix(r.URL.Path, "/stcapi/objects/")
			handle = strings.TrimSuffix(handle, "/")
			_ = json.NewEncoder(w).Encode(map[string]string{"handle": handle, "name": "n1"})
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	// /stcapi/perform/ — Perform
	h.HandleFunc("/stcapi/perform/", func(w http.ResponseWriter, r *http.Request) {
		m.requests = append(m.requests, r)
		w.Header().Set("Content-Type", "application/json")
		_ = r.ParseForm()
		cmd := r.PostFormValue("command")
		_ = json.NewEncoder(w).Encode(map[string]string{"command": cmd, "status": "OK"})
	})

	// /stcapi/system/ — SystemInfo
	h.HandleFunc("/stcapi/system/", func(w http.ResponseWriter, r *http.Request) {
		m.requests = append(m.requests, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"stc_version": "5.0.0",
			"api_version": "2.3.0",
		})
	})

	return httptest.NewServer(h)
}

func newClientAgainstMock(t *testing.T, srvURL string) *Client {
	t.Helper()
	u, _ := url.Parse(srvURL)
	port, _ := strconv.Atoi(u.Port())
	client, err := NewClient(Options{Server: u.Hostname(), Port: port})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestNewConnectsAndListsSessions(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	ids, err := client.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty session list, got %v", ids)
	}
}

func TestNewSessionSetsSessionHeader(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	sid, err := client.NewSession("alice", "demo", false)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sid != "demo - alice" {
		t.Fatalf("unexpected sid %q", sid)
	}
	if !client.Started() {
		t.Fatal("Started()=false after NewSession")
	}
	if got := client.rest.headers["X-STC-API-Session"]; got != sid {
		t.Fatalf("session header=%q, want %q", got, sid)
	}
}

func TestCreateAndGet(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	if _, err := client.NewSession("u", "s", false); err != nil {
		t.Fatal(err)
	}

	h, err := client.Create("project", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h != "project1" {
		t.Fatalf("handle=%q, want project1", h)
	}

	got, err := client.Get(h)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	gotMap, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("Get(handle) returned %T, want map", got)
	}
	if gotMap["handle"] != "project1" {
		t.Fatalf("Get returned %v", got)
	}
}

func TestPerform(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	if _, err := client.NewSession("u", "s", false); err != nil {
		t.Fatal(err)
	}
	out, err := client.Perform("ApplyCommand", nil)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if out["command"] != "ApplyCommand" || out["status"] != "OK" {
		t.Fatalf("unexpected: %v", out)
	}
}

func TestEndSessionDetachIsLocal(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	if _, err := client.NewSession("u", "s", false); err != nil {
		t.Fatal(err)
	}
	ok, err := client.EndSession(EndDetach, "", 0)
	if err != nil || !ok {
		t.Fatalf("EndSession(EndDetach) = %v, %v", ok, err)
	}
	if client.Started() {
		t.Fatal("Started() still true after detach")
	}
	if _, has := client.rest.headers["X-STC-API-Session"]; has {
		t.Fatal("session header not cleared on detach")
	}
}

func TestEndSessionDeleteHitsServer(t *testing.T) {
	m := newMockSTC()
	srv := m.server(t)
	defer srv.Close()

	client := newClientAgainstMock(t, srv.URL)
	sid, err := client.NewSession("u", "s", false)
	if err != nil {
		t.Fatal(err)
	}

	// zero timeout → don't poll for session-gone; just verify DELETE was sent.
	if _, err := client.EndSession(EndDelete, "", 0); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	found := false
	for _, r := range m.requests {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, sid) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no DELETE /stcapi/sessions/%s/ recorded", sid)
	}
}

func TestNewRequiresServer(t *testing.T) {
	// Clear TC_SERVER_ADDRESS so the env fallback doesn't mask the error.
	t.Setenv("TC_SERVER_ADDRESS", "")
	_, err := NewClient(Options{})
	if err == nil {
		t.Fatal("expected error when Server empty")
	}
}
