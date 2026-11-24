package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPostGemini(t *testing.T) {
	const key = "test-placeholder"
	for _, tc := range []struct {
		name       string
		status     int
		mime, body string
		wantErr    error
	}{
		{name: "JSON", status: 200, mime: "application/json; charset=utf-8", body: "{}"},
		{name: "no retries on 429", status: 429, body: "untrusted response marker", wantErr: ErrStatus},
		{name: "no retries on 503", status: 503, wantErr: ErrStatus},
		{name: "no redirects", status: 307, wantErr: ErrStatus},
		{name: "wrong MIME", status: 200, mime: "text/html", wantErr: ErrInvalidMIME},
		{name: "oversize response", status: 200, mime: "application/json", body: strings.Repeat("a", geminiMaxResponseBytes+1), wantErr: ErrTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validations, calls := 0, 0
			validator := ValidatorFunc(func(_ context.Context, _ *url.URL) error {
				validations++
				return nil
			})
			h := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.String() != "https://generativelanguage.googleapis.com/v1beta/interactions" ||
					r.Header.Get("X-Goog-Api-Key") != key || r.Header.Get("Content-Type") != "application/json" ||
					r.Header.Get("Api-Revision") != "2026-05-20" {
					t.Fatal("unexpected Gemini request")
				}
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "{}" {
					t.Fatal("unexpected request body")
				}
				header := make(http.Header)
				header.Set("Content-Type", tc.mime)
				header.Set("Location", "https://example.org/redirect")
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(tc.body)), ContentLength: -1}, nil
			})}
			got, err := NewForTest(h, validator, 3).PostGemini(context.Background(), key, []byte("{}"))
			if !errors.Is(err, tc.wantErr) || calls != 1 || validations != 1 {
				t.Fatalf("calls=%d validations=%d error=%v", calls, validations, err)
			}
			if err == nil && string(got) != tc.body {
				t.Fatal("unexpected response")
			}
			if err != nil && (strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "untrusted response marker")) {
				t.Fatal("error exposes request or response values")
			}
		})
	}
}

func TestPostGeminiLimitsAndTimeout(t *testing.T) {
	calls := 0
	h := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		defer r.Body.Close()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	client := NewForTest(h, nil, 3)
	for _, body := range [][]byte{nil, make([]byte, geminiMaxRequestBytes+1)} {
		if _, err := client.PostGemini(context.Background(), "test-placeholder", body); err == nil {
			t.Fatal("expected body size error")
		}
	}
	if _, err := client.PostGemini(context.Background(), "", []byte("{}")); err == nil || calls != 0 {
		t.Fatal("invalid input reached Gemini transport")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.PostGemini(ctx, "test-placeholder", []byte("{}")); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
