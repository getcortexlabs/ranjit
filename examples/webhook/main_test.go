package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInboxAcknowledgesOnlyDurableUniqueEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.db")
	db, err := openInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secret := strings.Repeat("s", 40)
	body := []byte(fmt.Sprintf(`{"id":"evt-test","type":"sync.succeeded","created_at":%q,"data":{"job_id":"job-test","connection_id":"con-test","customer_id":"customer"}}`, time.Now().UTC().Format(time.RFC3339)))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	signature := "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	deliver := func(signature string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/webhooks/ranjit", bytes.NewReader(body))
		req.Header.Set("Ranjit-Signature", signature)
		w := httptest.NewRecorder()
		receiver(db, secret).ServeHTTP(w, req)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	deliver("invalid", 401)
	deliver(signature, 204)
	db.Close()
	db, err = openInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deliver(signature, 204)
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM webhook_inbox`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	db.Close()
	deliver(signature, 503)
}
