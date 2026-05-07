package testcenterrestclient

// AionClient is an AION-authenticated TestCenter client. It embeds *Client
// and wires token-refresh hooks on the underlying restHttp directly.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// AionOptions configures a new AionClient.
type AionOptions struct {
	// AionURL is the AION platform base URL. Falls back to AION_URL.
	AionURL string
	// Username falls back to AION_USERNAME.
	Username string
	// Password falls back to AION_PASSWORD.
	Password string
	// NodeName optionally filters the inventory by AION node name.
	NodeName string
	// UIPort optionally filters the inventory by UI port (when multiple
	// product instances exist on the same node).
	UIPort int
	// ProductCACert verifies the stcapi HTTPS endpoint.
	ProductCACert string
	// AionCACert verifies the AION platform HTTPS endpoint.
	AionCACert string
	Debug      bool
	Timeout    time.Duration
	// LogWriter receives debug output when Debug is true. Nil defaults to
	// os.Stdout. Pass io.Discard to silence, or an *os.File to log to disk.
	LogWriter io.Writer
}

// AionClient is a TestCenter REST API client that authenticates against
// the AION platform. It embeds *Client (so all session and object methods
// are available) and adds transparent IAM login, stcapi endpoint
// discovery, and proactive/reactive bearer-token refresh.
type AionClient struct {
	*Client
	auth  *aionAuth
	debug bool
}

// NewAionClient constructs an AionClient. It logs in to AION using the
// credentials in opts (or the matching environment variables), discovers
// the stcapi endpoint from the product inventory, builds an underlying
// Client against that endpoint, and wires the refresh hooks so expired
// access tokens are renewed automatically. See AionOptions for the
// available fields.
//
// Returns an error if required configuration is missing, login fails, the
// stcapi endpoint cannot be discovered, or the underlying Client cannot
// be constructed.
func NewAionClient(opts AionOptions) (*AionClient, error) {
	if opts.AionURL == "" {
		opts.AionURL = os.Getenv("AION_URL")
	}
	if opts.Username == "" {
		opts.Username = os.Getenv("AION_USERNAME")
	}
	if opts.Password == "" {
		opts.Password = os.Getenv("AION_PASSWORD")
	}
	if opts.AionURL == "" {
		return nil, errors.New("AionURL is required (or set AION_URL)")
	}
	if opts.Username == "" {
		return nil, errors.New("Username is required (or set AION_USERNAME)")
	}
	if opts.Password == "" {
		return nil, errors.New("Password is required (or set AION_PASSWORD)")
	}

	lw := opts.LogWriter
	if lw == nil {
		lw = os.Stdout
	}

	auth, err := newAionAuth(opts.AionURL, opts.AionCACert, opts.Debug, lw)
	if err != nil {
		return nil, err
	}
	if err := auth.login(opts.Username, opts.Password, ""); err != nil {
		return nil, err
	}

	ep, err := auth.getStcapiEndpoint(opts.NodeName, opts.UIPort)
	if err != nil {
		return nil, err
	}

	client, err := NewClient(Options{
		Server:     ep.host,
		Port:       ep.port,
		UseHTTPS:   ep.proto == "https",
		CACertFile: opts.ProductCACert,
		Debug:      opts.Debug,
		Timeout:    opts.Timeout,
		Token:      auth.getAccessToken(),
		LogWriter:  lw,
	})
	if err != nil {
		return nil, err
	}

	a := &AionClient{Client: client, auth: auth, debug: opts.Debug}

	client.rest.beforeRequest = func() error {
		if auth.needsRefresh() {
			if opts.Debug {
				fmt.Fprintln(lw, "===> proactive token refresh triggered before request")
			}
			if err := auth.refresh(); err != nil {
				return err
			}
			client.rest.addHeader("Authorization", "Bearer "+auth.getAccessToken())
			if opts.Debug {
				fmt.Fprintln(lw, "===> proactive token refresh complete, retrying with new token")
			}
		}
		return nil
	}
	client.rest.onTokenExpired = func() error {
		if opts.Debug {
			fmt.Fprintln(lw, "===> token expired (401/4002), triggering reactive refresh")
		}
		if err := auth.refresh(); err != nil {
			return err
		}
		client.rest.addHeader("Authorization", "Bearer "+auth.getAccessToken())
		if opts.Debug {
			fmt.Fprintln(lw, "===> reactive token refresh complete, retrying request")
		}
		return nil
	}

	return a, nil
}

// AccessToken returns the bearer token currently used for stcapi
// requests. The token may have been refreshed since login, so callers
// should not cache the returned value across requests.
func (a *AionClient) AccessToken() string { return a.auth.getAccessToken() }
