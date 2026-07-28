package onec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex/mcp-1c-data/internal/domain"
)

var (
	ErrUnauthorized = errors.New("1C rejected the user credentials")
	ErrForbidden    = errors.New("1C denied access for this user")
	ErrTooLarge     = errors.New("1C response exceeded the configured size limit")
	ErrUnavailable  = errors.New("1C endpoint is unavailable")
	ErrInvalidReply = errors.New("1C returned an invalid response")
)

type Client struct {
	baseURL  *url.URL
	username string
	password string
	maxBytes int64
	http     *http.Client
}

func NewClient(rawURL, username, password string, timeout time.Duration, maxBytes int64) (*Client, error) {
	baseURL, err := validateBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 || timeout > 30*time.Second {
		return nil, fmt.Errorf("timeout must be between 1ns and 30s")
	}
	if maxBytes <= 0 || maxBytes > domain.MaxResponseSize {
		return nil, fmt.Errorf("max response size must be between 1 and %d bytes", domain.MaxResponseSize)
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxConnsPerHost:       1,
		MaxIdleConnsPerHost:   1,
		ResponseHeaderTimeout: timeout,
		TLSHandshakeTimeout:   5 * time.Second,
	}
	client := &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		maxBytes: maxBytes,
	}
	client.http = &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, nil
}

func validateBaseURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("base URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("credentials in the base URL are forbidden")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("base URL must not contain a query or fragment")
	}
	host := parsed.Hostname()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback(host)) {
		return nil, fmt.Errorf("plain HTTP is allowed only for loopback addresses")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}
	parsed.Path = strings.TrimRight(parsed.EscapedPath(), "/") + "/"
	parsed.RawPath = ""
	return parsed, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) GetConfigurationInfo(ctx context.Context) (map[string]any, error) {
	var response map[string]any
	if err := c.do(ctx, http.MethodGet, "info", nil, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) GetMetadataCatalog(ctx context.Context) (domain.MetadataCatalog, error) {
	var response domain.MetadataCatalog
	err := c.do(ctx, http.MethodGet, "metadata", nil, &response)
	return response, err
}

func (c *Client) GetQuerySchema(ctx context.Context, request domain.SchemaRequest) (domain.SchemaResponse, error) {
	var response domain.SchemaResponse
	err := c.do(ctx, http.MethodPost, "schema", request, &response)
	return response, err
}

func (c *Client) ValidateQuery(ctx context.Context, request domain.ValidateRequest) (domain.ValidateResponse, error) {
	var response domain.ValidateResponse
	err := c.do(ctx, http.MethodPost, "validate-query", request, &response)
	return response, err
}

func (c *Client) ExecuteQuery(ctx context.Context, request domain.ExecuteRequest) (domain.QueryResult, error) {
	var response domain.QueryResult
	err := c.do(ctx, http.MethodPost, "query", request, &response)
	return response, err
}

func (c *Client) GetLatestDocuments(ctx context.Context, request domain.LatestDocumentsRequest) (domain.LatestDocumentsResult, error) {
	var response domain.LatestDocumentsResult
	method := http.MethodPost
	var body any = request
	if request.Responsible == nil && request.Posted == nil && request.Organization == nil && request.DateFrom == "" && request.DateTo == "" {
		method = http.MethodGet
		body = nil
	}
	err := c.do(ctx, method, "latest-documents", body, &response)
	return response, err
}

func (c *Client) GetDocumentTablePart(ctx context.Context, request domain.DocumentTablePartRequest) (domain.DocumentTablePartResult, error) {
	var response domain.DocumentTablePartResult
	err := c.do(ctx, http.MethodPost, "document-table-parts", request, &response)
	return response, err
}

func (c *Client) do(ctx context.Context, method, relative string, body any, output any) error {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: relative})
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if c.username != "" || c.password != "" {
		request.SetBasicAuth(c.username, c.password)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, c.maxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return ErrInvalidReply
	}
	if int64(len(raw)) > c.maxBytes {
		return ErrTooLarge
	}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope domain.ErrorEnvelope
		if json.Unmarshal(raw, &envelope) == nil && envelope.Error.Code != "" {
			return fmt.Errorf("%s: 1C rejected the request", sanitizeCode(envelope.Error.Code))
		}
		return ErrUnavailable
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return ErrInvalidReply
	}
	return nil
}

func sanitizeCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "ONEC_REQUEST_FAILED"
	}
	for _, r := range value {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return "ONEC_REQUEST_FAILED"
		}
	}
	return value
}
