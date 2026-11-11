package service

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/quic-go/quic-go/http3"
)

// The HTTP/3 transports are shared by every client. Each one owns a UDP socket
// that only Transport.Close releases, while cached clients are dropped without
// being closed when the client cache is reset. A transport opens its socket on
// the first request, so a deployment without an HTTP/3 channel never opens one.
var (
	sharedHTTP3Transport         = &http3.Transport{}
	sharedInsecureHTTP3Transport = &http3.Transport{TLSClientConfig: common.InsecureTLSConfig.Clone()}
)

// http3RoundTripper sends requests over HTTP/3 for channels whose http_protocol
// is "http3". HTTP/3 is forced, not negotiated: a request fails when the
// upstream does not serve it or UDP cannot reach it.
type http3RoundTripper struct {
	transport *http3.Transport
	// proxy reports the proxy a stock transport would use for the request.
	// QUIC cannot go through it, so such a request is refused instead of
	// leaving through a different network path.
	proxy func(*http.Request) (*url.URL, error)
	// responseHeaderTimeout bounds the wait for the response headers, like
	// http.Transport.ResponseHeaderTimeout, except that it also covers the
	// connection setup and the request upload. Zero means no limit.
	responseHeaderTimeout time.Duration
}

type http3ResponseHeaderTimeoutError struct{}

func (http3ResponseHeaderTimeoutError) Error() string {
	return "http3: timeout awaiting response headers"
}

func (http3ResponseHeaderTimeoutError) Timeout() bool { return true }

// http3ResponseBody releases the request context once the caller is done with
// the response.
type http3ResponseBody struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
}

func (b *http3ResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}

func (r *http3RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if proxyURL, err := r.proxy(req); err != nil || proxyURL != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errors.New("http3 cannot be used through the proxy configured in the environment")
	}
	if r.responseHeaderTimeout <= 0 {
		return r.transport.RoundTrip(req)
	}

	ctx, cancel := context.WithCancelCause(req.Context())
	timer := time.AfterFunc(r.responseHeaderTimeout, func() {
		cancel(http3ResponseHeaderTimeoutError{})
	})
	resp, err := r.transport.RoundTrip(req.WithContext(ctx))
	if !timer.Stop() {
		// The timer fired: the request context is cancelled, so a response that
		// arrived at the same moment cannot be read any more.
		if err == nil {
			_ = resp.Body.Close()
		}
		return nil, http3ResponseHeaderTimeoutError{}
	}
	if err != nil {
		cancel(nil)
		return nil, err
	}
	resp.Body = &http3ResponseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func (r *http3RoundTripper) CloseIdleConnections() {
	r.transport.CloseIdleConnections()
}

// newHTTP3Client builds the client of an HTTP/3 channel. A nil tlsConfig uses
// the shared transport; a caller-provided one gets a transport of its own.
func newHTTP3Client(tlsConfig *tls.Config) *http.Client {
	transport := sharedHTTP3Transport
	switch {
	case tlsConfig == common.InsecureTLSConfig || tlsConfig == nil && common.TLSInsecureSkipVerify:
		transport = sharedInsecureHTTP3Transport
	case tlsConfig != nil:
		transport = &http3.Transport{TLSClientConfig: tlsConfig.Clone()}
	}
	return newRelayHTTPClient(&http3RoundTripper{
		transport:             transport,
		proxy:                 http.ProxyFromEnvironment,
		responseHeaderTimeout: time.Duration(min(max(common.RelayResponseHeaderTimeout, 0), maxTimeoutSeconds)) * time.Second,
	})
}
