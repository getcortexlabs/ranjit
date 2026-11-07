package ranjit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ranjit: %s (%d): %s", e.Code, e.StatusCode, e.Message)
}

func NewClient(baseURL, apiKey string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid Ranjit base URL")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Ranjit API key is required")
	}
	return &Client{baseURL: u.String(), apiKey: apiKey, HTTPClient: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Institutions(ctx context.Context) (Page[Institution], error) {
	var out Page[Institution]
	err := c.do(ctx, http.MethodGet, "/v1/institutions", nil, &out)
	return out, err
}

func (c *Client) Connect(ctx context.Context, input ConnectRequest) (ConnectResponse, error) {
	var out ConnectResponse
	err := c.do(ctx, http.MethodPost, "/v1/connections", input, &out)
	return out, err
}

func (c *Client) Connection(ctx context.Context, id string) (Connection, error) {
	var out Connection
	err := c.do(ctx, http.MethodGet, "/v1/connections/"+url.PathEscape(id), nil, &out)
	return out, err
}

func (c *Client) Connections(ctx context.Context, customer, cursor string, limit int) (Page[Connection], error) {
	var out Page[Connection]
	query := pageQuery(cursor, limit)
	query.Set("customer_id", customer)
	err := c.do(ctx, http.MethodGet, "/v1/connections?"+query.Encode(), nil, &out)
	return out, err
}

func (c *Client) Sync(ctx context.Context, connectionID string) (Job, error) {
	var out Job
	err := c.do(ctx, http.MethodPost, "/v1/connections/"+url.PathEscape(connectionID)+"/sync", nil, &out)
	return out, err
}

func (c *Client) Job(ctx context.Context, id string) (Job, error) {
	var out Job
	err := c.do(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(id), nil, &out)
	return out, err
}

func (c *Client) Accounts(ctx context.Context, connectionID, cursor string, limit int) (Page[Account], error) {
	var out Page[Account]
	err := c.do(ctx, http.MethodGet, "/v1/connections/"+url.PathEscape(connectionID)+"/accounts?"+pageQuery(cursor, limit).Encode(), nil, &out)
	return out, err
}

func (c *Client) Transactions(ctx context.Context, accountID, cursor string, limit int) (Page[Transaction], error) {
	var out Page[Transaction]
	err := c.do(ctx, http.MethodGet, "/v1/accounts/"+url.PathEscape(accountID)+"/transactions?"+pageQuery(cursor, limit).Encode(), nil, &out)
	return out, err
}

func (c *Client) Revoke(ctx context.Context, connectionID string) error {
	return c.do(ctx, http.MethodDelete, "/v1/connections/"+url.PathEscape(connectionID), nil, nil)
}

func pageQuery(cursor string, limit int) url.Values {
	v := url.Values{}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	if limit != 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return v
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var problem struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = decoder.Decode(&problem)
		return &APIError{StatusCode: resp.StatusCode, Code: problem.Error.Code, Message: problem.Error.Message, RequestID: resp.Header.Get("X-Request-ID")}
	}
	if output == nil {
		return nil
	}
	return decoder.Decode(output)
}
