package testcenterrestclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// aionHandler returns a mux that implements the three AION endpoints
// needed by NewAionClient. inventoryURL is the stcapi URL advertised in the
// product-instances response.
func aionHandler(t *testing.T, loginCalls, refreshCalls *int, inventoryURL string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/iam/organizations/default", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "org"})
	})
	mux.HandleFunc("/api/iam/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.PostFormValue("grant_type") {
		case "password":
			*loginCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "at-1",
				"refresh_token": "rt-1",
				"expires_in":    float64(3600),
			})
		case "refresh_token":
			*refreshCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "at-2",
				"refresh_token": "rt-2",
				"expires_in":    float64(3600),
			})
		default:
			http.Error(w, "bad grant_type", http.StatusBadRequest)
		}
	})
	mux.HandleFunc("/api/inv/product-instances", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{
			"node": map[string]interface{}{"name": "node-1"},
			"ports": []interface{}{
				map[string]interface{}{
					"name": "StcApi",
					"http": map[string]interface{}{"url": inventoryURL, "has_ui": false},
				},
			},
		}})
	})
	return mux
}

func TestNewAionHappyPath(t *testing.T) {
	m := newMockSTC()
	stcSrv := m.server(t)
	defer stcSrv.Close()

	var loginCalls, refreshCalls int
	aionSrv := httptest.NewServer(aionHandler(t, &loginCalls, &refreshCalls, stcSrv.URL))
	defer aionSrv.Close()

	client, err := NewAionClient(AionOptions{
		AionURL:  aionSrv.URL,
		Username: "user",
		Password: "pw",
	})
	if err != nil {
		t.Fatalf("NewAionClient: %v", err)
	}
	if loginCalls != 1 {
		t.Fatalf("loginCalls=%d", loginCalls)
	}
	if refreshCalls != 0 {
		t.Fatalf("unexpected refreshCalls=%d", refreshCalls)
	}
	if client.AccessToken() != "at-1" {
		t.Fatalf("access token=%q", client.AccessToken())
	}
	if got := client.rest.headers["Authorization"]; got != "Bearer at-1" {
		t.Fatalf("Authorization header=%q", got)
	}
}

func TestNewAionReactiveRefreshOnTokenExpired(t *testing.T) {
	// STC mock: the /sessions probe in NewClient() must succeed so hooks
	// get wired. Subsequent request returns 401/4002 once, then 200.
	probeCount := 0
	stcCalls := 0
	stcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/stcapi/sessions/" && r.Method == http.MethodGet {
			probeCount++
			// First call is the probe from NewClient() — succeed.
			if probeCount == 1 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			// Subsequent calls: second one returns 401/4002.
			stcCalls++
			if stcCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":4002,"detail":"expired"}`))
				return
			}
			// After refresh, require new token.
			if got := r.Header.Get("Authorization"); got != "Bearer at-2" {
				t.Errorf("retry Authorization=%q, want Bearer at-2", got)
			}
			_, _ = w.Write([]byte(`["s1"]`))
		}
	}))
	defer stcSrv.Close()

	var loginCalls, refreshCalls int
	aionSrv := httptest.NewServer(aionHandler(t, &loginCalls, &refreshCalls, stcSrv.URL))
	defer aionSrv.Close()

	client, err := NewAionClient(AionOptions{
		AionURL:  aionSrv.URL,
		Username: "user",
		Password: "pw",
	})
	if err != nil {
		t.Fatalf("NewAionClient: %v", err)
	}

	ids, err := client.Sessions()
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("ids=%v", ids)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls=%d, want 1", refreshCalls)
	}
	if client.AccessToken() != "at-2" {
		t.Fatalf("access token=%q after refresh", client.AccessToken())
	}
}

func TestNewAionRequiresCredentials(t *testing.T) {
	t.Setenv("AION_URL", "")
	t.Setenv("AION_USERNAME", "")
	t.Setenv("AION_PASSWORD", "")

	if _, err := NewAionClient(AionOptions{Username: "u", Password: "p"}); err == nil {
		t.Fatal("expected error with empty AionURL")
	}
	if _, err := NewAionClient(AionOptions{AionURL: "http://x", Password: "p"}); err == nil {
		t.Fatal("expected error with empty Username")
	}
	if _, err := NewAionClient(AionOptions{AionURL: "http://x", Username: "u"}); err == nil {
		t.Fatal("expected error with empty Password")
	}
}

