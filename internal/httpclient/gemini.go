package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	geminiEndpoint         = "https://generativelanguage.googleapis.com/v1beta/interactions"
	geminiTimeout          = 12 * time.Second
	geminiMaxRequestBytes  = 64 << 10
	geminiMaxResponseBytes = 64 << 10
)

// PostGemini uses the shared network policy and host gate for a single authenticated
// request. Redirects and retries are disabled for this credentialed POST.
func (c *Client) PostGemini(ctx context.Context, apiKey string, body []byte) ([]byte, error) {
	if c == nil || c.http == nil || c.validator == nil {
		return nil, errors.New("HTTP client is not initialized")
	}
	if apiKey == "" {
		return nil, errors.New("Gemini API key is missing")
	}
	if len(body) == 0 || len(body) > geminiMaxRequestBytes {
		return nil, errors.New("Gemini request body outside size limit")
	}
	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create Gemini request: %w", err)
	}
	if err := c.validator.CheckURL(ctx, req.URL); err != nil {
		return nil, fmt.Errorf("validate Gemini URL: %w", err)
	}
	if err := c.wait(ctx, req.URL.Hostname()); err != nil {
		return nil, fmt.Errorf("wait for Gemini rate limit: %w", err)
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Revision", "2026-05-20")
	req.Header.Set("X-Goog-Api-Key", apiKey)
	noRedirect := *c.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		// Transport errors may include header values. Keep only the context error.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("send Gemini request: %w", ctx.Err())
		}
		return nil, errors.New("send Gemini request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini: %w: %d", ErrStatus, resp.StatusCode)
	}
	response, err := readResponse(resp, []string{"application/json"}, geminiMaxResponseBytes)
	switch {
	case err == nil:
		return response, nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("read Gemini response: %w", ctx.Err())
	case errors.Is(err, ErrInvalidMIME):
		return nil, ErrInvalidMIME
	case errors.Is(err, ErrTooLarge):
		return nil, ErrTooLarge
	default:
		return nil, errors.New("read Gemini response failed")
	}
}
