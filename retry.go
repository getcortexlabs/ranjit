package ranjit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

const maxResponseBytes = 1 << 20

// RetryPolicy applies only to GET requests. Configure it before sharing a Client.
// The zero value makes one attempt, preserving the default client behavior.
type RetryPolicy struct {
	MaxAttempts    int
	MaxElapsedTime time.Duration
	BaseDelay      time.Duration
	MaxDelay       time.Duration
}

func (p RetryPolicy) normalized() (RetryPolicy, error) {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 1
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 10 || p.MaxElapsedTime < 0 || p.BaseDelay < 0 || p.MaxDelay < 0 {
		return p, ErrInvalid
	}
	if p.MaxElapsedTime == 0 {
		p.MaxElapsedTime = 30 * time.Second
	}
	if p.BaseDelay == 0 {
		p.BaseDelay = 200 * time.Millisecond
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = 2 * time.Second
	}
	if p.MaxElapsedTime > 5*time.Minute || p.MaxDelay > time.Minute || p.BaseDelay > p.MaxDelay {
		return p, ErrInvalid
	}
	return p, nil
}

func (c *Client) send(req *http.Request, output any) error {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	policy := RetryPolicy{MaxAttempts: 1}
	if req.Method == http.MethodGet {
		policy = c.ReadRetries
	}
	policy, err := policy.normalized()
	if err != nil {
		return err
	}
	if policy.MaxAttempts > 1 {
		ctx, cancel := context.WithTimeout(req.Context(), policy.MaxElapsedTime)
		defer cancel()
		req = req.Clone(ctx)
	}
	for attempt := 1; ; attempt++ {
		retryAfter, err := c.sendOnce(req, output)
		if err == nil || attempt >= policy.MaxAttempts {
			return err
		}
		if req.Context().Err() != nil {
			return req.Context().Err()
		}
		if !canRetryRead(err) {
			return err
		}
		delay := min(policy.BaseDelay*time.Duration(1<<min(attempt-1, 9)), policy.MaxDelay)
		delay = delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
		delay = max(delay, retryAfter)
		if deadline, ok := req.Context().Deadline(); ok && time.Until(deadline) <= delay {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return req.Context().Err()
		case <-timer.C:
		}
	}
}

func (c *Client) sendOnce(req *http.Request, output any) (time.Duration, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	problem := &APIError{StatusCode: resp.StatusCode, RequestID: resp.Header.Get("X-Request-ID")}
	if err != nil {
		problem.Code, problem.Message, problem.Cause = "response_read_failed", "response could not be read", err
		return retryAfter, problem
	}
	if len(data) > maxResponseBytes {
		problem.Code, problem.Message, problem.Cause = "response_too_large", "response exceeds 1 MiB", ErrResponseTooLarge
		return 0, problem
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil {
			problem.Code, problem.Message = envelope.Error.Code, envelope.Error.Message
		}
		if problem.Message == "" {
			problem.Message = http.StatusText(resp.StatusCode)
		}
		return retryAfter, problem
	}
	if output == nil {
		return 0, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err = decoder.Decode(output); err != nil || decoder.Decode(new(any)) != io.EOF {
		problem.Code, problem.Message, problem.Cause = "invalid_response", "response is not a single JSON value", ErrInvalidResponse
		return 0, problem
	}
	return 0, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		// Values beyond the maximum read budget must not overflow into an early retry.
		return time.Duration(min(seconds, 86400)) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, date.Sub(now))
	}
	return 0
}

func canRetryRead(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrResponseTooLarge) || errors.Is(err, ErrInvalidResponse) {
		return false
	}
	var api *APIError
	if errors.As(err, &api) {
		switch api.StatusCode {
		case 408, 429, 500, 502, 503, 504:
			return true
		}
		return transientTransport(api.Cause)
	}
	return transientTransport(err)
}

func transientTransport(err error) bool {
	if err == nil {
		return false
	}
	var network net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) || errors.As(err, &network) && (network.Timeout() || network.Temporary())
}

func (e *APIError) Unwrap() error { return e.Cause }
func (e *APIError) Is(target error) bool {
	if e.StatusCode == 401 && target == ErrUnauthorized || e.StatusCode == 429 && target == ErrRateLimited {
		return true
	}
	codes := map[string]error{"not_found": ErrNotFound, "inactive_connection": ErrConflict, "invalid_request": ErrInvalid, "invalid_authorization": ErrConsent, "provider_credentials": ErrProviderCredentials, "provider_unavailable": ErrProviderUnavailable, "idempotency_conflict": ErrIdempotency, "webhook_not_retryable": ErrWebhookState}
	return codes[e.Code] != nil && codes[e.Code] == target
}
