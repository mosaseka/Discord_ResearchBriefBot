package discord

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiEndpoint = "https://discord.com/api/v10"

type Client struct {
	token, endpoint string
	http            *http.Client
	attempts        int
}
type response struct {
	ID string `json:"id"`
}
type rateBody struct {
	RetryAfter float64 `json:"retry_after"`
}
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Discord returned HTTP %d", e.Status) }

type ambiguousPostError struct{ cause error }

func (e *ambiguousPostError) Error() string {
	return "Discord delivery outcome is unknown: " + e.cause.Error()
}
func (e *ambiguousPostError) Unwrap() error { return e.cause }

func New(token string) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	h := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("Discord redirects are not followed") }}
	return &Client{token: token, endpoint: apiEndpoint, http: h, attempts: 4}
}
func NewForTest(token, endpoint string, h *http.Client) *Client {
	return &Client{token: token, endpoint: strings.TrimRight(endpoint, "/"), http: h, attempts: 4}
}

func (c *Client) Post(ctx context.Context, channel string, payload Payload) (messageID string, postErr error) {
	ambiguousAttempt := false
	defer func() {
		if postErr != nil && ambiguousAttempt {
			postErr = &ambiguousPostError{cause: postErr}
		}
	}()
	if c == nil || c.http == nil || strings.TrimSpace(c.token) == "" {
		return "", errors.New("Discord client is not configured")
	}
	if _, err := strconv.ParseUint(channel, 10, 64); err != nil {
		return "", errors.New("invalid Discord channel ID")
	}
	payload.AllowedMentions = noMentions()
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := c.endpoint + "/channels/" + url.PathEscape(channel) + "/messages"
	var last error
	for attempt := 0; attempt < c.attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bot "+c.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "DiscordResearchBriefBot/1.0")
		resp, err := c.http.Do(req)
		if err != nil {
			ambiguousAttempt = true
			last = fmt.Errorf("send Discord message: %w", err)
		} else {
			if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout {
				ambiguousAttempt = true
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if readErr != nil {
				return "", readErr
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var decoded response
				if err = json.Unmarshal(body, &decoded); err != nil {
					return "", fmt.Errorf("decode Discord response: %w", err)
				}
				if decoded.ID == "" {
					return "", errors.New("Discord returned an empty message ID")
				}
				return decoded.ID, nil
			}
			last = &HTTPError{Status: resp.StatusCode}
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return "", last
			}
			wait := time.Duration(1<<attempt) * 250 * time.Millisecond
			if resp.StatusCode == http.StatusTooManyRequests {
				var rate rateBody
				if json.Unmarshal(body, &rate) == nil && rate.RetryAfter >= 0 {
					wait = time.Duration(rate.RetryAfter * float64(time.Second))
				} else if seconds, e := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); e == nil {
					wait = time.Duration(seconds * float64(time.Second))
				}
			}
			if attempt+1 < c.attempts {
				if err = sleep(ctx, wait); err != nil {
					return "", err
				}
				continue
			}
		}
		if attempt+1 < c.attempts {
			if err = sleep(ctx, time.Duration(1<<attempt)*250*time.Millisecond); err != nil {
				return "", err
			}
		}
	}
	return "", last
}
func Nonce(channel, date, runID string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", channel, date, runID, index)))
	return hex.EncodeToString(sum[:12])
}
func IsAmbiguous(err error) bool {
	if err == nil {
		return false
	}
	var ambiguousErr *ambiguousPostError
	if errors.As(err, &ambiguousErr) {
		return true
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status >= 500 || httpErr.Status == http.StatusRequestTimeout
	}
	return true
}
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
