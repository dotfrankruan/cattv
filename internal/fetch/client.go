// Package fetch performs bounded, cancellable HTTP image downloads. It never
// writes image bytes to disk.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxBytes = 8 << 20
	defaultTimeout  = 15 * time.Second
	defaultRetries  = 2
)

// Kind classifies a download failure so callers can decide whether to try a
// different photo or stop and report an offline condition.
type Kind string

const (
	KindNetwork      Kind = "network"
	KindHTTP         Kind = "http"
	KindTooLarge     Kind = "too_large"
	KindInvalidImage Kind = "invalid_response"
	KindCanceled     Kind = "canceled"
)

// Error is a structured fetch failure.
type Error struct {
	Kind       Kind
	URL        string
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

func (e *Error) Error() string {
	prefix := "download failed"
	if e.StatusCode > 0 {
		prefix = fmt.Sprintf("image server returned HTTP %d", e.StatusCode)
	}
	if e.Err != nil {
		return prefix + ": " + e.Err.Error()
	}
	return prefix
}

func (e *Error) Unwrap() error { return e.Err }

// IsNetwork reports whether err is a transport/DNS failure rather than a
// server response.
func IsNetwork(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == KindNetwork
}

// IsCanceled reports whether the caller's context ended.
func IsCanceled(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var e *Error
	return errors.As(err, &e) && e.Kind == KindCanceled
}

// HTTPStatus returns an HTTP status code for an Error, if any.
func HTTPStatus(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.StatusCode
	}
	return 0
}

// Client is a bounded image downloader. It is safe for concurrent use.
type Client struct {
	HTTP       *http.Client
	Retries    int
	MaxBytes   int64
	RetryDelay time.Duration
	UserAgent  string
}

// New constructs a downloader with conservative defaults and a transport that
// bounds each phase of the request.
func New(timeout time.Duration, retries int, maxBytes int64, userAgent string) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if retries < 0 {
		retries = 0
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if userAgent == "" {
		userAgent = "cattv"
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: minDuration(timeout, 10*time.Second),
		ExpectContinueTimeout: time.Second,
	}
	return &Client{
		HTTP: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		Retries:    retries,
		MaxBytes:   maxBytes,
		RetryDelay: 350 * time.Millisecond,
		UserAgent:  userAgent,
	}
}

// Get downloads one image URL with bounded retries. Retryable statuses are
// 408, 425, 429, 500, 502, 503, and 504; other HTTP errors fail fast.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	if c == nil {
		return nil, errors.New("nil fetch client")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, &Error{Kind: KindInvalidImage, URL: rawURL, Err: errors.New("invalid image URL")}
	}
	if c.HTTP == nil {
		c.HTTP = New(defaultTimeout, c.Retries, c.MaxBytes, c.UserAgent).HTTP
	}
	retries := c.Retries
	if retries < 0 {
		retries = 0
	}
	maxBytes := c.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	delay := c.RetryDelay
	if delay <= 0 {
		delay = 350 * time.Millisecond
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "cattv"
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		data, err := c.downloadOnce(ctx, rawURL, ua, maxBytes)
		if err == nil {
			return data, nil
		}
		if IsCanceled(err) {
			return nil, err
		}
		lastErr = err
		var fetchErr *Error
		retryable := errors.As(err, &fetchErr) &&
			(fetchErr.Kind == KindNetwork || retryableStatus(fetchErr.StatusCode))
		if !retryable || attempt == retries {
			return nil, err
		}

		wait := delay * time.Duration(1<<attempt)
		if fetchErr.RetryAfter > 0 {
			wait = fetchErr.RetryAfter
		}
		wait = minDuration(wait, 5*time.Second)
		// Small positive jitter avoids synchronized retries when scripts run
		// several viewers at once.
		wait += time.Duration(rand.Int63n(int64(50 * time.Millisecond)))
		if err := sleepContext(ctx, wait); err != nil {
			return nil, &Error{Kind: KindCanceled, URL: rawURL, Err: err}
		}
	}
	return nil, lastErr
}

func (c *Client) downloadOnce(ctx context.Context, rawURL, ua string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &Error{Kind: KindInvalidImage, URL: rawURL, Err: err}
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/*;q=0.8")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Error{Kind: KindCanceled, URL: rawURL, Err: ctx.Err()}
		}
		return nil, &Error{Kind: KindNetwork, URL: rawURL, Err: err}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 32*1024))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		fetchErr := &Error{
			Kind:       KindHTTP,
			URL:        rawURL,
			StatusCode: resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Err:        errors.New(http.StatusText(resp.StatusCode)),
		}
		return nil, fetchErr
	}
	ctype := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if strings.HasPrefix(ctype, "text/") || ctype == "application/json" {
		return nil, &Error{Kind: KindInvalidImage, URL: rawURL, StatusCode: resp.StatusCode,
			Err: fmt.Errorf("unexpected content type %q", ctype)}
	}
	if resp.ContentLength > maxBytes {
		return nil, &Error{Kind: KindTooLarge, URL: rawURL, StatusCode: resp.StatusCode,
			Err: fmt.Errorf("image is %d bytes; limit is %d", resp.ContentLength, maxBytes)}
	}
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &Error{Kind: KindCanceled, URL: rawURL, Err: ctx.Err()}
		}
		return nil, &Error{Kind: KindNetwork, URL: rawURL, Err: err}
	}
	if int64(len(data)) > maxBytes {
		return nil, &Error{Kind: KindTooLarge, URL: rawURL,
			Err: fmt.Errorf("image exceeds %d-byte limit", maxBytes)}
	}
	if len(data) == 0 {
		return nil, &Error{Kind: KindInvalidImage, URL: rawURL, Err: errors.New("empty response body")}
	}
	return data, nil
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
