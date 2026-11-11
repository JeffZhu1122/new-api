package service

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/fingerprint_setting"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

// ConfigureFingerprintWebSocketDialer makes the WebSocket handshake of a
// channel that selected a fingerprint profile look like its HTTP/1.1 requests:
// the same ClientHello, header order and User-Agent. Every other channel keeps
// the dialer and the header exactly as they are.
//
// The dialer cannot hand over a connection after its own proxy tunnel, so a
// fingerprint channel opens the tunnel here and the dialer's proxy is cleared.
func ConfigureFingerprintWebSocketDialer(dialer *websocket.Dialer, header http.Header, settings dto.ChannelSettings) error {
	policy := NormalizeHTTPTransportPolicy(settings)
	if policy.Fingerprint == "" {
		return nil
	}
	profile, ok := fingerprint_setting.Lookup(policy.Fingerprint)
	if !ok {
		return fmt.Errorf("fingerprint profile %q does not exist", policy.Fingerprint)
	}
	tlsFingerprint, err := newFingerprintTLS(profile, true, dialer.TLSClientConfig)
	if err != nil {
		return err
	}
	connector := &fingerprintWebSocketConnector{tlsFingerprint: tlsFingerprint, tlsConfig: dialer.TLSClientConfig}
	if settings.Proxy != "" {
		connector.proxyURL, _, err = common.ParseProxyURLRuntime(settings.Proxy)
		if err != nil {
			return err
		}
	}
	for _, name := range profile.HeaderOrder {
		connector.headerOrder = append(connector.headerOrder, textproto.CanonicalMIMEHeaderKey(name))
	}

	dropEmptyRelayHeaders(header)
	if profile.UserAgent != "" && header.Get("User-Agent") == "" {
		header.Set("User-Agent", profile.UserAgent)
	}
	dialer.Proxy = nil
	dialer.NetDial = nil
	dialer.NetDialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return connector.connect(ctx, address, false)
	}
	dialer.NetDialTLSContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return connector.connect(ctx, address, true)
	}
	return nil
}

// dropEmptyRelayHeaders removes Accept and Content-Type when they are empty.
// The relay copies both from the client even when the client sent neither,
// which leaves an empty header no real client writes.
func dropEmptyRelayHeaders(header http.Header) {
	for _, name := range []string{"Accept", "Content-Type"} {
		if values, ok := header[name]; ok && !slices.ContainsFunc(values, func(value string) bool { return value != "" }) {
			delete(header, name)
		}
	}
}

// fingerprintWebSocketConnector opens the connection a WebSocket handshake is
// written to: through the proxy when there is one, then the TLS handshake of
// the profile.
type fingerprintWebSocketConnector struct {
	// proxyURL is the channel proxy. Without one the environment decides.
	proxyURL *url.URL
	// tlsFingerprint is nil when the profile has no TLS part.
	tlsFingerprint *fingerprintTLS
	// tlsConfig is the TLS config of the dialer. It serves the profiles without
	// a TLS part and the TLS session to an HTTPS proxy.
	tlsConfig *tls.Config
	// headerOrder holds canonical header names. Empty keeps Go's order.
	headerOrder []string
}

func (f *fingerprintWebSocketConnector) connect(ctx context.Context, address string, secure bool) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	proxyURL := f.proxyURL
	if proxyURL == nil {
		target := &url.URL{Scheme: "http", Host: address}
		if secure {
			target.Scheme = "https"
		}
		proxyURL, err = http.ProxyFromEnvironment(&http.Request{URL: target})
		if err != nil {
			return nil, err
		}
	}
	standardTLS := &tls.Config{}
	if f.tlsConfig != nil {
		standardTLS = f.tlsConfig.Clone()
	}
	netDialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	var conn net.Conn
	switch {
	case proxyURL == nil:
		conn, err = netDialer.DialContext(ctx, "tcp", address)
	case proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h":
		var socks proxy.Dialer
		socks, err = proxy.FromURL(proxyURL, netDialer)
		if err != nil {
			return nil, err
		}
		contextDialer, ok := socks.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("the SOCKS5 proxy dialer cannot be bound to a request")
		}
		conn, err = contextDialer.DialContext(ctx, "tcp", address)
	case proxyURL.Scheme == "http" || proxyURL.Scheme == "https":
		proxyAddress := proxyURL.Host
		if proxyURL.Port() == "" && proxyURL.Scheme == "https" {
			proxyAddress = net.JoinHostPort(proxyURL.Hostname(), "443")
		} else if proxyURL.Port() == "" {
			proxyAddress = net.JoinHostPort(proxyURL.Hostname(), "80")
		}
		conn, err = netDialer.DialContext(ctx, "tcp", proxyAddress)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if proxyURL != nil && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") {
		if proxyURL.Scheme == "https" {
			proxyTLS := standardTLS.Clone()
			proxyTLS.ServerName = cmp.Or(proxyTLS.ServerName, proxyURL.Hostname())
			tlsConn := tls.Client(conn, proxyTLS)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			conn = tlsConn
		}
		tunnel := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: address}, Host: address, Header: make(http.Header)}
		if user := proxyURL.User; user != nil {
			password, _ := user.Password()
			tunnel.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user.Username()+":"+password)))
		}
		if err := tunnel.Write(conn); err != nil {
			_ = conn.Close()
			return nil, err
		}
		// The upstream stays silent until the client speaks, so nothing but the
		// proxy's answer can sit in this reader.
		resp, err := http.ReadResponse(bufio.NewReader(conn), tunnel)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy refused the tunnel to %s: %s", address, resp.Status)
		}
	}

	if secure {
		if f.tlsFingerprint != nil {
			tlsConn, _, err := f.tlsFingerprint.handshake(ctx, host, conn)
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			conn = tlsConn
		} else {
			// The same handshake the dialer performs on its own.
			standardTLS.ServerName = cmp.Or(standardTLS.ServerName, host)
			tlsConn := tls.Client(conn, standardTLS)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, err
			}
			conn = tlsConn
		}
	}
	if len(f.headerOrder) > 0 {
		conn = &orderedHandshakeConn{Conn: conn, order: f.headerOrder}
	}
	return conn, nil
}

// orderedHandshakeConn rewrites the request head of the WebSocket handshake,
// the first thing written to the connection, so its headers follow the order
// of a fingerprint profile. Everything after the head passes through.
type orderedHandshakeConn struct {
	net.Conn
	order   []string
	pending []byte
	written bool
}

func (c *orderedHandshakeConn) Write(p []byte) (int, error) {
	if c.written {
		return c.Conn.Write(p)
	}
	c.pending = append(c.pending, p...)
	head, rest, found := bytes.Cut(c.pending, []byte("\r\n\r\n"))
	if !found {
		return len(p), nil
	}
	// The first line is the request line. Headers the profile does not name
	// keep their place after the ones it names.
	lines := strings.Split(string(head), "\r\n")
	slices.SortStableFunc(lines[1:], func(a, b string) int {
		return cmp.Compare(c.rank(a), c.rank(b))
	})
	c.written = true
	c.pending = nil
	if _, err := c.Conn.Write(append([]byte(strings.Join(lines, "\r\n")+"\r\n\r\n"), rest...)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// rank is the position of a header line in the profile order.
func (c *orderedHandshakeConn) rank(line string) int {
	name, _, _ := strings.Cut(line, ":")
	if index := slices.Index(c.order, textproto.CanonicalMIMEHeaderKey(name)); index >= 0 {
		return index
	}
	return len(c.order)
}
