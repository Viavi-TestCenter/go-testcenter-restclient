package testcenterrestclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newAION(t *testing.T, handler http.Handler) (*aionAuth, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	a, err := newAionAuth(srv.URL, "", false, nil)
	if err != nil {
		t.Fatalf("newAionAuth: %v", err)
	}
	return a, srv
}

func TestGetDefaultOrg(t *testing.T) {
	a, _ := newAION(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/iam/organizations/default" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "org-123"})
	}))

	id, err := a.getDefaultOrg()
	if err != nil {
		t.Fatalf("getDefaultOrg: %v", err)
	}
	if id != "org-123" {
		t.Fatalf("id=%q", id)
	}
}

func TestLoginAndRefresh(t *testing.T) {
	loginCalls := 0
	refreshCalls := 0
	a, _ := newAION(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/iam/organizations/default":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "org-x"})
		case "/api/iam/oauth2/token":
			_ = r.ParseForm()
			switch r.PostFormValue("grant_type") {
			case "password":
				loginCalls++
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  "at-1",
					"refresh_token": "rt-1",
					"expires_in":    float64(3600),
				})
			case "refresh_token":
				refreshCalls++
				if got := r.PostFormValue("refresh_token"); got != "rt-1" {
					t.Errorf("refresh_token=%q", got)
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token":  "at-2",
					"refresh_token": "rt-2",
					"expires_in":    float64(3600),
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))

	if err := a.login("user@example.com", "pw", ""); err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginCalls != 1 {
		t.Fatalf("loginCalls=%d", loginCalls)
	}
	if a.getAccessToken() != "at-1" {
		t.Fatalf("access_token=%q", a.getAccessToken())
	}

	if err := a.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls=%d", refreshCalls)
	}
	if a.getAccessToken() != "at-2" {
		t.Fatalf("access_token=%q after refresh", a.getAccessToken())
	}
	if a.refreshToken != "rt-2" {
		t.Fatalf("refreshToken=%q after refresh", a.refreshToken)
	}
}

func TestLoginFailure(t *testing.T) {
	a, _ := newAION(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/iam/organizations/default":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "org"})
		case "/api/iam/oauth2/token":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		}
	}))

	err := a.login("user", "bad", "")
	if err == nil {
		t.Fatal("expected error")
	}
	ae, ok := err.(*AionError)
	if !ok {
		t.Fatalf("expected *AionError, got %T", err)
	}
	if ae.HTTPStatus != 401 {
		t.Fatalf("HTTPStatus=%d", ae.HTTPStatus)
	}
}

func TestRefreshWithoutLoginFails(t *testing.T) {
	a, err := newAionAuth("http://unreachable.invalid", "", false, nil)
	if err != nil {
		t.Fatalf("newAionAuth: %v", err)
	}
	if err := a.refresh(); err == nil {
		t.Fatal("expected error when refreshing without login")
	}
}

func TestNeedsRefresh(t *testing.T) {
	a := &aionAuth{accessToken: "at", expiresIn: 100}

	// Fresh (0s elapsed) → no refresh needed.
	a.lastRefreshTime = time.Now()
	if a.needsRefresh() {
		t.Fatal("needsRefresh=true at 0s elapsed")
	}

	// 85% of 100s elapsed → needs refresh.
	a.lastRefreshTime = time.Now().Add(-85 * time.Second)
	if !a.needsRefresh() {
		t.Fatal("needsRefresh=false at 85% elapsed")
	}

	// No access token → never needs refresh.
	a.accessToken = ""
	if a.needsRefresh() {
		t.Fatal("needsRefresh=true with empty access token")
	}
}

func TestGetStcapiEndpointRequiresLogin(t *testing.T) {
	a, err := newAionAuth("http://example.invalid", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.getStcapiEndpoint("", 0)
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
}

func TestGetStcapiEndpointParsesInventory(t *testing.T) {
	a, _ := newAION(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/inv/product-instances" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at-test" {
			t.Errorf("Authorization=%q", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"node": map[string]interface{}{"name": "node-A"},
				"ports": []interface{}{
					map[string]interface{}{
						"name": "StcApi",
						"http": map[string]interface{}{
							"url":    "https://client.internal:9443",
							"has_ui": false,
						},
					},
				},
			},
		})
	}))
	a.accessToken = "at-test"

	ep, err := a.getStcapiEndpoint("node-A", 0)
	if err != nil {
		t.Fatalf("getStcapiEndpoint: %v", err)
	}
	if ep.proto != "https" || ep.host != "client.internal" || ep.port != 9443 {
		t.Fatalf("endpoint=%+v", ep)
	}
}

func TestAionErrorMessage(t *testing.T) {
	e := &AionError{Message: "oops", HTTPStatus: 500}
	if e.Error() != "oops" {
		t.Fatalf("unexpected: %q", e.Error())
	}
}
