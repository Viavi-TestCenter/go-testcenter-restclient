package testcenterrestclient

// Low-level REST HTTP wrapper.
//
// Exported surface: the three error types (RestHttpError, TokenExpiredError,
// ConnectionError). Everything else here is package-internal.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RestHttpError is returned when the server responds with a non-2xx
// status. Match it with errors.As to inspect the HTTP status and any
// application-level error code returned in the body.
type RestHttpError struct {
	// HTTPStatus is the HTTP status code (e.g. 400, 404, 500).
	HTTPStatus int
	// HTTPReason is the HTTP reason phrase (e.g. "Bad Request").
	HTTPReason string
	// Msg is the application-level error message extracted from the
	// response body, or "" if the body did not carry one.
	Msg string
	// Code is the application-level error code from the response body,
	// or 0 if absent. Code 4002 is hoisted into TokenExpiredError.
	Code int
}

func (e *RestHttpError) Error() string {
	base := fmt.Sprintf("%d %s", e.HTTPStatus, e.HTTPReason)
	if e.Msg != "" {
		return base + ": " + e.Msg
	}
	return base
}

// Status returns the HTTP status code (alias for the HTTPStatus field,
// useful when matching against an interface that exposes Status()).
func (e *RestHttpError) Status() int { return e.HTTPStatus }

// TokenExpiredError is returned on HTTP 401 with application code 4002
// (AUTH_TOKEN_INVALID). It embeds RestHttpError, so errors.As against
// *RestHttpError also matches. On AionClient the reactive-refresh hook
// handles this transparently - callers usually never see it.
type TokenExpiredError struct {
	RestHttpError
}

func (e *TokenExpiredError) Error() string { return e.RestHttpError.Error() }
func (e *TokenExpiredError) Unwrap() error { return &e.RestHttpError }

// ConnectionError wraps low-level transport failures (DNS, TCP, TLS).
type ConnectionError struct {
	// Msg describes the underlying transport failure.
	Msg string
	// Code is reserved for a future error-code mapping. Currently always -1.
	Code int
}

func (e *ConnectionError) Error() string { return e.Msg }

// restOptions configures a new restHttp.
type restOptions struct {
	User       string
	Password   string
	CACertFile string
	Debug      bool
	Timeout    time.Duration
	// LogWriter receives debug output when Debug is true. Nil defaults to
	// os.Stdout.
	LogWriter io.Writer
}

// restHttp is a lightweight HTTP client wrapper providing JSON-aware
// request helpers and optional auth-refresh hooks.
type restHttp struct {
	baseURL   string
	headers   map[string]string
	client    *http.Client
	debug     bool
	logWriter io.Writer
	timeout   time.Duration

	// beforeRequest, if set, runs before every request. A non-nil return
	// aborts the request.
	beforeRequest func() error
	// onTokenExpired, if set, runs when a request fails with
	// TokenExpiredError. A nil return triggers a single retry.
	onTokenExpired func() error
}

func newRestHttp(baseURL string, opts restOptions) (*restHttp, error) {
	lw := opts.LogWriter
	if lw == nil {
		lw = os.Stdout
	}
	r := &restHttp{
		baseURL:   strings.TrimRight(baseURL, "/"),
		headers:   map[string]string{"Accept": "application/json"},
		debug:     opts.Debug,
		logWriter: lw,
		timeout:   opts.Timeout,
	}

	tlsCfg := &tls.Config{}
	if opts.CACertFile != "" {
		pem, err := os.ReadFile(opts.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read ca cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("invalid ca cert: %s", opts.CACertFile)
		}
		tlsCfg.RootCAs = pool
	}
	r.client = &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
		Timeout:   opts.Timeout,
	}

	if opts.User != "" && opts.Password != "" {
		creds := opts.User + ":" + opts.Password
		r.headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
	}

	return r, nil
}

// buildURL constructs an absolute URL from the given components. A
// trailing slash is always present.
func buildURL(proto, server string, port int, uri string) (string, error) {
	if proto != "http" && proto != "https" {
		return "", fmt.Errorf("invalid proto: %s", proto)
	}
	var b strings.Builder
	b.WriteString(proto)
	b.WriteString("://")
	b.WriteString(server)
	if port != 0 {
		if port < 1 || port > 65535 {
			return "", errors.New("invalid port value")
		}
		if !((proto == "http" && port == 80) || (proto == "https" && port == 443)) {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(port))
		}
	}
	if uri != "" {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(strings.Trim(uri, "/")))
	}
	b.WriteByte('/')
	return b.String(), nil
}

func (r *restHttp) addHeader(name, value string) { r.headers[name] = value }
func (r *restHttp) delHeader(name string)        { delete(r.headers, name) }

// makeURL builds a request URL under baseURL.
//
// queryItems may be nil, a []string of bare attribute names (joined with
// '&'), or a map[string]string of key=value pairs.
func (r *restHttp) makeURL(container, resource string, queryItems interface{}) string {
	parts := []string{r.baseURL}
	if container != "" {
		parts = append(parts, strings.Trim(container, "/"))
	}
	if resource != "" {
		parts = append(parts, escapePath(resource))
	} else {
		parts = append(parts, "")
	}
	u := strings.Join(parts, "/")

	switch q := queryItems.(type) {
	case nil:
	case []string:
		if len(q) > 0 {
			u += "?" + strings.Join(q, "&")
		}
	case map[string]string:
		if len(q) > 0 {
			vals := url.Values{}
			keys := make([]string, 0, len(q))
			for k := range q {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				vals.Set(k, q[k])
			}
			u += "?" + vals.Encode()
		}
	}
	return u
}

func (r *restHttp) headRequest(container, resource string) (int, error) {
	if err := r.callBeforeRequest(); err != nil {
		return 0, err
	}
	u := r.makeURL(container, resource, nil)
	req, err := http.NewRequest(http.MethodHead, u, nil)
	if err != nil {
		return 0, err
	}
	r.applyHeaders(req, "")
	rsp, err := r.client.Do(req)
	if err != nil {
		return 0, connError(err)
	}
	defer rsp.Body.Close()
	if r.debug {
		r.printReq("HEAD", req.URL.String(), req.Header, nil)
	}
	return rsp.StatusCode, nil
}

func (r *restHttp) getRequest(container, resource string, queryItems interface{}, accept string) (int, interface{}, error) {
	return r.doJSON(http.MethodGet, container, resource, queryItems, nil, accept, false)
}

func (r *restHttp) postRequest(container, resource string, params map[string]interface{}, accept string) (int, interface{}, error) {
	return r.doJSON(http.MethodPost, container, resource, nil, params, accept, false)
}

func (r *restHttp) putRequest(container, resource string, params map[string]interface{}, accept string) (int, interface{}, error) {
	return r.doJSON(http.MethodPut, container, resource, nil, params, accept, false)
}

func (r *restHttp) deleteRequest(container, resource string, queryItems interface{}, accept string) (int, interface{}, error) {
	return r.doJSON(http.MethodDelete, container, resource, queryItems, nil, accept, false)
}

func (r *restHttp) bulkGetRequest(container, resource string, queryItems interface{}, depth int, accept string) (int, interface{}, error) {
	return r.withExtraHeader("X-STC-API-Children-Depth", strconv.Itoa(depth), func() (int, interface{}, error) {
		return r.doJSON(http.MethodGet, container, resource, queryItems, nil, accept, false)
	})
}

func (r *restHttp) bulkPostRequest(container, resource string, body []byte, accept string) (int, interface{}, error) {
	return r.doRaw(http.MethodPost, container, resource, nil, body, "application/json", accept)
}

func (r *restHttp) bulkPutRequest(container, resource string, body []byte, accept string) (int, interface{}, error) {
	return r.doRaw(http.MethodPut, container, resource, nil, body, "application/json", accept)
}

// downloadFile streams a file response to savePath. If savePath is
// empty, the last path element of resource is used.
func (r *restHttp) downloadFile(container, resource, savePath, accept string, queryItems interface{}) (int, string, int64, error) {
	resource = strings.ReplaceAll(resource, "\\", "/")
	u := r.makeURL(container, resource, queryItems)
	if savePath == "" {
		parts := strings.Split(resource, "/")
		savePath = parts[len(parts)-1]
	}

	if err := r.callBeforeRequest(); err != nil {
		return 0, "", 0, err
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, "", 0, err
	}
	r.applyHeaders(req, accept)

	rsp, err := r.client.Do(req)
	if err != nil {
		return 0, "", 0, connError(err)
	}
	defer rsp.Body.Close()
	if r.debug {
		r.printReq("GET", req.URL.String(), req.Header, nil)
	}

	if rsp.StatusCode >= 300 {
		body, _ := io.ReadAll(rsp.Body)
		return rsp.StatusCode, "", 0, &RestHttpError{
			HTTPStatus: rsp.StatusCode,
			HTTPReason: http.StatusText(rsp.StatusCode),
			Msg:        string(body),
		}
	}

	f, err := os.Create(savePath)
	if err != nil {
		return rsp.StatusCode, "", 0, fmt.Errorf("could not download file: %w", err)
	}
	defer f.Close()
	n, err := io.Copy(f, rsp.Body)
	if err != nil {
		return rsp.StatusCode, "", 0, fmt.Errorf("could not download file: %w", err)
	}
	return rsp.StatusCode, savePath, n, nil
}

func (r *restHttp) uploadFile(container, srcPath, dstName string, put bool, contentType string) (int, interface{}, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return 0, nil, fmt.Errorf("file not found: %s", srcPath)
	}
	if dstName == "" {
		dstName = filepath.Base(srcPath)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}

	method := http.MethodPost
	u := r.makeURL(container, "", nil)
	if put {
		method = http.MethodPut
		u = r.makeURL(container, dstName, nil)
	}

	if err := r.callBeforeRequest(); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(method, u, f)
	if err != nil {
		return 0, nil, err
	}
	r.applyHeaders(req, "")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Disposition", "attachment; filename="+dstName)
	req.ContentLength = st.Size()

	rsp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, connError(err)
	}
	defer rsp.Body.Close()
	if r.debug {
		r.printReq(method, req.URL.String(), req.Header, nil)
	}
	return r.handleResponse(rsp, false)
}

func (r *restHttp) uploadFileMP(container, srcPath, dstName, contentType string) (int, interface{}, error) {
	if _, err := os.Stat(srcPath); err != nil {
		return 0, nil, fmt.Errorf("file not found: %s", srcPath)
	}
	if dstName == "" {
		dstName = filepath.Base(srcPath)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return r.uploadMultipart(container, map[string]string{srcPath: dstName}, "file", contentType)
}

func (r *restHttp) uploadFiles(container string, srcDstMap map[string]string, contentType string) (int, interface{}, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return r.uploadMultipart(container, srcDstMap, "files", contentType)
}

func (r *restHttp) uploadMultipart(container string, srcDstMap map[string]string, fieldName, contentType string) (int, interface{}, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for src, dst := range srcDstMap {
		if dst == "" {
			dst = filepath.Base(src)
		}
		f, err := os.Open(src)
		if err != nil {
			return 0, nil, fmt.Errorf("file not found: %s", src)
		}
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, fieldName, dst)}
		h["Content-Type"] = []string{contentType}
		part, err := w.CreatePart(h)
		if err != nil {
			f.Close()
			return 0, nil, err
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return 0, nil, err
		}
		f.Close()
	}
	if err := w.Close(); err != nil {
		return 0, nil, err
	}

	u := r.makeURL(container, "", nil)
	if err := r.callBeforeRequest(); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, u, &buf)
	if err != nil {
		return 0, nil, err
	}
	r.applyHeaders(req, "")
	req.Header.Set("Content-Type", w.FormDataContentType())

	rsp, err := r.client.Do(req)
	if err != nil {
		return 0, nil, connError(err)
	}
	defer rsp.Body.Close()
	return r.handleResponse(rsp, false)
}

// -----------------------------------------------------------------------------
// internals

func (r *restHttp) applyHeaders(req *http.Request, accept string) {
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
}

func (r *restHttp) callBeforeRequest() error {
	if r.beforeRequest != nil {
		return r.beforeRequest()
	}
	return nil
}

func (r *restHttp) doJSON(method, container, resource string, queryItems interface{}, params map[string]interface{}, accept string, toLower bool) (int, interface{}, error) {
	attempt := func() (int, interface{}, error) {
		if err := r.callBeforeRequest(); err != nil {
			return 0, nil, err
		}
		u := r.makeURL(container, resource, queryItems)
		var body io.Reader
		var contentType string
		if params != nil {
			form := encodeForm(params)
			body = strings.NewReader(form)
			contentType = "application/x-www-form-urlencoded"
		}
		req, err := http.NewRequest(method, u, body)
		if err != nil {
			return 0, nil, err
		}
		r.applyHeaders(req, accept)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rsp, err := r.client.Do(req)
		if err != nil {
			return 0, nil, connError(err)
		}
		defer rsp.Body.Close()
		if r.debug {
			r.printReq(method, req.URL.String(), req.Header, params)
		}
		return r.handleResponse(rsp, toLower)
	}
	return r.withRetryOnTokenExpired(attempt)
}

func (r *restHttp) doRaw(method, container, resource string, queryItems interface{}, body []byte, contentType, accept string) (int, interface{}, error) {
	attempt := func() (int, interface{}, error) {
		if err := r.callBeforeRequest(); err != nil {
			return 0, nil, err
		}
		u := r.makeURL(container, resource, queryItems)
		req, err := http.NewRequest(method, u, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		r.applyHeaders(req, accept)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.ContentLength = int64(len(body))
		rsp, err := r.client.Do(req)
		if err != nil {
			return 0, nil, connError(err)
		}
		defer rsp.Body.Close()
		if r.debug {
			r.printReq(method, req.URL.String(), req.Header, nil)
		}
		return r.handleResponse(rsp, false)
	}
	return r.withRetryOnTokenExpired(attempt)
}

func (r *restHttp) withRetryOnTokenExpired(fn func() (int, interface{}, error)) (int, interface{}, error) {
	status, data, err := fn()
	if err == nil {
		return status, data, nil
	}
	var te *TokenExpiredError
	if !errors.As(err, &te) {
		return status, data, err
	}
	if r.onTokenExpired == nil {
		return status, data, err
	}
	if refreshErr := r.onTokenExpired(); refreshErr != nil {
		return status, data, refreshErr
	}
	return fn()
}

func (r *restHttp) withExtraHeader(name, value string, fn func() (int, interface{}, error)) (int, interface{}, error) {
	prev, had := r.headers[name]
	r.headers[name] = value
	defer func() {
		if had {
			r.headers[name] = prev
		} else {
			delete(r.headers, name)
		}
	}()
	return fn()
}

func (r *restHttp) handleResponse(rsp *http.Response, toLower bool) (int, interface{}, error) {
	if r.debug {
		fmt.Fprintf(r.logWriter, "===> response status: %d %s\n", rsp.StatusCode, http.StatusText(rsp.StatusCode))
	}

	var data interface{}
	if rsp.StatusCode != http.StatusNoContent {
		raw, err := io.ReadAll(rsp.Body)
		if err != nil {
			return rsp.StatusCode, nil, err
		}
		ct := rsp.Header.Get("Content-Type")
		if ct == "" || strings.HasPrefix(ct, "application/json") {
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &data); err != nil {
					data = raw
				}
			}
		} else {
			data = raw
		}
		if toLower {
			data = lowercase(data)
		}
		if r.debug {
			fmt.Fprintf(r.logWriter, "===> response content-type: %s\n", ct)
			fmt.Fprintf(r.logWriter, "===> DATA: %v\n", data)
		}
	}

	if rsp.StatusCode >= 300 {
		var msg string
		var code int
		if m, ok := data.(map[string]interface{}); ok {
			if v, ok := m["detail"].(string); ok {
				msg = v
			} else if v, ok := m["message"].(string); ok {
				msg = v
			} else if len(m) > 0 {
				msg = fmt.Sprintf("unknown error: %v", m)
			}
			if c, ok := m["code"].(float64); ok {
				code = int(c)
			}
		} else if data != nil {
			msg = fmt.Sprintf("unknown error: %v", data)
		}
		base := RestHttpError{
			HTTPStatus: rsp.StatusCode,
			HTTPReason: http.StatusText(rsp.StatusCode),
			Msg:        msg,
			Code:       code,
		}
		if rsp.StatusCode == http.StatusUnauthorized && code == 4002 {
			return rsp.StatusCode, data, &TokenExpiredError{RestHttpError: base}
		}
		return rsp.StatusCode, data, &base
	}
	return rsp.StatusCode, data, nil
}

func (r *restHttp) printReq(method, u string, headers http.Header, params map[string]interface{}) {
	fmt.Fprintf(r.logWriter, "===> %s %s\n", method, u)
	fmt.Fprintln(r.logWriter, "  --- Headers ---")
	for k, v := range headers {
		fmt.Fprintf(r.logWriter, "    %s: %s\n", k, strings.Join(v, ","))
	}
	if params != nil {
		fmt.Fprintln(r.logWriter, "  --- Params ---")
		redacted := map[string]interface{}{}
		for k, v := range params {
			if strings.EqualFold(k, "password") {
				redacted[k] = "******"
			} else {
				redacted[k] = v
			}
		}
		fmt.Fprintf(r.logWriter, "    %v\n", redacted)
	}
}

// escapePath percent-escapes unsafe path characters while preserving '/'
// so file-like resource paths (e.g. "results/foo.xml") stay intact.
func escapePath(s string) string {
	segs := strings.Split(s, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

func encodeForm(params map[string]interface{}) string {
	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, fmt.Sprintf("%v", v))
	}
	return vals.Encode()
}

func lowercase(v interface{}) interface{} {
	switch x := v.(type) {
	case string:
		return strings.ToLower(x)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[strings.ToLower(k)] = lowercase(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = lowercase(val)
		}
		return out
	default:
		return v
	}
}

func connError(err error) error {
	var nerr net.Error
	if errors.As(err, &nerr) {
		return &ConnectionError{Msg: err.Error(), Code: -1}
	}
	return &ConnectionError{Msg: err.Error(), Code: -1}
}
