package testcenterrestclient

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	cases := []struct {
		name                 string
		proto, server        string
		port                 int
		uri                  string
		want                 string
		wantErr              bool
	}{
		{"http default", "http", "host", 80, "stcapi", "http://host/stcapi/", false},
		{"https default", "https", "host", 443, "stcapi", "https://host/stcapi/", false},
		{"http explicit non-default port", "http", "host", 8080, "stcapi", "http://host:8080/stcapi/", false},
		{"trim slashes in uri", "http", "host", 80, "/stcapi/", "http://host/stcapi/", false},
		{"no uri", "http", "host", 80, "", "http://host/", false},
		{"bad proto", "ftp", "host", 80, "stcapi", "", true},
		{"bad port", "http", "host", 70000, "stcapi", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildURL(tc.proto, tc.server, tc.port, tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEscapePathPreservesSlashes(t *testing.T) {
	got := escapePath("results/foo bar.xml")
	want := "results/foo%20bar.xml"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeForm(t *testing.T) {
	got := encodeForm(map[string]interface{}{"a": 1, "b": "x y"})
	if !strings.Contains(got, "a=1") || !strings.Contains(got, "b=x+y") {
		t.Fatalf("unexpected form encoding: %s", got)
	}
}

func TestLowercaseRecursive(t *testing.T) {
	in := map[string]interface{}{
		"Outer": []interface{}{"HELLO", map[string]interface{}{"Nested": "WORLD"}},
	}
	out := lowercase(in).(map[string]interface{})
	if _, ok := out["outer"]; !ok {
		t.Fatalf("outer key not lowercased: %v", out)
	}
	arr := out["outer"].([]interface{})
	if arr[0] != "hello" {
		t.Fatalf("array element not lowercased: %v", arr[0])
	}
	inner := arr[1].(map[string]interface{})
	if inner["nested"] != "world" {
		t.Fatalf("nested value not lowercased: %v", inner)
	}
}

func TestRestHttpErrorMessage(t *testing.T) {
	e := &RestHttpError{HTTPStatus: 404, HTTPReason: "Not Found", Msg: "missing"}
	if got := e.Error(); got != "404 Not Found: missing" {
		t.Fatalf("unexpected: %q", got)
	}
	bare := &RestHttpError{HTTPStatus: 500, HTTPReason: "Internal Server Error"}
	if got := bare.Error(); got != "500 Internal Server Error" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestTokenExpiredUnwrapsToRestHttpError(t *testing.T) {
	te := &TokenExpiredError{RestHttpError: RestHttpError{HTTPStatus: 401, HTTPReason: "Unauthorized", Code: 4002}}
	var httpErr *RestHttpError
	if !errors.As(te, &httpErr) {
		t.Fatalf("errors.As should match *RestHttpError")
	}
	if httpErr.HTTPStatus != 401 || httpErr.Code != 4002 {
		t.Fatalf("unexpected unwrap: %+v", httpErr)
	}
}

func newTestRest(t *testing.T, base string) *restHttp {
	t.Helper()
	r, err := newRestHttp(base, restOptions{})
	if err != nil {
		t.Fatalf("newRestHttp: %v", err)
	}
	return r
}

func TestGetRequestOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/sessions/" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `["s1","s2"]`)
	}))
	defer srv.Close()

	r := newTestRest(t, srv.URL+"/")
	status, data, err := r.getRequest("sessions", "", nil, "")
	if err != nil {
		t.Fatalf("getRequest: %v", err)
	}
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	arr, ok := data.([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func TestHandle401WithCode4002ReturnsTokenExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"detail":"expired","code":4002}`)
	}))
	defer srv.Close()

	r := newTestRest(t, srv.URL+"/")
	_, _, err := r.getRequest("objects", "anything", nil, "")
	var te *TokenExpiredError
	if !errors.As(err, &te) {
		t.Fatalf("expected TokenExpiredError, got %T: %v", err, err)
	}
	if te.HTTPStatus != 401 || te.Code != 4002 {
		t.Fatalf("unexpected token-expired fields: %+v", te)
	}
}

func TestBeforeRequestHook(t *testing.T) {
	called := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	r := newTestRest(t, srv.URL+"/")
	r.beforeRequest = func() error { called++; return nil }
	if _, _, err := r.getRequest("sessions", "", nil, ""); err != nil {
		t.Fatalf("getRequest: %v", err)
	}
	if called != 1 {
		t.Fatalf("beforeRequest called %d times, want 1", called)
	}
}

func TestReactiveRefreshOnTokenExpired(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		attempt++
		w.Header().Set("Content-Type", "application/json")
		if attempt == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"code":4002,"detail":"expired"}`)
			return
		}
		io.WriteString(w, `["ok"]`)
	}))
	defer srv.Close()

	r := newTestRest(t, srv.URL+"/")
	refreshed := 0
	r.onTokenExpired = func() error { refreshed++; return nil }

	_, data, err := r.getRequest("sessions", "", nil, "")
	if err != nil {
		t.Fatalf("getRequest: %v", err)
	}
	if refreshed != 1 {
		t.Fatalf("onTokenExpired called %d times, want 1", refreshed)
	}
	arr, _ := data.([]interface{})
	if len(arr) != 1 || arr[0] != "ok" {
		t.Fatalf("unexpected retry result: %#v", data)
	}
}
