package service

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/fingerprint_setting"

	"github.com/gorilla/websocket"
	"github.com/imroc/req/v3"
	utls "github.com/refraction-networking/utls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// useFingerprintProfiles installs the fingerprint setting for one test.
func useFingerprintProfiles(t *testing.T, enabled bool, profiles ...fingerprint_setting.Profile) {
	t.Helper()
	apply := func(enabled bool, profiles []fingerprint_setting.Profile) {
		raw := []byte("[]")
		if len(profiles) > 0 {
			var err error
			raw, err = common.Marshal(profiles)
			require.NoError(t, err)
		}
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"fingerprint_setting.enabled":  strconv.FormatBool(enabled),
			"fingerprint_setting.profiles": string(raw),
		}))
		fingerprint_setting.Refresh()
	}
	apply(enabled, profiles)
	t.Cleanup(func() { apply(true, nil) })
}

// clientHelloTraits is what a TLS server can tell about a ClientHello.
type clientHelloTraits struct {
	CipherSuites []uint16
	Curves       []tls.CurveID
	Signatures   []tls.SignatureScheme
	Protocols    []string
	Versions     []uint16
	Extensions   []uint16
}

// startClientHelloServer starts an HTTP/2-capable TLS server. The returned
// function reports the ClientHello of the most recent connection.
func startClientHelloServer(t *testing.T, handler http.Handler) (*httptest.Server, func() clientHelloTraits) {
	t.Helper()
	var mutex sync.Mutex
	var last clientHelloTraits
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		mutex.Lock()
		defer mutex.Unlock()
		last = clientHelloTraits{
			CipherSuites: hello.CipherSuites,
			Curves:       hello.SupportedCurves,
			Signatures:   hello.SignatureSchemes,
			Protocols:    hello.SupportedProtos,
			Versions:     hello.SupportedVersions,
			Extensions:   hello.Extensions,
		}
		return nil, nil
	}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, func() clientHelloTraits {
		mutex.Lock()
		defer mutex.Unlock()
		return last
	}
}

// withoutGREASE drops the random placeholder values browsers mix into a ClientHello.
func withoutGREASE(values []uint16) []uint16 {
	return slices.DeleteFunc(slices.Clone(values), func(value uint16) bool { return value&0x0f0f == 0x0a0a })
}

func TestFingerprintClientHello(t *testing.T) {
	withRelayHTTPTransportSettings(t)
	var userAgent atomic.Value
	server, lastHello := startClientHelloServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		userAgent.Store(request.UserAgent())
	}))
	trust := testTLSClientConfig(t, server)

	// A ClientHello recorded from another TLS stack: crypto/tls with settings
	// that differ from the ones net/http uses.
	other := &tls.Config{
		ServerName:       "example.com",
		NextProtos:       []string{"h2", "http/1.1"},
		CipherSuites:     []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
	capture := func(config *tls.Config) string {
		clientSide, serverSide := net.Pipe()
		go func() { _ = tls.Client(clientSide, config).Handshake() }()
		record := make([]byte, 5)
		_, err := io.ReadFull(serverSide, record)
		require.NoError(t, err)
		message := make([]byte, int(record[3])<<8|int(record[4]))
		_, err = io.ReadFull(serverSide, message)
		require.NoError(t, err)
		require.NoError(t, serverSide.Close())
		require.NoError(t, clientSide.Close())
		return hex.EncodeToString(append(record, message...))
	}
	// The same stack without HTTP/2, as Node.js connects.
	otherHTTP1 := other.Clone()
	otherHTTP1.NextProtos = []string{"http/1.1"}

	// What the server sees when that stack connects on its own. The server is
	// reached by IP address, so neither hello carries a server name.
	direct := other.Clone()
	direct.ServerName = ""
	direct.RootCAs = trust.RootCAs
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), direct)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	otherHello := lastHello()

	useFingerprintProfiles(t, true,
		fingerprint_setting.Profile{Name: "chrome", TLSPreset: "chrome_133"},
		fingerprint_setting.Profile{Name: "captured", TLSClientHello: capture(other)},
		fingerprint_setting.Profile{Name: "captured-http1", TLSClientHello: capture(otherHTTP1)},
		fingerprint_setting.Profile{Name: "headers-only", HeaderOrder: []string{"accept"}},
	)
	chrome, err := utls.UTLSIdToSpec(utls.HelloChrome_133)
	require.NoError(t, err)

	observe := func(t *testing.T, protocol string, profile string) (clientHelloTraits, int) {
		t.Helper()
		client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: protocol, Shards: 1, Fingerprint: profile}, nil, trust)
		require.NoError(t, err)
		t.Cleanup(client.CloseIdleConnections)
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		drainClose(t, resp)
		return lastHello(), resp.ProtoMajor
	}
	stock, proto := observe(t, dto.HTTPProtocolAuto, "")
	require.Equal(t, 2, proto)
	stockHTTP1, proto := observe(t, dto.HTTPProtocolHTTP1, "")
	require.Equal(t, 1, proto)
	require.NotEqual(t, stock.CipherSuites, otherHello.CipherSuites)

	t.Run("a profile without a TLS part keeps the stock ClientHello", func(t *testing.T) {
		hello, proto := observe(t, dto.HTTPProtocolAuto, "headers-only")
		assert.Equal(t, stock, hello)
		assert.Equal(t, 2, proto)
		hello, proto = observe(t, dto.HTTPProtocolHTTP1, "headers-only")
		assert.Equal(t, stockHTTP1, hello)
		assert.Equal(t, 1, proto)
	})

	t.Run("preset", func(t *testing.T) {
		hello, proto := observe(t, dto.HTTPProtocolAuto, "chrome")
		assert.Equal(t, withoutGREASE(chrome.CipherSuites), withoutGREASE(hello.CipherSuites))
		assert.NotEqual(t, hello.CipherSuites, withoutGREASE(hello.CipherSuites), "a browser hello carries GREASE values")
		assert.NotEqual(t, stock.Extensions, hello.Extensions)
		assert.Equal(t, []string{"h2", "http/1.1"}, hello.Protocols)
		assert.Equal(t, 2, proto)

		hello, proto = observe(t, dto.HTTPProtocolHTTP1, "chrome")
		assert.Equal(t, withoutGREASE(chrome.CipherSuites), withoutGREASE(hello.CipherSuites))
		assert.Equal(t, []string{"http/1.1"}, hello.Protocols, "a channel forced to HTTP/1.1 must not offer h2")
		assert.Equal(t, 1, proto)
	})

	t.Run("captured ClientHello", func(t *testing.T) {
		hello, proto := observe(t, dto.HTTPProtocolAuto, "captured")
		assert.Equal(t, otherHello, hello)
		assert.Equal(t, 2, proto)
		assert.Equal(t, "Go-http-client/2.0", userAgent.Load())

		// A capture that does not offer h2 keeps the channel on HTTP/1.1, and
		// the default User-Agent names the version that is spoken.
		hello, proto = observe(t, dto.HTTPProtocolAuto, "captured-http1")
		assert.Equal(t, otherHello.CipherSuites, hello.CipherSuites)
		assert.Equal(t, []string{"http/1.1"}, hello.Protocols)
		assert.Equal(t, 1, proto)
		assert.Equal(t, "Go-http-client/1.1", userAgent.Load())

		hello, proto = observe(t, dto.HTTPProtocolHTTP1, "captured")
		assert.Equal(t, otherHello.CipherSuites, hello.CipherSuites)
		assert.Equal(t, []string{"http/1.1"}, hello.Protocols)
		assert.Equal(t, 1, proto)
	})
}

// http2Capture is what an HTTP/2 server can tell about a client connection
// and its first request.
type http2Capture struct {
	Settings     []http2.Setting
	WindowUpdate uint32
	// Priorities holds the PRIORITY frames sent before the first request.
	Priorities []http2.PriorityFrame
	// StreamID and HeaderPriority describe the HEADERS frame of the request.
	StreamID       uint32
	HeaderPriority http2.PriorityParam
	Fields         []string
	UserAgent      string
}

// captureHTTP2Request reads one request from a raw HTTP/2 connection and
// answers it with an empty 200 response.
func captureHTTP2Request(conn net.Conn) (http2Capture, error) {
	defer conn.Close()
	var capture http2Capture
	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(conn, preface); err != nil {
		return capture, err
	}
	framer := http2.NewFramer(conn, conn)
	framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	if err := framer.WriteSettings(); err != nil {
		return capture, err
	}
	for {
		frame, err := framer.ReadFrame()
		if err != nil {
			return capture, err
		}
		switch frame := frame.(type) {
		case *http2.SettingsFrame:
			if frame.IsAck() {
				continue
			}
			_ = frame.ForeachSetting(func(setting http2.Setting) error {
				capture.Settings = append(capture.Settings, setting)
				return nil
			})
			if err := framer.WriteSettingsAck(); err != nil {
				return capture, err
			}
		case *http2.WindowUpdateFrame:
			if frame.StreamID == 0 {
				capture.WindowUpdate = frame.Increment
			}
		case *http2.PriorityFrame:
			capture.Priorities = append(capture.Priorities, http2.PriorityFrame{FrameHeader: http2.FrameHeader{StreamID: frame.StreamID}, PriorityParam: frame.PriorityParam})
		case *http2.MetaHeadersFrame:
			capture.StreamID = frame.StreamID
			capture.HeaderPriority = frame.Priority
			for _, field := range frame.Fields {
				capture.Fields = append(capture.Fields, field.Name)
				if field.Name == "user-agent" {
					capture.UserAgent = field.Value
				}
			}
			var block bytes.Buffer
			if err := hpack.NewEncoder(&block).WriteField(hpack.HeaderField{Name: ":status", Value: "200"}); err != nil {
				return capture, err
			}
			return capture, framer.WriteHeaders(http2.HeadersFrameParam{StreamID: frame.StreamID, BlockFragment: block.Bytes(), EndHeaders: true, EndStream: true})
		}
	}
}

func TestFingerprintRequestHeaders(t *testing.T) {
	withRelayHTTPTransportSettings(t)
	useFingerprintProfiles(t, true,
		fingerprint_setting.Profile{
			Name:        "ordered",
			HeaderOrder: []string{"host", "content-type", "authorization", "user-agent", "accept", "content-length"},
			UserAgent:   "example-client/1.0",
		},
		fingerprint_setting.Profile{Name: "tls-only", TLSPreset: "firefox_120"},
		fingerprint_setting.Profile{
			Name:                "preface",
			HTTP2Fingerprint:    "1:65536;2:0;4:131072;5:16384;9:1|12517377|3:0:0:201,5:1:3:1|m,p,a,s",
			HTTP2HeaderPriority: "1:0:256",
		},
	)
	newRequest := func(t *testing.T, target string) *http.Request {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(`{"a":1}`))
		require.NoError(t, err)
		request.Header.Set("X-Zeta", "z")
		request.Header.Set("Authorization", "Bearer key")
		request.Header.Set("Accept", "*/*")
		request.Header.Set("X-Alpha", "a")
		request.Header.Set("Content-Type", "application/json")
		return request
	}

	t.Run("HTTP/1.1", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })
		heads := make(chan []string, 1)
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				reader := bufio.NewReader(conn)
				var lines []string
				bodyLength := 0
				for {
					line, err := reader.ReadString('\n')
					line = strings.TrimRight(line, "\r\n")
					if err != nil || line == "" {
						break
					}
					if value, ok := strings.CutPrefix(line, "Content-Length: "); ok {
						bodyLength, _ = strconv.Atoi(value)
					}
					lines = append(lines, line)
				}
				_, _ = io.CopyN(io.Discard, reader, int64(bodyLength))
				_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
				_ = conn.Close()
				heads <- lines
			}
		}()
		target := "http://" + listener.Addr().String() + "/v1/messages"
		send := func(t *testing.T, profile string, request *http.Request) []string {
			t.Helper()
			client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: dto.HTTPProtocolHTTP1, Shards: 1, Fingerprint: profile}, nil, nil)
			require.NoError(t, err)
			resp, err := client.Do(request)
			require.NoError(t, err)
			drainClose(t, resp)
			return (<-heads)[1:]
		}

		request := newRequest(t, target)
		// A forwarded header must not be able to steer the header layout.
		request.Header[req.HeaderOderKey] = []string{"x-zeta"}
		before := request.Header.Clone()
		assert.Equal(t, []string{
			"Host: " + listener.Addr().String(),
			"Content-Type: application/json",
			"Authorization: Bearer key",
			"User-Agent: example-client/1.0",
			"Accept: */*",
			"Content-Length: 7",
			"X-Alpha: a",
			"X-Zeta: z",
			"Accept-Encoding: gzip",
		}, send(t, "ordered", request))
		assert.Equal(t, before, request.Header, "the caller's request must not be modified")

		request = newRequest(t, target)
		request.Header.Set("User-Agent", "caller/2.0")
		assert.Contains(t, send(t, "ordered", request), "User-Agent: caller/2.0", "a request's own User-Agent wins")

		stock := send(t, "", newRequest(t, target))
		assert.Contains(t, stock, "User-Agent: Go-http-client/1.1")
		request = newRequest(t, target)
		request.Header[req.HeaderOderKey] = []string{"x-zeta"}
		assert.Equal(t, stock, send(t, "tls-only", request), "a profile without a header part keeps the stock request head")

		// The relay sets Accept and Content-Type even when the client sent
		// neither. Only a channel with a profile drops the empty headers.
		empty := func() *http.Request {
			request := newRequest(t, target)
			request.Header.Set("Accept", "")
			request.Header.Set("Content-Type", "")
			return request
		}
		stock = send(t, "", empty())
		assert.Contains(t, stock, "Accept: ")
		assert.Contains(t, stock, "Content-Type: ")
		for _, profile := range []string{"ordered", "tls-only", "preface"} {
			head := send(t, profile, empty())
			assert.False(t, slices.ContainsFunc(head, func(line string) bool {
				return strings.HasPrefix(line, "Accept:") || strings.HasPrefix(line, "Content-Type:")
			}), "profile %s: %v", profile, head)
			assert.Len(t, head, len(stock)-2, "profile %s drops nothing else", profile)
		}
		assert.Equal(t, send(t, "", newRequest(t, target)), send(t, "preface", newRequest(t, target)), "an HTTP/2 part leaves HTTP/1.1 requests alone")

		// A channel that allows HTTP/2 still speaks HTTP/1.1 to a plain HTTP upstream.
		client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: dto.HTTPProtocolAuto, Shards: 1, Fingerprint: "tls-only"}, nil, nil)
		require.NoError(t, err)
		resp, err := client.Do(newRequest(t, target))
		require.NoError(t, err)
		drainClose(t, resp)
		assert.Contains(t, <-heads, "User-Agent: Go-http-client/1.1")
	})

	t.Run("HTTP/2", func(t *testing.T) {
		captures := make(chan http2Capture, 1)
		server := httptest.NewUnstartedServer(nil)
		server.EnableHTTP2 = true
		server.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
			"h2": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
				capture, err := captureHTTP2Request(conn)
				assert.NoError(t, err)
				captures <- capture
			},
		}
		server.StartTLS()
		t.Cleanup(server.Close)
		trust := testTLSClientConfig(t, server)
		send := func(t *testing.T, profile string) http2Capture {
			t.Helper()
			client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: dto.HTTPProtocolAuto, Shards: 1, Fingerprint: profile}, nil, trust)
			require.NoError(t, err)
			t.Cleanup(client.CloseIdleConnections)
			resp, err := client.Do(newRequest(t, server.URL))
			require.NoError(t, err)
			require.Equal(t, 2, resp.ProtoMajor)
			drainClose(t, resp)
			return <-captures
		}

		stock := send(t, "")
		assert.Equal(t, "Go-http-client/2.0", stock.UserAgent)
		pseudoHeaders := stock.Fields[:4]
		// The one known difference below the TLS layer: net/http announces a
		// larger maximum frame size, the fingerprint transport keeps the
		// protocol default.
		settings := slices.DeleteFunc(slices.Clone(stock.Settings), func(setting http2.Setting) bool {
			return setting.ID == http2.SettingMaxFrameSize
		})

		ordered := send(t, "ordered")
		assert.Equal(t, append(slices.Clone(pseudoHeaders),
			"content-type", "authorization", "user-agent", "accept", "content-length", "x-alpha", "x-zeta", "accept-encoding",
		), ordered.Fields)
		assert.Equal(t, "example-client/1.0", ordered.UserAgent)
		assert.Equal(t, settings, ordered.Settings)
		assert.Equal(t, stock.WindowUpdate, ordered.WindowUpdate)

		tlsOnly := send(t, "tls-only")
		assert.Equal(t, pseudoHeaders, tlsOnly.Fields[:4])
		assert.ElementsMatch(t, stock.Fields, tlsOnly.Fields)
		assert.Equal(t, stock.UserAgent, tlsOnly.UserAgent, "the transport library must not announce itself")
		assert.Equal(t, settings, tlsOnly.Settings)
		assert.Equal(t, stock.WindowUpdate, tlsOnly.WindowUpdate)
		for _, capture := range []http2Capture{stock, ordered, tlsOnly} {
			assert.Empty(t, capture.Priorities)
			assert.Zero(t, capture.HeaderPriority)
			assert.EqualValues(t, 1, capture.StreamID)
		}

		preface := send(t, "preface")
		assert.Equal(t, []http2.Setting{
			{ID: http2.SettingHeaderTableSize, Val: 65536},
			{ID: http2.SettingEnablePush, Val: 0},
			{ID: http2.SettingInitialWindowSize, Val: 131072},
			{ID: http2.SettingMaxFrameSize, Val: 16384},
			{ID: http2.SettingID(9), Val: 1},
		}, preface.Settings)
		assert.EqualValues(t, 12517377, preface.WindowUpdate)
		assert.Equal(t, []http2.PriorityFrame{
			{FrameHeader: http2.FrameHeader{StreamID: 3}, PriorityParam: http2.PriorityParam{Weight: 200}},
			{FrameHeader: http2.FrameHeader{StreamID: 5}, PriorityParam: http2.PriorityParam{StreamDep: 3, Exclusive: true, Weight: 0}},
		}, preface.Priorities)
		assert.EqualValues(t, 7, preface.StreamID, "the first request follows the streams the PRIORITY frames name")
		assert.Equal(t, http2.PriorityParam{Exclusive: true, Weight: 255}, preface.HeaderPriority)
		assert.Equal(t, []string{":method", ":path", ":authority", ":scheme"}, preface.Fields[:4])
		assert.ElementsMatch(t, stock.Fields[4:], preface.Fields[4:], "an HTTP/2 part leaves the regular headers alone")
		assert.Equal(t, stock.UserAgent, preface.UserAgent)
	})
}

// The WebSocket relays dial with a dialer of their own. A channel with a
// profile gives the handshake the fingerprint of its HTTP/1.1 requests.
func TestFingerprintWebSocketDial(t *testing.T) {
	withRelayHTTPTransportSettings(t)
	useFingerprintProfiles(t, true,
		fingerprint_setting.Profile{
			Name:        "ordered",
			TLSPreset:   "firefox_120",
			HeaderOrder: []string{"host", "connection", "upgrade", "sec-websocket-version", "sec-websocket-key", "authorization", "user-agent"},
			UserAgent:   "example-client/1.0",
		},
		fingerprint_setting.Profile{Name: "agent-only", UserAgent: "example-client/1.0"},
	)

	var mutex sync.Mutex
	heads := make(chan []string, 4)
	upgrader := websocket.Upgrader{}
	server, lastHello := startClientHelloServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !websocket.IsWebSocketUpgrade(r) {
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		defer conn.Close()
		for {
			kind, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, message); err != nil {
				return
			}
		}
	}))
	// The server sees the request head only after parsing, so the proxy in
	// front of it records the bytes of plain-text handshakes instead.
	plain := httptest.NewServer(server.Config.Handler)
	t.Cleanup(plain.Close)
	var tunnels []string
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		mutex.Lock()
		tunnels = append(tunnels, r.Host+" "+r.Header.Get("Proxy-Authorization"))
		mutex.Unlock()
		upstream, err := net.Dial("tcp", r.Host)
		if !assert.NoError(t, err) {
			return
		}
		defer upstream.Close()
		client, buffered, err := w.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		defer client.Close()
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { _, _ = io.Copy(client, upstream) }()
		reader := bufio.NewReader(io.TeeReader(buffered, upstream))
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			if err != nil || line == "" {
				break
			}
			lines = append(lines, line)
		}
		// A tunnel that carries TLS shows no request head.
		if len(lines) > 0 && strings.HasPrefix(lines[0], "GET ") {
			heads <- lines
		}
		_, _ = io.Copy(io.Discard, reader)
	}))
	t.Cleanup(proxyServer.Close)
	proxyURL := strings.Replace(proxyServer.URL, "http://", "http://user:secret@", 1)

	dial := func(t *testing.T, target string, settings dto.ChannelSettings, header http.Header) http.Header {
		t.Helper()
		dialer := *websocket.DefaultDialer
		dialer.TLSClientConfig = testTLSClientConfig(t, server)
		require.NoError(t, ConfigureFingerprintWebSocketDialer(&dialer, header, settings))
		conn, resp, err := dialer.Dial(strings.Replace(target, "http", "ws", 1), header)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("ping")))
		_, message, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, "ping", string(message))
		return resp.Header
	}
	newHeader := func() http.Header {
		return http.Header{"Authorization": {"Bearer key"}, "Content-Type": {""}}
	}

	t.Run("a channel without a profile keeps the dialer", func(t *testing.T) {
		for _, settings := range []dto.ChannelSettings{{}, {Proxy: proxyURL}} {
			dialer := *websocket.DefaultDialer
			header := newHeader()
			require.NoError(t, ConfigureFingerprintWebSocketDialer(&dialer, header, settings))
			assert.NotNil(t, dialer.Proxy)
			assert.Nil(t, dialer.NetDialContext)
			assert.Nil(t, dialer.NetDialTLSContext)
			assert.Equal(t, newHeader(), header)
		}
		dial(t, server.URL, dto.ChannelSettings{}, newHeader())
		stock := lastHello()
		assert.Empty(t, stock.Protocols)

		// A profile without a TLS part keeps that ClientHello.
		dial(t, server.URL, dto.ChannelSettings{FingerprintProfile: "agent-only"}, newHeader())
		assert.Equal(t, stock, lastHello())
	})

	t.Run("ClientHello of the profile", func(t *testing.T) {
		client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: dto.HTTPProtocolHTTP1, Shards: 1, Fingerprint: "ordered"}, nil, testTLSClientConfig(t, server))
		require.NoError(t, err)
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		drainClose(t, resp)
		client.CloseIdleConnections()
		want := lastHello()
		require.Equal(t, []string{"http/1.1"}, want.Protocols)

		for _, proxy := range []string{"", proxyURL} {
			dial(t, server.URL, dto.ChannelSettings{FingerprintProfile: "ordered", Proxy: proxy}, newHeader())
			assert.Equal(t, want, lastHello(), "proxy %q", proxy)
		}
		mutex.Lock()
		defer mutex.Unlock()
		assert.Equal(t, []string{strings.TrimPrefix(server.URL, "https://") + " Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))}, tunnels)
	})

	t.Run("request head of the profile", func(t *testing.T) {
		header := newHeader()
		dial(t, plain.URL, dto.ChannelSettings{FingerprintProfile: "ordered", Proxy: proxyURL}, header)
		assert.Equal(t, http.Header{"Authorization": {"Bearer key"}, "User-Agent": {"example-client/1.0"}}, header)
		head := <-heads
		var names []string
		for _, line := range head[1:] {
			name, _, _ := strings.Cut(line, ":")
			names = append(names, name)
		}
		assert.Equal(t, []string{"Host", "Connection", "Upgrade", "Sec-WebSocket-Version", "Sec-WebSocket-Key", "Authorization", "User-Agent"}, names)
		assert.Contains(t, head, "User-Agent: example-client/1.0")

		header = newHeader()
		header.Set("User-Agent", "caller/2.0")
		dial(t, plain.URL, dto.ChannelSettings{FingerprintProfile: "ordered", Proxy: proxyURL}, header)
		assert.Contains(t, <-heads, "User-Agent: caller/2.0", "a request's own User-Agent wins")

		// Without a header part the head is the one the stock dialer writes,
		// except for the empty Content-Type.
		dial(t, plain.URL, dto.ChannelSettings{FingerprintProfile: "agent-only", Proxy: proxyURL}, newHeader())
		names = nil
		for _, line := range (<-heads)[1:] {
			name, _, _ := strings.Cut(line, ":")
			names = append(names, name)
		}
		assert.Equal(t, []string{"Host", "User-Agent", "Authorization", "Connection", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Upgrade"}, names)
	})

	t.Run("fails closed", func(t *testing.T) {
		dialer := *websocket.DefaultDialer
		require.ErrorContains(t, ConfigureFingerprintWebSocketDialer(&dialer, newHeader(), dto.ChannelSettings{FingerprintProfile: "missing"}), "does not exist")

		useFingerprintProfiles(t, false, fingerprint_setting.Profile{Name: "ordered", TLSPreset: "firefox_120"})
		require.NoError(t, ConfigureFingerprintWebSocketDialer(&dialer, newHeader(), dto.ChannelSettings{FingerprintProfile: "ordered"}))
		assert.Nil(t, dialer.NetDialTLSContext, "the global switch sends every channel back to the stock dialer")
	})
}

// A fingerprint client hands response bodies over exactly like the stock one.
func TestFingerprintResponseBodyParity(t *testing.T) {
	withRelayHTTPTransportSettings(t)
	useFingerprintProfiles(t, true, fingerprint_setting.Profile{Name: "chrome", TLSPreset: "chrome_133", HeaderOrder: []string{"accept"}})
	gbk := []byte{0xc4, 0xe3, 0xba, 0xc3}
	server := startHTTP2TLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gbk" {
			w.Header().Set("Content-Type", "text/plain; charset=gbk")
			_, _ = w.Write(gbk)
			return
		}
		assert.Equal(t, "gzip", r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = writer.Write([]byte("compressed body"))
		_ = writer.Close()
	}))
	trust := testTLSClientConfig(t, server)

	for _, profile := range []string{"", "chrome"} {
		t.Run("profile="+profile, func(t *testing.T) {
			client, err := newHTTPClientFromPolicy(HTTPTransportPolicy{Protocol: dto.HTTPProtocolAuto, Shards: 1, Fingerprint: profile}, nil, trust)
			require.NoError(t, err)
			t.Cleanup(client.CloseIdleConnections)

			resp, err := client.Get(server.URL + "/gzip")
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, "compressed body", string(body))
			assert.True(t, resp.Uncompressed)
			assert.Empty(t, resp.Header.Get("Content-Encoding"))

			resp, err = client.Get(server.URL + "/gbk")
			require.NoError(t, err)
			body, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, gbk, body, "the body must not be transcoded")
		})
	}
}

// The channel setting is the switch: these cases go through the same entry
// point the relay uses.
func TestFingerprintChannelSwitch(t *testing.T) {
	defaultClient := initDefaultHTTPClientFixture(t)
	useFingerprintProfiles(t, true, fingerprint_setting.Profile{Name: "chrome", TLSPreset: "chrome_133"})
	chrome, err := utls.UTLSIdToSpec(utls.HelloChrome_133)
	require.NoError(t, err)

	var mutex sync.Mutex
	connections := make(map[string]struct{})
	upstream, lastHello := startClientHelloServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		connections[r.RemoteAddr] = struct{}{}
	}))
	var tunnels int
	connectProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := net.Dial("tcp", r.Host)
		if r.Method != http.MethodConnect || err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer target.Close()
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		mutex.Lock()
		tunnels++
		mutex.Unlock()
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(target, conn) }()
		_, _ = io.Copy(conn, target)
	}))
	t.Cleanup(connectProxy.Close)

	fingerprintClients := func() int {
		proxyClients.mutex.RLock()
		defer proxyClients.mutex.RUnlock()
		count := 0
		for key := range proxyClients.clients {
			if strings.Contains(key, fingerprintCacheKeyMark) {
				count++
			}
		}
		return count
	}

	t.Run("a channel without a profile keeps the stock client", func(t *testing.T) {
		assert.Equal(t, defaultHTTPTransportPolicy(), NormalizeHTTPTransportPolicy(dto.ChannelSettings{}))
		client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
		require.NoError(t, err)
		assert.Same(t, defaultClient, client)
		assert.Zero(t, fingerprintClients())
	})

	t.Run("upstream certificates are still verified", func(t *testing.T) {
		client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{FingerprintProfile: "chrome"})
		require.NoError(t, err)
		_, err = client.Get(upstream.URL)
		require.ErrorContains(t, err, "certificate")
	})

	for _, tc := range []struct {
		name            string
		proxy           string
		settings        dto.ChannelSettings
		wantProto       int
		wantConnections int
	}{
		{name: "direct", settings: dto.ChannelSettings{FingerprintProfile: "chrome", TLSInsecureSkipVerify: true}, wantProto: 2, wantConnections: 1},
		{name: "through a proxy", proxy: connectProxy.URL, settings: dto.ChannelSettings{FingerprintProfile: "chrome", TLSInsecureSkipVerify: true}, wantProto: 2, wantConnections: 1},
		{name: "forced HTTP/1.1", settings: dto.ChannelSettings{FingerprintProfile: "chrome", TLSInsecureSkipVerify: true, HTTPProtocol: dto.HTTPProtocolHTTP1}, wantProto: 1, wantConnections: 1},
		{name: "three HTTP/2 connections", settings: dto.ChannelSettings{FingerprintProfile: "chrome", TLSInsecureSkipVerify: true, HTTP2ConnectionShards: 3}, wantProto: 2, wantConnections: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutex.Lock()
			clear(connections)
			tunnels = 0
			mutex.Unlock()
			client, err := GetHttpClientWithProxySettings(tc.proxy, tc.settings)
			require.NoError(t, err)
			again, err := GetHttpClientWithProxySettings(tc.proxy, tc.settings)
			require.NoError(t, err)
			assert.Same(t, client, again, "the client is cached per channel setting")
			assert.NotSame(t, defaultClient, client)
			for range 6 {
				resp, err := client.Get(upstream.URL)
				require.NoError(t, err)
				assert.Equal(t, tc.wantProto, resp.ProtoMajor)
				drainClose(t, resp)
			}
			assert.Equal(t, withoutGREASE(chrome.CipherSuites), withoutGREASE(lastHello().CipherSuites))
			mutex.Lock()
			defer mutex.Unlock()
			assert.Len(t, connections, tc.wantConnections)
			if tc.proxy != "" {
				assert.Equal(t, tc.wantConnections, tunnels, "the fingerprint handshake runs inside the proxy tunnel")
			}
		})
	}

	t.Run("editing a profile replaces its clients", func(t *testing.T) {
		settings := dto.ChannelSettings{FingerprintProfile: "chrome"}
		before, err := GetHttpClientWithProxySettings("", settings)
		require.NoError(t, err)
		require.Positive(t, fingerprintClients())

		useFingerprintProfiles(t, true, fingerprint_setting.Profile{Name: "chrome", TLSPreset: "firefox_120"})
		assert.Zero(t, fingerprintClients(), "clients of the old revision are dropped")
		after, err := GetHttpClientWithProxySettings("", settings)
		require.NoError(t, err)
		assert.NotSame(t, before, after)
		assert.Equal(t, 1, fingerprintClients())
	})

	t.Run("a missing profile fails the request instead of using the stock client", func(t *testing.T) {
		for _, proxy := range []string{"", connectProxy.URL} {
			_, err := GetHttpClientWithProxySettings(proxy, dto.ChannelSettings{FingerprintProfile: "gone"})
			require.ErrorContains(t, err, `fingerprint profile "gone" does not exist`)
		}
	})

	t.Run("the global switch turns every channel back to the stock client", func(t *testing.T) {
		profile := fingerprint_setting.Profile{Name: "chrome", TLSPreset: "chrome_133"}
		useFingerprintProfiles(t, true, profile)
		settings := dto.ChannelSettings{FingerprintProfile: "chrome"}
		_, err := GetHttpClientWithProxySettings("", settings)
		require.NoError(t, err)
		require.Equal(t, 1, fingerprintClients())
		useFingerprintProfiles(t, false, profile)
		assert.Zero(t, fingerprintClients())
		assert.Equal(t, defaultHTTPTransportPolicy(), NormalizeHTTPTransportPolicy(settings))
		client, err := GetHttpClientWithProxySettings("", settings)
		require.NoError(t, err)
		assert.Same(t, defaultClient, client)
	})
}

func TestValidateFingerprintOption(t *testing.T) {
	// ClientHello messages without the TLS record header, as some capture
	// tools export them; the second one resumes an earlier session.
	handshakeMessage := func(t *testing.T, extensions ...utls.TLSExtension) string {
		t.Helper()
		spec, err := utls.UTLSIdToSpec(utls.HelloFirefox_120)
		require.NoError(t, err)
		spec.Extensions = append(spec.Extensions, extensions...)
		conn := utls.UClient(nil, &utls.Config{ServerName: "example.com"}, utls.HelloCustom)
		require.NoError(t, conn.ApplyPreset(&spec))
		require.NoError(t, conn.BuildHandshakeState())
		return hex.EncodeToString(conn.HandshakeState.Hello.Raw)
	}
	fresh := handshakeMessage(t)
	resumed := handshakeMessage(t, &utls.FakePreSharedKeyExtension{
		Identities: []utls.PskIdentity{{Label: []byte("ticket"), ObfuscatedTicketAge: 1}},
		Binders:    [][]byte{make([]byte, 32)},
	})

	channel := &model.Channel{Name: "fingerprint-in-use", Key: "key"}
	channel.SetSetting(dto.ChannelSettings{FingerprintProfile: "in-use"})
	require.NoError(t, model.DB.Create(channel).Error)
	t.Cleanup(func() { require.NoError(t, model.DB.Delete(channel).Error) })
	inUse := `{"name":"in-use","user_agent":"example-client/1.0"}`

	for _, tc := range []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "switch on", key: "fingerprint_setting.enabled", value: "true"},
		{name: "switch must be a boolean", key: "fingerprint_setting.enabled", value: "maybe", wantErr: "true or false"},
		{name: "unknown field", key: "fingerprint_setting.other", value: "1", wantErr: "unknown fingerprint setting"},
		{name: "every part is optional", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_preset":"safari_16_0","header_order":["Host","Accept"]}]`},
		{name: "captured handshake message", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_client_hello":"` + fresh + `"}]`},
		{name: "not JSON", key: "fingerprint_setting.profiles", value: `{`, wantErr: "profiles"},
		{name: "bad name", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a|b"}]`, wantErr: "name"},
		{name: "duplicate name", key: "fingerprint_setting.profiles", value: `[` + inUse + `,` + inUse + `]`, wantErr: "in-use"},
		{name: "unknown preset", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_preset":"netscape_4"}]`, wantErr: "unknown tls_preset"},
		{name: "preset and capture together", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_preset":"chrome_133","tls_client_hello":"` + fresh + `"}]`, wantErr: "tls_preset"},
		{name: "capture is not hex", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_client_hello":"hello"}]`, wantErr: "hex dump"},
		{name: "capture is not a ClientHello", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_client_hello":"1603010002abcd"}]`, wantErr: "cannot be parsed"},
		{name: "capture of a resumed session", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","tls_client_hello":"` + resumed + `"}]`, wantErr: "resumes an earlier session"},
		{name: "header order with a duplicate", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","header_order":["Accept","accept"]}]`, wantErr: "header_order"},
		{name: "user agent with a line break", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","user_agent":"a\nb"}]`, wantErr: "user_agent"},
		{name: "HTTP/2 part", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"1:65536;2:0;4:4194304;6:262144|15663105|0|m,a,s,p","http2_header_priority":"1:0:256"}]`},
		{name: "HTTP/2 part with priorities", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"1:65536,4:131072,5:16384|12517377|3:0:0:201,5:0:0:101,7:0:0:1,9:0:7:1,11:0:3:1,13:0:0:241|m,p,a,s"}]`},
		{name: "HTTP/2 part with a missing section", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|m,a,s,p"}]`, wantErr: "SETTINGS|WINDOW_UPDATE|PRIORITY"},
		{name: "HTTP/2 part without settings", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"|15663105|0|m,a,s,p"}]`, wantErr: "id:value"},
		{name: "HTTP/2 setting repeated", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0;2:0|15663105|0|m,a,s,p"}]`, wantErr: "more than once"},
		{name: "HTTP/2 server push", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:1|15663105|0|m,a,s,p"}]`, wantErr: "ENABLE_PUSH"},
		{name: "HTTP/2 stream window larger than the transport keeps", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"4:6291456|15663105|0|m,a,s,p"}]`, wantErr: "INITIAL_WINDOW_SIZE"},
		{name: "HTTP/2 stream window that can stall", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"4:1024|15663105|0|m,a,s,p"}]`, wantErr: "INITIAL_WINDOW_SIZE"},
		{name: "HTTP/2 frame size the transport does not write", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"5:65536|15663105|0|m,a,s,p"}]`, wantErr: "MAX_FRAME_SIZE"},
		{name: "HTTP/2 header list too small", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"6:100|15663105|0|m,a,s,p"}]`, wantErr: "MAX_HEADER_LIST_SIZE"},
		{name: "HTTP/2 without a window update", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|00|0|m,a,s,p"}]`, wantErr: "WINDOW_UPDATE"},
		{name: "HTTP/2 priority on a server stream", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|4:0:0:201|m,a,s,p"}]`, wantErr: "odd, increasing"},
		{name: "HTTP/2 priorities out of order", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|5:0:0:201,3:0:0:101|m,a,s,p"}]`, wantErr: "odd, increasing"},
		{name: "HTTP/2 priority weight out of range", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|3:0:0:257|m,a,s,p"}]`, wantErr: "stream:exclusive:dependency:weight"},
		{name: "HTTP/2 pseudo-header named twice", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|0|m,a,s,s"}]`, wantErr: "m, a, s and p"},
		{name: "HTTP/2 pseudo-header missing", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_fingerprint":"2:0|15663105|0|m,a,s"}]`, wantErr: "m, a, s and p"},
		{name: "HTTP/2 header priority without a weight", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_header_priority":"1:0"}]`, wantErr: "http2_header_priority"},
		{name: "HTTP/2 header priority weight zero", key: "fingerprint_setting.profiles", value: `[` + inUse + `,{"name":"a","http2_header_priority":"1:0:0"}]`, wantErr: "http2_header_priority"},
		{name: "profile still selected by a channel", key: "fingerprint_setting.profiles", value: `[]`, wantErr: "still selected by channel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFingerprintOption(tc.key, tc.value)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestFingerprintProfileOptions(t *testing.T) {
	useFingerprintProfiles(t, false,
		fingerprint_setting.Profile{Name: "zeta", TLSPreset: "chrome_133", UserAgent: "example-client/1.0"},
		fingerprint_setting.Profile{Name: "alpha", HeaderOrder: []string{"accept"}},
	)

	options := GetFingerprintProfileOptions()
	assert.False(t, options.Enabled)
	assert.Equal(t, []string{"zeta", "alpha"}, options.Profiles, "names keep the saved order")
	assert.Equal(t, []string{"chrome_120", "chrome_120_pq", "chrome_131", "chrome_133", "edge_106", "firefox_120", "ios_14", "safari_16_0"}, options.TLSPresets)

	// The channel editor reads this as JSON; an empty list must not be null.
	useFingerprintProfiles(t, true)
	encoded, err := common.Marshal(GetFingerprintProfileOptions())
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"enabled":true,"profiles":[],"tls_presets":["chrome_120"`)
}
