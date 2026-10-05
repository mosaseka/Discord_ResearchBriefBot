package discord

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostRetries429And5xx(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bot secret" {
			t.Error("missing authorization")
		}
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"retry_after":0}`))
			return
		}
		if calls == 2 {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42"}`))
	}))
	defer server.Close()
	id, err := NewForTest("secret", server.URL, server.Client()).Post(context.Background(), "123", Payload{Content: "hello"})
	if err != nil || id != "42" || calls != 3 {
		t.Fatalf("id=%s calls=%d err=%v", id, calls, err)
	}
}
func TestPostCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"retry_after":1}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := NewForTest("secret", server.URL, server.Client()).Post(ctx, "123", Payload{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestDeliveryAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"success", nil, false},
		{"rejected", &HTTPError{Status: 403}, false},
		{"rate limited", &HTTPError{Status: 429}, false},
		{"server error", &HTTPError{Status: 503}, true},
		{"request timeout", &HTTPError{Status: 408}, true},
		{"canceled", context.Canceled, true},
		{"deadline", context.DeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAmbiguous(tc.err); got != tc.want {
				t.Fatalf("ambiguous=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestAmbiguousAttemptFollowedByRejection(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	_, err := NewForTest("fixture-token", server.URL, server.Client()).Post(context.Background(), "123", Payload{Content: "fixture"})
	var rejection *HTTPError
	if !errors.As(err, &rejection) || rejection.Status != http.StatusForbidden || !IsAmbiguous(err) || calls != 2 {
		t.Fatalf("earlier ambiguous attempt was lost: calls=%d err=%v", calls, err)
	}
}
