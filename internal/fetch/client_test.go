package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClientRetries(retries int, maxBytes int64) *Client {
	c := New(2*time.Second, retries, maxBytes, "cattv/test")
	c.RetryDelay = time.Millisecond
	return c
}

func TestGetSuccessSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg bytes"))
	}))
	defer srv.Close()

	got, err := testClientRetries(0, 1024).Get(context.Background(), srv.URL+"/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "jpeg bytes" {
		t.Fatalf("body = %q", got)
	}
	if gotUA != "cattv/test" {
		t.Fatalf("user agent = %q", gotUA)
	}
}

func TestGetRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	got, err := testClientRetries(3, 1024).Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ok" {
		t.Fatalf("body = %q", got)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("calls = %d", got)
	}
}

func TestGetDoesNotRetryNotFound(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := testClientRetries(3, 1024).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := HTTPStatus(err); got != http.StatusNotFound {
		t.Fatalf("status = %d", got)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestGetBoundsResponseSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	_, err := testClientRetries(0, 16).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected size error")
	}
	var fetchErr *Error
	if !errors.As(err, &fetchErr) || fetchErr.Kind != KindTooLarge {
		t.Fatalf("error = %#v", err)
	}
}

func TestGetRejectsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>not a cat</html>"))
	}))
	defer srv.Close()

	_, err := testClientRetries(0, 1024).Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected content type error")
	}
	var fetchErr *Error
	if !errors.As(err, &fetchErr) || fetchErr.Kind != KindInvalidImage {
		t.Fatalf("error = %#v", err)
	}
}

func TestGetClassifiesNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := testClientRetries(1, 1024).Get(context.Background(), url)
	if err == nil {
		t.Fatal("expected network error")
	}
	if !IsNetwork(err) {
		t.Fatalf("not classified as network: %#v", err)
	}
}

func TestGetHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := testClientRetries(2, 1024).Get(ctx, "https://example.invalid/cat.jpg")
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if !IsCanceled(err) {
		t.Fatalf("not classified as canceled: %#v", err)
	}
}

func TestGetRejectsUnsupportedScheme(t *testing.T) {
	_, err := testClientRetries(0, 1024).Get(context.Background(), "file:///etc/passwd")
	if err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("2"); got != 2*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := parseRetryAfter("not a date"); got != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestClientTimeoutIsRetryableNotCallerCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	c := New(2*time.Second, 2, 1024, "cattv/test")
	c.HTTP.Timeout = 15 * time.Millisecond
	c.RetryDelay = time.Millisecond
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if IsCanceled(err) {
		t.Fatalf("per-request timeout was misclassified as caller cancellation: %v", err)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("calls = %d, want retries after timeout", got)
	}
}
