package appconnectors

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"strconv"
	"strings"

	"github.com/calnode/calnode/internal/mailer"
	"golang.org/x/net/html"
)

// InboxMessage contains only message text and routing headers, never attachments.
type InboxMessage struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id"`
	From       string `json:"from"`
	Subject    string `json:"subject"`
	MessageID  string `json:"message_id"`
	Body       string `json:"body"`
	ReceivedAt int64  `json:"received_at"`
}
type gmailPart struct {
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Headers []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Parts []gmailPart `json:"parts"`
}

func (p gmailPart) header(name string) string {
	var values []string
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			values = append(values, h.Value)
		}
	}
	return strings.Join(values, ", ")
}
func (p gmailPart) text(kind string) string {
	if p.Filename != "" {
		return ""
	}
	if p.MimeType == kind {
		b, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
		if e == nil {
			return string(b)
		}
	}
	var parts []string
	for _, child := range p.Parts {
		if s := child.text(kind); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}
func addressed(headers, address string) bool {
	list, err := mail.ParseAddressList(headers)
	if err != nil {
		return false
	}
	for _, a := range list {
		if strings.EqualFold(a.Address, address) {
			return true
		}
	}
	return false
}
func plainHTML(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var out strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out.String()
		case html.TextToken:
			out.Write(z.Text())
			out.WriteByte(' ')
		}
	}
}

// PollInbox pages all matching mail. The caller advances its watermark only after
// every page completes; durable message IDs make a retried scan harmless.
func (s *Service) PollInbox(ctx context.Context, address string, after int64, known func(string) (bool, error), accept func(string, InboxMessage) error) error {
	if err := s.acquire(ctx, "gmail"); err != nil {
		return err
	}
	defer s.release("gmail")
	generation, raw, cfg, err := s.load(ctx, "gmail")
	if err != nil {
		return errors.New("connect Gmail first")
	}
	if !cfg.InboxAccess {
		return errors.New("reconnect Gmail to grant inbox read access")
	}
	if !strings.EqualFold(address, cfg.Email) {
		return errors.New("the dedicated address must be the connected Gmail mailbox")
	}
	token, err := s.gmailClient(ctx, generation, raw)
	if err != nil {
		return err
	}
	page := ""
	for {
		q := url.Values{"q": {fmt.Sprintf("after:%d {to:%s cc:%s} -in:spam -in:trash -in:sent -in:drafts", after-1, address, address)}, "maxResults": {"100"}}
		if page != "" {
			q.Set("pageToken", page)
		}
		var list struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			Next string `json:"nextPageToken"`
		}
		if err = s.request(ctx, s.client, "GET", "https://gmail.googleapis.com/gmail/v1/users/me/messages?"+q.Encode(), token.AccessToken, nil, &list, false); err != nil {
			return err
		}
		for _, item := range list.Messages {
			if known != nil {
				seen, e := known(item.ID)
				if e != nil {
					return e
				}
				if seen {
					continue
				}
			}
			var m struct {
				ID       string    `json:"id"`
				ThreadID string    `json:"threadId"`
				Date     string    `json:"internalDate"`
				Labels   []string  `json:"labelIds"`
				Payload  gmailPart `json:"payload"`
			}
			if err = s.request(ctx, s.client, "GET", "https://gmail.googleapis.com/gmail/v1/users/me/messages/"+url.PathEscape(item.ID)+"?format=full", token.AccessToken, nil, &m, false); err != nil {
				if errors.Is(err, providerHTTPError(404)) || errors.Is(err, errInvalidResponse) {
					continue
				}
				return err
			}
			skip := false
			for _, l := range m.Labels {
				if l == "SENT" || l == "DRAFT" || l == "SPAM" || l == "TRASH" {
					skip = true
				}
			}
			at, _ := strconv.ParseInt(m.Date, 10, 64)
			p := m.Payload
			if skip || at < after*1000 || (!addressed(p.header("To"), address) && !addressed(p.header("Cc"), address)) {
				continue
			}
			if a := p.header("Auto-Submitted"); a != "" && !strings.EqualFold(a, "no") {
				continue
			}
			if p.header("List-Id") != "" || p.header("Precedence") != "" {
				continue
			}
			sender, e := mail.ParseAddress(p.header("From"))
			if e != nil || strings.EqualFold(sender.Address, address) {
				continue
			}
			body := p.text("text/plain")
			if body == "" {
				body = plainHTML(p.text("text/html"))
			}
			// Bound model input independently of MIME attachment sizes. Reject truncation
			// rather than hiding the end of a scheduling request from the agent.
			if len(body) > 64000 {
				continue
			}
			mid := p.header("Message-ID")
			if !validMessageID(mid) || m.ID == "" || m.ThreadID == "" {
				continue
			}
			if err = accept(generation, InboxMessage{ID: m.ID, ThreadID: m.ThreadID, From: sender.Address, Subject: p.header("Subject"), MessageID: mid, Body: body, ReceivedAt: at}); err != nil {
				return err
			}
		}
		if list.Next == "" {
			break
		}
		if list.Next == page {
			return errors.New("Gmail returned a repeated page token")
		}
		page = list.Next
	}
	s.mark(ctx, "gmail", generation, nil)
	return nil
}
func validMessageID(v string) bool {
	return len(v) > 2 && len(v) < 998 && strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">") && !strings.ContainsAny(v, "\r\n \t")
}

// SendSlotReply never accepts model-selected recipients or free-form headers.
// The durable caller marks sending before this call and never retries an
// ambiguous Gmail response, preventing duplicate offers after a crash.
func (s *Service) SendSlotReply(ctx context.Context, generation string, in InboxMessage, body string) error {
	if err := s.acquire(ctx, "gmail"); err != nil {
		return err
	}
	defer s.release("gmail")
	current, raw, cfg, err := s.load(ctx, "gmail")
	if err != nil || current != generation {
		return errors.New("Gmail connection changed")
	}
	if !validMessageID(in.MessageID) {
		return errors.New("invalid original message ID")
	}
	token, err := s.gmailClient(ctx, current, raw)
	if err != nil {
		return err
	}
	subject := in.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	data, err := mailer.EncodeMIME(cfg.Email, "OpenSlot", mailer.Message{To: []string{in.From}, Subject: subject, Text: body})
	if err != nil {
		return err
	}
	// Headers are validated above. The original ID provides both RFC threading headers.
	prefix := "In-Reply-To: " + in.MessageID + "\r\nReferences: " + in.MessageID + "\r\nAuto-Submitted: auto-replied\r\n"
	data = append([]byte(prefix), data...)
	var result struct {
		ID string `json:"id"`
	}
	err = s.request(ctx, s.client, "POST", "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", token.AccessToken, map[string]string{"raw": base64.RawURLEncoding.EncodeToString(data), "threadId": in.ThreadID}, &result, false)
	if err == nil && result.ID == "" {
		err = io.ErrUnexpectedEOF
	}
	s.mark(ctx, "gmail", generation, err)
	return err
}
