// Package llm provides bounded, local-only Ollama adapters. It never pulls models.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

const (
	embeddingTimeout       = 20 * time.Second
	chatTimeout            = 35 * time.Second
	embeddingResponseLimit = 2 * 1024 * 1024
	chatResponseLimit      = 64 * 1024
)

type Client struct {
	baseURL string
	http    *http.Client
	slots   chan struct{}
}

// New validates a literal loopback endpoint without contacting the server.
// Hostnames are not accepted, avoiding DNS changes and proxy-based egress.
func New(baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Host == "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.Contains(baseURL, "#") {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, rag.NewError("INVALID_REQUEST")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	dialer := &net.Dialer{}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			ip, parseErr := netip.ParseAddr(host)
			if err != nil || parseErr != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() {
				return nil, errors.New("local endpoint required")
			}
			return dialer.DialContext(ctx, network, address)
		},
		DisableCompression:     true,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		MaxConnsPerHost:        2,
		IdleConnTimeout:        90 * time.Second,
		MaxResponseHeaderBytes: 16 * 1024,
	}
	return &Client{
		baseURL: strings.TrimSuffix(u.String(), "/"),
		http:    &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		slots:   make(chan struct{}, 2),
	}, nil
}

func (c *Client) Embedder() rag.Embedder   { return &embeddingClient{client: c} }
func (c *Client) Generator() rag.Generator { return &chatClient{client: c} }

func (c *Client) post(ctx context.Context, path string, payload any, timeout time.Duration, limit int64) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, rag.NewError("MODEL_TIMEOUT")
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		return nil, rag.NewError("RAG_BUSY")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, rag.NewError("MODEL_UPSTREAM_ERROR")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, requestError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return nil, rag.NewError("MODEL_RATE_LIMITED")
		case http.StatusNotFound, http.StatusServiceUnavailable:
			return nil, rag.NewError("MODEL_UNAVAILABLE")
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return nil, rag.NewError("MODEL_TIMEOUT")
		default:
			return nil, rag.NewError("MODEL_UPSTREAM_ERROR")
		}
	}
	response, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, requestError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, rag.NewError("MODEL_TIMEOUT")
	}
	if int64(len(response)) > limit {
		return nil, rag.NewError("MODEL_INVALID_RESPONSE")
	}
	return response, nil
}

func requestError(ctx context.Context, err error) error {
	var netErr net.Error
	if ctx.Err() != nil || errors.As(err, &netErr) && netErr.Timeout() {
		return rag.NewError("MODEL_TIMEOUT")
	}
	return rag.NewError("MODEL_UNAVAILABLE")
}

// decodeJSON rejects duplicate keys, invalid Unicode and trailing JSON. Outer
// Ollama envelopes may contain documented timing metadata; inner output has a
// separately enforced field allowlist.
func decodeJSON(body []byte, target any) error {
	if !validJSONUnicode(body) {
		return rag.NewError("MODEL_INVALID_RESPONSE")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 0); err != nil {
		return rag.NewError("MODEL_INVALID_RESPONSE")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return rag.NewError("MODEL_INVALID_RESPONSE")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return rag.NewError("MODEL_INVALID_RESPONSE")
	}
	return nil
}

func validateJSONValue(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("invalid JSON")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid JSON")
			}
			if _, duplicate := seen[name]; duplicate {
				return errors.New("invalid JSON")
			}
			seen[name] = struct{}{}
			if err := validateJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := validateJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON")
	}
	_, err = d.Token()
	return err
}

// encoding/json replaces malformed surrogate escapes; reject them instead.
func validJSONUnicode(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	quoted := false
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '"':
			quoted = !quoted
		case '\\':
			if !quoted {
				continue
			}
			if i+1 >= len(body) {
				return false
			}
			if body[i+1] != 'u' {
				i++
				continue
			}
			unit, ok := unicodeEscape(body, i)
			if !ok || unit >= 0xdc00 && unit <= 0xdfff {
				return false
			}
			i += 5
			if unit >= 0xd800 && unit <= 0xdbff {
				low, ok := unicodeEscape(body, i+1)
				if !ok || low < 0xdc00 || low > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

func unicodeEscape(body []byte, offset int) (uint16, bool) {
	if offset+6 > len(body) || body[offset] != '\\' || body[offset+1] != 'u' {
		return 0, false
	}
	v, err := strconv.ParseUint(string(body[offset+2:offset+6]), 16, 16)
	return uint16(v), err == nil
}
