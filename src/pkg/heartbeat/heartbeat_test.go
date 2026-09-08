package heartbeat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTickerPostsWithBearer(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("missing bearer, got %q", r.Header.Get("Authorization"))
		}
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	Start(ctx, Config{
		RemindersURL: srv.URL, DigestURL: srv.URL,
		Secret: "test-secret", Interval: 20 * time.Millisecond, Enabled: true,
	})
	time.Sleep(75 * time.Millisecond)
	cancel()
	if atomic.LoadInt32(&hits) < 2 {
		t.Fatalf("expected >=2 hits, got %d", hits)
	}
}

func TestDisabledDoesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, Config{Enabled: false}) // must not panic, must not dial
}
