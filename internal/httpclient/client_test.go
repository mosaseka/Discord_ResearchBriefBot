package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMIMEOversizeAndRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client := NewForTest(server.Client(), nil, 2)
	body, err := client.Fetch(context.Background(), server.URL, "application/json", []string{"application/json"}, 100)
	if err != nil || string(body) != "{\"ok\":true}" || calls != 2 {
		t.Fatalf("body=%s calls=%d err=%v", body, calls, err)
	}
	badMIME := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("{}"))
	}))
	defer badMIME.Close()
	if _, err = NewForTest(badMIME.Client(), nil, 1).Fetch(context.Background(), badMIME.URL, "application/json", []string{"application/json"}, 100); !errors.Is(err, ErrInvalidMIME) {
		t.Fatalf("expected MIME error, got %v", err)
	}
	oversize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("12345"))
	}))
	defer oversize.Close()
	if _, err = NewForTest(oversize.Client(), nil, 1).Fetch(context.Background(), oversize.URL, "application/json", []string{"application/json"}, 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected oversize error, got %v", err)
	}
}

func TestRedirectIsRevalidated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/private", http.StatusFound)
	}))
	defer server.Close()
	calls := 0
	validator := ValidatorFunc(func(_ context.Context, u *url.URL) error {
		calls++
		if u.Path == "/private" {
			return errors.New("blocked redirect")
		}
		return nil
	})
	_, err := NewForTest(server.Client(), validator, 1).Fetch(context.Background(), server.URL, "application/json", []string{"application/json"}, 100)
	if err == nil || calls < 2 {
		t.Fatalf("redirect was not revalidated: calls=%d err=%v", calls, err)
	}
}

func TestErrorsDoNotExposeQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer server.Close()
	_, err := NewForTest(server.Client(), nil, 1).Fetch(context.Background(), server.URL+"?email=private@example.org", "application/json", []string{"application/json"}, 100)
	if err == nil || strings.Contains(err.Error(), "private@example.org") {
		t.Fatalf("query was exposed: %v", err)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	h := server.Client()
	h.Timeout = 30 * time.Millisecond
	client := NewForTest(h, nil, 1)
	_, err := client.Fetch(context.Background(), server.URL, "application/json", []string{"application/json"}, 100)
	if err == nil {
		t.Fatal("expected timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Fetch(ctx, server.URL, "application/json", []string{"application/json"}, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
