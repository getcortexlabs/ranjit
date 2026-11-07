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

func (c *Client) Status(ctx context.Context, connectionID string) (ConnectionStatus, error) {
	var out ConnectionStatus
	err := c.do(ctx, http.MethodGet, "/v1/connections/"+url.PathEscape(connectionID)+"/status", nil, &out)
	return out, err
}

type ImportOptions struct {
	Columns    map[string]string
	Delimiter  string
	CustomerID string
	Account    string
	Format     string
	Decimal    string
	DateFormat string
}

// Import uploads a local OFX/CSV export. Requests are limited to 8 MiB.
func (c *Client) Import(ctx context.Context, input io.Reader, opts ImportOptions) (ImportResult, error) {
	var out ImportResult
	err := c.upload(ctx, "/v1/imports", input, opts, &out)
	return out, err
}
func (c *Client) PreviewImport(ctx context.Context, input io.Reader, opts ImportOptions) (ImportPreview, error) {
	var out ImportPreview
	err := c.upload(ctx, "/v1/imports/preview", input, opts, &out)
	return out, err
}
func (c *Client) upload(ctx context.Context, path string, input io.Reader, opts ImportOptions, out any) error {
	data, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("statement exceeds 8 MiB")
	}
	q := url.Values{"customer_id": {opts.CustomerID}, "account": {opts.Account}, "format": {opts.Format}, "decimal": {opts.Decimal}, "date_format": {opts.DateFormat}, "delimiter": {opts.Delimiter}}
	for field, column := range opts.Columns {
		switch field {
		case "date", "description", "amount", "id":
			q.Set(field+"_column", column)
		default:
			return fmt.Errorf("unknown CSV field %q", field)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path+"?"+q.Encode(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	return c.send(req, out)
}
func (c *Client) Pause(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/connections/"+url.PathEscape(id)+"/pause", nil, nil)
}
func (c *Client) Renew(ctx context.Context, id string) (Connection, error) {
	var out Connection
	err := c.do(ctx, http.MethodPost, "/v1/connections/"+url.PathEscape(id)+"/renew", nil, &out)
	return out, err
}

type TransactionFilter struct {
	CustomerID, AccountID, Query, From, Through, Cursor string
	Limit                                               int
}

func (c *Client) SearchTransactions(ctx context.Context, f TransactionFilter) (Page[Transaction], error) {
	var out Page[Transaction]
	q := pageQuery(f.Cursor, f.Limit)
	q.Set("customer_id", f.CustomerID)
	q.Set("account_id", f.AccountID)
	q.Set("q", f.Query)
	q.Set("from", f.From)
	q.Set("through", f.Through)
	err := c.do(ctx, http.MethodGet, "/v1/transactions?"+q.Encode(), nil, &out)
	return out, err
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
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, output)
}

func (c *Client) send(req *http.Request, output any) error {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
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
