package appconnectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGmailInboxSkipsPoisonMessagesAndReadsPaddedBodies(t *testing.T) {
	s, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			io.WriteString(w, `{"access_token":"test","refresh_token":"refresh","expires_in":3600}`)
		case "/v1/userinfo":
			io.WriteString(w, `{"email":"host@example.test","email_verified":true}`)
		case "/gmail/v1/users/me/messages":
			if r.URL.Query().Get("pageToken") == "next" {
				io.WriteString(w, `{"messages":[{"id":"good"}]}`)
			} else {
				io.WriteString(w, `{"messages":[{"id":"deleted"},{"id":"big"}],"nextPageToken":"next"}`)
			}
		case "/gmail/v1/users/me/messages/deleted":
			w.WriteHeader(404)
		case "/gmail/v1/users/me/messages/big":
			io.WriteString(w, `{"payload":{"body":{"data":"`+strings.Repeat("a", 3<<20)+`"}}}`)
		case "/gmail/v1/users/me/messages/good":
			json.NewEncoder(w).Encode(map[string]any{"id": "good", "threadId": "thread", "internalDate": fmt.Sprint(time.Now().UnixMilli()), "payload": map[string]any{"mimeType": "text/plain", "body": map[string]string{"data": base64.URLEncoding.EncodeToString([]byte("Offer slots please."))}, "headers": []map[string]string{{"name": "From", "value": "guest@example.test"}, {"name": "To", "value": "Host <host@example.test>"}, {"name": "Message-ID", "value": "<test@example.test>"}}}})
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	if e := s.ExchangeGmail(context.Background(), "host", "code", "verifier"); e != nil {
		t.Fatal(e)
	}
	count := 0
	e := s.PollInbox(context.Background(), "host@example.test", time.Now().Add(-time.Minute).Unix(), nil, func(_ string, m InboxMessage) error {
		count++
		if m.Body != "Offer slots please." {
			t.Errorf("decoded %q", m.Body)
		}
		return nil
	})
	if e != nil || count != 1 {
		t.Fatal("poison message blocked later page", count, e)
	}
}
