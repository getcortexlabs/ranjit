package ranjit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type WebhookEvent struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	CreatedAt time.Time   `json:"created_at"`
	Data      WebhookData `json:"data"`
}

type WebhookData struct {
	JobID        string `json:"job_id"`
	ConnectionID string `json:"connection_id"`
	CustomerID   string `json:"customer_id"`
	ErrorCode    string `json:"error_code,omitempty"`
}

type WebhookDelivery struct {
	Event         WebhookEvent `json:"event"`
	Status        string       `json:"status"`
	Attempts      int          `json:"attempts"`
	HTTPStatus    int          `json:"http_status,omitempty"`
	ErrorCode     string       `json:"error_code,omitempty"`
	UpdatedAt     time.Time    `json:"updated_at"`
	NextAttemptAt *time.Time   `json:"next_attempt_at,omitempty"`
}

// VerifyWebhook authenticates the unmodified request body and rejects signatures
// older than five minutes. The receiver must also deduplicate event IDs durably.
func VerifyWebhook(body []byte, signature, secret string, now time.Time) (WebhookEvent, error) {
	var event WebhookEvent
	invalid := errors.New("invalid webhook signature or event")
	if len(body) > 16<<10 || len(secret) < 32 {
		return event, invalid
	}
	parts := strings.Split(signature, ",")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
		return event, invalid
	}
	timestamp := strings.TrimPrefix(parts[0], "t=")
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || timestamp != strconv.FormatInt(unix, 10) || unix < now.Add(-5*time.Minute).Unix() || unix > now.Add(5*time.Minute).Unix() {
		return event, invalid
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(parts[1], "v1="))
	if err != nil {
		return event, invalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	if !hmac.Equal(digest, mac.Sum(nil)) {
		return event, invalid
	}
	if err := json.Unmarshal(body, &event); err != nil || event.ID == "" || event.Type == "" || event.CreatedAt.IsZero() || event.Data.JobID == "" || event.Data.ConnectionID == "" || event.Data.CustomerID == "" {
		return WebhookEvent{}, invalid
	}
	return event, nil
}

func (c *Client) WebhookDeliveries(ctx context.Context, cursor string, limit int) (Page[WebhookDelivery], error) {
	var out Page[WebhookDelivery]
	err := c.do(ctx, http.MethodGet, "/v1/webhooks/deliveries?"+pageQuery(cursor, limit).Encode(), nil, &out)
	return out, err
}

func (c *Client) RetryWebhook(ctx context.Context, eventID string) error {
	return c.do(ctx, http.MethodPost, "/v1/webhooks/deliveries/"+url.PathEscape(eventID)+"/retry", nil, nil)
}
