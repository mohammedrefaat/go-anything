package mcp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// maxHTTPResponseBytes bounds both recording and replay responses to 8 MiB.
const maxHTTPResponseBytes int64 = 8 << 20

func outboundClient(allowNetwork ...bool) *http.Client {
	allowPrivate := len(allowNetwork) > 0 && allowNetwork[0]
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Resolve and dial directly so proxies and DNS rebinding cannot bypass policy.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for %s", host)
		}
		for _, addr := range ips {
			ip := addr.IP
			if !allowPrivate && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
				return nil, fmt.Errorf("private or local HTTP destination is disabled: %s", host)
			}
		}
		var lastErr error
		for _, addr := range ips {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &boundedTransport{transport}}
}

type boundedTransport struct{ base *http.Transport }

func (t *boundedTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }
func (t *boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTrip runs for the initial URL and every redirect target.
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("only HTTP(S) destinations are supported")
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &boundedBody{ReadCloser: resp.Body, remaining: maxHTTPResponseBytes}
	return resp, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	if int64(n) > b.remaining {
		return 0, fmt.Errorf("HTTP response exceeds %d bytes", maxHTTPResponseBytes)
	}
	b.remaining -= int64(n)
	return n, err
}
