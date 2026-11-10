package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxRPCResponse = 64 << 20

var allowedMethods = map[string]bool{
	"getblockchaininfo": true, "getbestblockhash": true, "getblockhash": true,
	"getblockheader": true, "getrawtransaction": true, "gettxout": true,
	"scantxoutset": true, "getdescriptorinfo": true, "deriveaddresses": true,
	"getindexinfo": true, "getnetworkinfo": true,
}

type Config struct {
	URL         string
	CookieFile  string
	Username    string
	Password    string
	Timeout     time.Duration
	ScanTimeout time.Duration
}

type Client struct {
	baseURL     string
	cookieFile  string
	username    string
	password    string
	timeout     time.Duration
	scanTimeout time.Duration
	http        *http.Client
}

type RPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("Core RPC error %d: %s", e.Code, e.Message) }

func NewClient(cfg Config, httpClient *http.Client) (*Client, error) {
	if cfg.URL == "" {
		cfg.URL = "http://127.0.0.1:8332"
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Bitcoin RPC URL; use http(s)://host:port without embedded credentials")
	}
	if cfg.Username == "" && cfg.Password != "" || cfg.Username != "" && cfg.Password == "" {
		return nil, fmt.Errorf("both RPC username and password must be supplied together")
	}
	if cfg.CookieFile != "" && cfg.Username != "" {
		return nil, fmt.Errorf("choose cookie authentication or username/password, not both")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.ScanTimeout <= 0 {
		cfg.ScanTimeout = time.Hour
	}
	if httpClient == nil {
		httpClient = &http.Client{Transport: &http.Transport{
			MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
		}}
	}
	return &Client{baseURL: strings.TrimRight(cfg.URL, "/"), cookieFile: cfg.CookieFile,
		username: cfg.Username, password: cfg.Password, timeout: cfg.Timeout,
		scanTimeout: cfg.ScanTimeout, http: httpClient}, nil
}

func (c *Client) authenticate(req *http.Request) error {
	if c.cookieFile != "" {
		cookie, err := os.ReadFile(c.cookieFile)
		if err != nil {
			return fmt.Errorf("read Core RPC cookie: %w", err)
		}
		value := strings.TrimSpace(string(cookie))
		user, password, ok := strings.Cut(value, ":")
		if !ok || user == "" || password == "" {
			return fmt.Errorf("Core RPC cookie has invalid format")
		}
		req.SetBasicAuth(user, password)
	} else if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	return nil
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if !allowedMethods[method] {
		return fmt.Errorf("RPC method %q is not on the read-only allowlist", method)
	}
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "tracey-sof", "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}
	callCtx := ctx
	timeout := c.timeout
	if method == "scantxoutset" {
		timeout = c.scanTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.authenticate(req); err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Core RPC %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRPCResponse+1))
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}
	if len(body) > maxRPCResponse {
		return fmt.Errorf("%s response exceeds %d bytes", method, maxRPCResponse)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Core RPC %s returned HTTP %s; check that the node is running and credentials are correct", method, resp.Status)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
		ID     any             `json:"id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s: %w", method, envelope.Error)
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("%s response has no result", method)
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

func IsRPCCode(err error, code int64) bool {
	var rpcErr *RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == code
}
