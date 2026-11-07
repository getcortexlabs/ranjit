package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getcortexlabs/ranjit"
)

const MaxResponseBytes = 2 << 20

// OAuth owns the token cache for exactly one installed bank application.
// The request function retains each bank's transport and pacing policy.
type OAuth struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}
type OAuthOptions struct {
	TokenURL, ClientID, ClientSecret, StaticToken string
	Scopes                                        []string
	OptionalScope                                 bool
	JSONBasic                                     bool
	Now                                           func() time.Time
	Request                                       func(*http.Request, any) (int, error)
}

func (a *OAuth) accessToken(ctx context.Context, o OAuthOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if o.StaticToken != "" {
		return o.StaticToken, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && o.Now().Before(a.expiresAt) {
		return a.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {o.ClientID}}
	if o.ClientSecret != "" {
		form.Set("client_secret", o.ClientSecret)
	}
	if len(o.Scopes) > 0 {
		form.Set("scope", strings.Join(o.Scopes, " "))
	}
	body := form.Encode()
	contentType := "application/x-www-form-urlencoded"
	if o.JSONBasic {
		body = `{"grant_type":"client_credentials"}`
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(body))
	if err != nil {
		return "", ranjit.ErrProviderUnavailable
	}
	req.Header.Set("Content-Type", contentType)
	if o.JSONBasic {
		req.SetBasicAuth(o.ClientID, o.ClientSecret)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	started := o.Now()
	status, err := o.Request(req, &result)
	if status == 401 || status == 403 {
		return "", ranjit.ErrProviderCredentials
	}
	if err != nil {
		return "", err
	}
	if result.AccessToken == "" || len(result.AccessToken) > 8192 || strings.ContainsAny(result.AccessToken, " \t\r\n") || !strings.EqualFold(result.TokenType, "Bearer") || result.ExpiresIn <= 0 || result.ExpiresIn > 86400 {
		return "", ranjit.ErrProviderUnavailable
	}
	if !o.OptionalScope || result.Scope != "" {
		for _, required := range o.Scopes {
			found := false
			for _, scope := range strings.Fields(result.Scope) {
				if scope == required {
					found = true
				}
			}
			if !found {
				return "", ranjit.ErrProviderCredentials
			}
		}
	}
	lifetime := time.Duration(result.ExpiresIn) * time.Second
	a.token = result.AccessToken
	a.expiresAt = started.Add(lifetime - min(30*time.Second, lifetime/10))
	return a.token, nil
}

func (a *OAuth) Get(ctx context.Context, o OAuthOptions, endpoint string, headers http.Header, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	checkpoint, _ := ctx.Value(checkpointKey{}).(Checkpoint)
	if checkpoint != nil {
		body, err := checkpoint.Load(ctx, endpoint)
		if err != nil {
			return err
		}
		if body != nil {
			if json.Unmarshal(body, result) != nil {
				return ranjit.ErrInvalid
			}
			return nil
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := a.accessToken(ctx, o)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return ranjit.ErrProviderUnavailable
		}
		for name, values := range headers {
			req.Header[name] = append([]string(nil), values...)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		var raw json.RawMessage
		status, err := o.Request(req, &raw)
		if status == 401 && o.StaticToken == "" {
			a.mu.Lock()
			if a.token == token {
				a.token = ""
				a.expiresAt = time.Time{}
			}
			a.mu.Unlock()
			if attempt == 0 {
				continue
			}
		}
		if status == 401 || status == 403 {
			return ranjit.ErrProviderCredentials
		}
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, result) != nil {
			return fmt.Errorf("%w: malformed bank response", ranjit.ErrInvalid)
		}
		if checkpoint != nil {
			return checkpoint.Save(ctx, endpoint, raw)
		}
		return nil
	}
	return ranjit.ErrProviderCredentials
}

func RequestJSON(client *http.Client, req *http.Request, result any, now time.Time) (int, error) {
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return 0, req.Context().Err()
		}
		return 0, ranjit.ErrProviderUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		delay := time.Minute
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
			delay = time.Duration(min(seconds, 300)) * time.Second
		} else if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && at.After(now) {
			delay = min(at.Sub(now), 5*time.Minute)
		}
		return resp.StatusCode, &RetryError{After: delay}
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("%w: bank HTTP %d", ranjit.ErrProviderUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(body) > MaxResponseBytes {
		return resp.StatusCode, ranjit.ErrProviderUnavailable
	}
	if json.Unmarshal(body, result) != nil {
		return resp.StatusCode, fmt.Errorf("%w: malformed bank response", ranjit.ErrInvalid)
	}
	return resp.StatusCode, nil
}
