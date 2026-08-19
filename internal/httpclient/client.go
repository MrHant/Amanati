// Package httpclient sends a resolved request and captures everything the UI
// needs to show about the exchange.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mrhant/amanati/internal/collection"
)

// maxBody caps how much of a response we keep in memory for display.
const maxBody = 8 << 20 // 8 MiB

// Request is a fully resolved call: no variables left, nothing disabled.
type Request struct {
	Method  string
	URL     string
	Headers []collection.Param
	Query   []collection.Param
	Body    collection.Body
	Auth    collection.Auth
}

// Response is the captured result of sending a Request.
type Response struct {
	Status     string
	StatusCode int
	Proto      string
	Headers    http.Header
	Body       []byte
	Truncated  bool
	Size       int64
	Duration   time.Duration

	// Sent echoes what actually went out, so the user can debug variable
	// expansion without a proxy.
	SentURL     string
	SentMethod  string
	SentHeaders http.Header
}

// Client sends requests. The zero value is not usable; use New.
type Client struct {
	http *http.Client
}

// Options tweak transport behaviour.
type Options struct {
	Timeout            time.Duration
	FollowRedirects    bool
	InsecureSkipVerify bool
}

// New builds a client from opts.
func New(opts Options) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}

	c := &http.Client{Timeout: opts.Timeout, Transport: transport}
	if !opts.FollowRedirects {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return &Client{http: c}
}

// Send performs the request and reads the response body.
func (c *Client) Send(ctx context.Context, req Request) (*Response, error) {
	target, err := buildURL(req.URL, req.Query)
	if err != nil {
		return nil, err
	}

	payload, contentType, err := buildBody(req.Body)
	if err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), payload)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for _, h := range req.Headers {
		if key := strings.TrimSpace(h.Key); key != "" {
			httpReq.Header.Set(key, h.Value)
		}
	}
	applyAuth(httpReq, req.Auth)

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, unwrapURLError(err)
	}
	defer httpResp.Body.Close()

	body, truncated, err := readCapped(httpResp.Body, maxBody)
	elapsed := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	size := int64(len(body))
	if httpResp.ContentLength > 0 {
		size = httpResp.ContentLength
	}

	return &Response{
		Status:      httpResp.Status,
		StatusCode:  httpResp.StatusCode,
		Proto:       httpResp.Proto,
		Headers:     httpResp.Header,
		Body:        body,
		Truncated:   truncated,
		Size:        size,
		Duration:    elapsed,
		SentURL:     httpReq.URL.String(),
		SentMethod:  method,
		SentHeaders: httpReq.Header.Clone(),
	}, nil
}

// buildURL merges the extra query params into the URL, keeping any the URL
// already carried.
func buildURL(raw string, extra []collection.Param) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("invalid URL: missing host")
	}

	if len(extra) > 0 {
		q := parsed.Query()
		for _, p := range extra {
			key := strings.TrimSpace(p.Key)
			if key == "" {
				continue
			}
			q.Set(key, p.Value)
		}
		parsed.RawQuery = q.Encode()
	}
	return parsed, nil
}

// buildBody renders the body, returning the reader and the Content-Type it
// implies (an explicit header set by the user still wins, since headers are
// applied afterwards).
func buildBody(b collection.Body) (io.Reader, string, error) {
	switch b.Mode {
	case collection.BodyRaw:
		if b.Raw == "" {
			return nil, "", nil
		}
		ct := b.ContentType
		if ct == "" {
			ct = "text/plain"
		}
		return strings.NewReader(b.Raw), ct, nil

	case collection.BodyForm:
		form := url.Values{}
		for _, p := range b.Form {
			if p.Disabled || strings.TrimSpace(p.Key) == "" {
				continue
			}
			form.Add(p.Key, p.Value)
		}
		if len(form) == 0 {
			return nil, "", nil
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil

	case collection.BodyMulti:
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, p := range b.Form {
			if p.Disabled || strings.TrimSpace(p.Key) == "" {
				continue
			}
			if err := w.WriteField(p.Key, p.Value); err != nil {
				return nil, "", err
			}
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		if buf.Len() == 0 {
			return nil, "", nil
		}
		return &buf, w.FormDataContentType(), nil

	default:
		return nil, "", nil
	}
}

func applyAuth(req *http.Request, a collection.Auth) {
	switch a.Type {
	case collection.AuthBearer:
		if a.Token != "" {
			req.Header.Set("Authorization", "Bearer "+a.Token)
		}
	case collection.AuthBasic:
		req.SetBasicAuth(a.Username, a.Password)
	case collection.AuthAPIKey:
		if a.Key == "" {
			return
		}
		if a.In == "query" {
			q := req.URL.Query()
			q.Set(a.Key, a.Value)
			req.URL.RawQuery = q.Encode()
			return
		}
		req.Header.Set(a.Key, a.Value)
	}
}

func readCapped(r io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// unwrapURLError turns net/http's wrapped errors into something worth showing
// in the response pane, where "Get \"http://...\": dial tcp ..." is mostly noise.
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return errors.New("request timed out")
		}
		return urlErr.Err
	}
	return err
}
