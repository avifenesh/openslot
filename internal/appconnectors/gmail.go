package appconnectors

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/mailer"
	"github.com/calnode/calnode/internal/secret"
	"golang.org/x/oauth2"
)

const GmailReadScope = "https://www.googleapis.com/auth/gmail.readonly"

const GmailSendScope = "https://www.googleapis.com/auth/gmail.send"

func (s *Service) GoogleConfig() *oauth2.Config {
	if s.google == nil {
		return nil
	}
	original := s.google()
	if original == nil {
		return nil
	}
	config := *original
	config.Scopes = []string{"openid", "email", GmailSendScope, GmailReadScope}
	return &config
}

func (s *Service) ExchangeGmail(ctx context.Context, userID, code, verifier string) error {
	if err := s.acquire(ctx, "gmail"); err != nil {
		return err
	}
	defer s.release("gmail")
	config := s.GoogleConfig()
	if config == nil {
		return errors.New("configure Google OAuth first")
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.client)
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return errors.New("Google authorization failed; reconnect Gmail")
	}
	if scope, ok := token.Extra("scope").(string); ok && scope != "" {
		granted := false
		read := false
		for _, v := range strings.Fields(scope) {
			if v == GmailReadScope {
				read = true
			}
			if v == GmailSendScope {
				granted = true
			}
		}
		if !granted || !read {
			return errors.New("Gmail read and send permissions must both be granted")
		}
	}
	if token.RefreshToken == "" {
		return errors.New("Google did not issue offline access; reconnect Gmail and approve access")
	}
	var identity struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Name     string `json:"name"`
	}
	if err = s.request(ctx, s.client, "GET", "https://openidconnect.googleapis.com/v1/userinfo", token.AccessToken, nil, &identity, false); err != nil {
		return err
	}
	if !identity.Verified || identity.Email == "" {
		return errors.New("Google did not verify the sending email address")
	}
	raw, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return s.save(ctx, "gmail", userID, string(raw), Config{Email: identity.Email, Name: identity.Name, InboxAccess: true})
}

func (s *Service) gmailClient(ctx context.Context, generation, raw string) (*oauth2.Token, error) {
	var token oauth2.Token
	if json.Unmarshal([]byte(raw), &token) != nil {
		return nil, errors.New("Gmail credentials are invalid; reconnect")
	}
	config := s.GoogleConfig()
	if config == nil {
		return nil, errors.New("Google OAuth is not configured")
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, s.client)
	refreshed, err := config.TokenSource(ctx, &token).Token()
	if err != nil {
		return nil, errors.New("Gmail access expired; reconnect the account")
	}
	if refreshed.AccessToken != token.AccessToken {
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = token.RefreshToken
		}
		encoded, err := json.Marshal(refreshed)
		if err != nil {
			return nil, err
		}
		encrypted, err := secret.Encrypt(s.key, string(encoded))
		if err != nil {
			return nil, err
		}
		result, err := s.db.ExecContext(ctx, "UPDATE app_integrations SET credentials_enc=? WHERE provider='gmail' AND generation=?", encrypted, generation)
		if err != nil {
			return nil, err
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			return nil, errors.New("Gmail connection changed; retry the request")
		}
	}
	return refreshed, nil
}

// Send selects connected Gmail explicitly. Provider failure never silently changes senders.
func (s *Service) Send(ctx context.Context, msg mailer.Message) error {
	if !s.gmailActive.Load() {
		if s.fallback != nil {
			return s.fallback.Send(ctx, msg)
		}
		return errors.New("no email sender is configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.acquire(ctx, "gmail"); err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			s.release("gmail")
		}
	}()
	generation, raw, config, err := s.load(ctx, "gmail")
	if errors.Is(err, sql.ErrNoRows) {
		s.release("gmail")
		locked = false
		if s.fallback != nil {
			return s.fallback.Send(ctx, msg)
		}
		return errors.New("no email sender is configured")
	}
	if err != nil {
		return err
	}
	token, err := s.gmailClient(ctx, generation, raw)
	if err == nil {
		var data []byte
		data, err = mailer.EncodeMIME(config.Email, "OpenSlot", msg)
		if err == nil {
			var result struct {
				ID string `json:"id"`
			}
			err = s.request(ctx, s.client, "POST", "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", token.AccessToken, map[string]string{"raw": base64.RawURLEncoding.EncodeToString(data)}, &result, false)
			if err == nil && result.ID == "" {
				err = errors.New("Gmail did not confirm message acceptance")
			}
		}
	}
	s.mark(ctx, "gmail", generation, err)
	return err
}

// CheckGmail refreshes credentials and verifies identity without sending email.
func (s *Service) CheckGmail(ctx context.Context) error {
	if err := s.acquire(ctx, "gmail"); err != nil {
		return err
	}
	defer s.release("gmail")
	generation, raw, _, err := s.load(ctx, "gmail")
	if err != nil {
		return errors.New("connect Gmail first")
	}
	token, err := s.gmailClient(ctx, generation, raw)
	if err == nil {
		var result struct {
			Email string `json:"email"`
		}
		err = s.request(ctx, s.client, "GET", "https://openidconnect.googleapis.com/v1/userinfo", token.AccessToken, nil, &result, false)
		if err == nil && result.Email == "" {
			err = errors.New("Google did not return the account identity")
		}
	}
	s.mark(ctx, "gmail", generation, err)
	return err
}
