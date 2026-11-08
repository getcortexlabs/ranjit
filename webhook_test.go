package ranjit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyWebhookRejectsTamperingAndReplay(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	secret := strings.Repeat("s", 40)
	body, _ := json.Marshal(WebhookEvent{ID: "evt-test", Type: "sync.succeeded", CreatedAt: now, Data: WebhookData{JobID: "job-test", ConnectionID: "con-test", CustomerID: "customer"}})
	sign := func(body []byte, ts string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	}
	signature := sign(body, strconv.FormatInt(now.Unix(), 10))
	if _, err := VerifyWebhook(body, signature, secret, now); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name              string
		body              []byte
		signature, secret string
		now               time.Time
	}{
		{"altered body", append(append([]byte{}, body...), ' '), signature, secret, now},
		{"wrong secret", body, signature, strings.Repeat("x", 40), now},
		{"old", body, signature, secret, now.Add(5*time.Minute + time.Second)},
		{"future", body, signature, secret, now.Add(-5*time.Minute - time.Second)},
		{"duplicate", body, signature + ",v1=bad", secret, now},
		{"bad timestamp", body, sign(body, "not-a-time"), secret, now},
		{"overflow", body, sign(body, "9223372036854775807"), secret, now},
		{"oversized", []byte(strings.Repeat(" ", 16385)), signature, secret, now},
		{"empty event", []byte(`{}`), sign([]byte(`{}`), strconv.FormatInt(now.Unix(), 10)), secret, now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := VerifyWebhook(tt.body, tt.signature, tt.secret, tt.now); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
