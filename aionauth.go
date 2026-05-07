package testcenterrestclient

// AION authentication and stcapi endpoint discovery. 
//
// Platform interactions:
//   1. GET  /api/iam/organizations/default   -- discover org_id
//   2. POST /api/iam/oauth2/token            -- login (password grant)
//   3. POST /api/iam/oauth2/token            -- refresh (refresh_token grant)
//   4. GET  /api/inv/product-instances       -- discover stcapi host/port

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const refreshThreshold = 0.8

// AionError wraps failures from the AION IAM or inventory endpoints
// (login, refresh, organization lookup, product-instance discovery). It
// is returned by NewAionClient and by the AionClient's internal token
// refresh on the proactive/reactive code paths. Match it with errors.As
// to inspect the responding HTTP status.
type AionError struct {
	// Message describes the failure (HTTP status plus response body for
	// remote errors, or a static message for client-side validation).
	Message string
	// HTTPStatus is the HTTP status returned by AION, or 0 if the failure
	// occurred before a response (e.g. transport error, missing token).
	HTTPStatus int
}

func (e *AionError) Error() string { return e.Message }

type aionAuth struct {
	aionURL   string
	client    *http.Client
	debug     bool
	logWriter io.Writer

	accessToken     string
	refreshToken    string
	expiresIn       float64
	lastRefreshTime time.Time
}

func newAionAuth(aionURL, caCertFile string, debug bool, logWriter io.Writer) (*aionAuth, error) {
	if logWriter == nil {
		logWriter = os.Stdout
	}
	tlsCfg := &tls.Config{}
	if caCertFile != "" {
		pem, err := os.ReadFile(caCertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid ca cert: %s", caCertFile)
		}
		tlsCfg.RootCAs = pool
	}
	return &aionAuth{
		aionURL: strings.TrimRight(aionURL, "/"),
		client: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
			Timeout:   60 * time.Second,
		},
		debug:     debug,
		logWriter: logWriter,
		expiresIn: 86400,
	}, nil
}

func (a *aionAuth) getAccessToken() string { return a.accessToken }

func (a *aionAuth) getDefaultOrg() (string, error) {
	u := a.aionURL + "/api/iam/organizations/default"
	if a.debug {
		fmt.Fprintln(a.logWriter, "===> GET", u)
	}
	resp, err := a.client.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if a.debug {
		fmt.Fprintf(a.logWriter, "===> response status: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", &AionError{
			Message:    fmt.Sprintf("failed to get default org: %d %s", resp.StatusCode, string(body)),
			HTTPStatus: resp.StatusCode,
		}
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", err
	}
	id, _ := data["id"].(string)
	if a.debug {
		fmt.Fprintln(a.logWriter, "===> default org id:", id)
	}
	return id, nil
}

func (a *aionAuth) login(username, password, orgID string) error {
	if orgID == "" {
		id, err := a.getDefaultOrg()
		if err != nil {
			return err
		}
		orgID = id
	}
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", username)
	form.Set("password", password)
	form.Set("scope", orgID)
	if a.debug {
		fmt.Fprintln(a.logWriter, "===> POST", a.aionURL+"/api/iam/oauth2/token")
		fmt.Fprintln(a.logWriter, "  --- Params ---")
		fmt.Fprintf(a.logWriter, "    grant_type: password, username: %s, password: ******, scope: %s\n", username, orgID)
	}
	return a.tokenRequest(form, "login failed")
}

func (a *aionAuth) refresh() error {
	if a.refreshToken == "" {
		return &AionError{Message: "no refresh token available; call login first"}
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", a.refreshToken)
	if a.debug {
		fmt.Fprintln(a.logWriter, "===> POST", a.aionURL+"/api/iam/oauth2/token")
		fmt.Fprintln(a.logWriter, "  --- Params ---")
		fmt.Fprintln(a.logWriter, "    grant_type: refresh_token")
	}
	return a.tokenRequest(form, "token refresh failed")
}

func (a *aionAuth) tokenRequest(form url.Values, errPrefix string) error {
	u := a.aionURL + "/api/iam/oauth2/token"
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if a.debug {
		fmt.Fprintf(a.logWriter, "===> response status: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if resp.StatusCode != 200 {
		return &AionError{
			Message:    fmt.Sprintf("%s: %d %s", errPrefix, resp.StatusCode, string(body)),
			HTTPStatus: resp.StatusCode,
		}
	}
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	a.storeTokens(data)
	if a.debug {
		fmt.Fprintf(a.logWriter, "===> token stored, expires_in: %.0fs\n", a.expiresIn)
	}
	return nil
}

func (a *aionAuth) storeTokens(data map[string]interface{}) {
	if v, ok := data["access_token"].(string); ok {
		a.accessToken = v
	}
	if v, ok := data["refresh_token"].(string); ok {
		a.refreshToken = v
	}
	if v, ok := data["expires_in"].(float64); ok {
		a.expiresIn = v
	}
	a.lastRefreshTime = time.Now()
}

func (a *aionAuth) needsRefresh() bool {
	if a.accessToken == "" {
		return false
	}
	elapsed := time.Since(a.lastRefreshTime).Seconds()
	needed := elapsed >= a.expiresIn*refreshThreshold
	if needed && a.debug {
		fmt.Fprintf(a.logWriter, "===> proactive token refresh needed (elapsed: %.0fs, threshold: %.0fs)\n",
			elapsed, a.expiresIn*refreshThreshold)
	}
	return needed
}

type stcapiEndpoint struct {
	proto string
	host  string
	port  int
}

func (a *aionAuth) getStcapiEndpoint(nodeName string, uiPort int) (stcapiEndpoint, error) {
	if a.accessToken == "" {
		return stcapiEndpoint{}, &AionError{Message: "not logged in; call login first"}
	}
	u := a.aionURL + "/api/inv/product-instances"
	if a.debug {
		fmt.Fprintln(a.logWriter, "===> GET", u)
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+a.accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return stcapiEndpoint{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if a.debug {
		fmt.Fprintf(a.logWriter, "===> response status: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if resp.StatusCode != 200 {
		return stcapiEndpoint{}, &AionError{
			Message:    fmt.Sprintf("failed to get product instances: %d %s", resp.StatusCode, string(body)),
			HTTPStatus: resp.StatusCode,
		}
	}
	var instances []map[string]interface{}
	if err := json.Unmarshal(body, &instances); err != nil {
		return stcapiEndpoint{}, err
	}

	for _, inst := range instances {
		if nodeName != "" {
			node, _ := inst["node"].(map[string]interface{})
			name, _ := node["name"].(string)
			if name != nodeName {
				continue
			}
		}
		uiMatch := uiPort == 0
		stcapiURL := ""
		ports, _ := inst["ports"].([]interface{})
		for _, p := range ports {
			pm, _ := p.(map[string]interface{})
			http0, _ := pm["http"].(map[string]interface{})
			name, _ := pm["name"].(string)
			urlStr, _ := http0["url"].(string)
			if !uiMatch {
				hasUI, _ := http0["has_ui"].(bool)
				if hasUI && urlStr != "" {
					if parsed, err := url.Parse(urlStr); err == nil {
						if port := parsed.Port(); port != "" {
							if portEq(port, uiPort) {
								uiMatch = true
							}
						}
					}
				}
			}
			if strings.ToLower(name) == "stcapi" {
				stcapiURL = urlStr
			}
		}
		if !uiMatch || stcapiURL == "" {
			continue
		}
		parsed, err := url.Parse(stcapiURL)
		if err != nil {
			return stcapiEndpoint{}, &AionError{Message: "stcapi port entry has invalid url: " + stcapiURL}
		}
		portStr := parsed.Port()
		if parsed.Scheme == "" || parsed.Hostname() == "" || portStr == "" {
			return stcapiEndpoint{}, &AionError{Message: "stcapi port entry has invalid url: " + stcapiURL}
		}
		var port int
		fmt.Sscanf(portStr, "%d", &port)
		if a.debug {
			fmt.Fprintf(a.logWriter, "===> stcapi endpoint: %s://%s:%d\n", parsed.Scheme, parsed.Hostname(), port)
		}
		return stcapiEndpoint{proto: parsed.Scheme, host: parsed.Hostname(), port: port}, nil
	}

	switch {
	case nodeName != "" && uiPort != 0:
		return stcapiEndpoint{}, &AionError{Message: fmt.Sprintf("no stcapi port entry found for node_name %s, ui_port %d", nodeName, uiPort)}
	case nodeName != "":
		return stcapiEndpoint{}, &AionError{Message: fmt.Sprintf("no stcapi port entry found for node_name %s", nodeName)}
	case uiPort != 0:
		return stcapiEndpoint{}, &AionError{Message: fmt.Sprintf("no stcapi port entry found for ui_port %d", uiPort)}
	default:
		return stcapiEndpoint{}, &AionError{Message: "no stcapi port entry found in AION product instances"}
	}
}

func portEq(portStr string, want int) bool {
	var got int
	fmt.Sscanf(portStr, "%d", &got)
	return got == want
}
