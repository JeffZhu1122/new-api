// Package volcark calls the Volcengine Ark OpenAPI (the Action-style
// management API at ark.<region>.volcengineapi.com) with Access Key
// signatures. The asset library uses it to manage private asset groups and
// assets, which Ark only exposes through AK/SK authentication.
package volcark

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	// APIVersion is the Ark OpenAPI version of the asset APIs.
	APIVersion = "2024-01-01"
	// Service is the signing service name of Ark.
	Service = "ark"
	// DefaultRegion is the region the asset library is offered in.
	DefaultRegion = "cn-beijing"

	maxResponseBytes = 4 << 20
)

// Credentials is one Volcengine account's Access Key pair and the region and
// project its assets live in.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	// Endpoint overrides https://ark.<region>.volcengineapi.com, for private
	// endpoints and tests. Empty uses the public endpoint of Region.
	Endpoint string
}

// Client sends signed Ark OpenAPI requests for one account.
type Client struct {
	credentials Credentials
	httpClient  *http.Client
	now         func() time.Time
}

// NewClient returns a client for the account. A nil httpClient uses one with
// a 30 second timeout.
func NewClient(credentials Credentials, httpClient *http.Client) *Client {
	if credentials.Region == "" {
		credentials.Region = DefaultRegion
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{credentials: credentials, httpClient: httpClient, now: time.Now}
}

// Error is an error Ark returned in ResponseMetadata.Error, or an HTTP error
// without that envelope.
type Error struct {
	Action     string
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *Error) Error() string {
	text := fmt.Sprintf("volcengine ark %s failed (HTTP %d", e.Action, e.StatusCode)
	if e.Code != "" {
		text += ", " + e.Code
	}
	if e.RequestID != "" {
		text += ", request id " + e.RequestID
	}
	text += ")"
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

type responseMetadata struct {
	RequestId string `json:"RequestId"`
	Action    string `json:"Action"`
	Error     *struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	} `json:"Error"`
}

type envelope struct {
	ResponseMetadata responseMetadata `json:"ResponseMetadata"`
	Result           any              `json:"Result"`
}

// Response is Ark's raw answer to one action, kept byte for byte so a gateway
// can return it unchanged.
type Response struct {
	StatusCode int
	Body       []byte
}

// Call sends action with the JSON body and decodes the Result object into
// out (when out is not nil).
func (c *Client) Call(ctx context.Context, action string, body any, out any) error {
	payload := []byte("{}")
	if body != nil {
		encoded, err := common.Marshal(body)
		if err != nil {
			return err
		}
		payload = encoded
	}
	response, err := c.CallRaw(ctx, action, payload)
	if err != nil {
		return err
	}
	return response.Decode(action, out)
}

// CallRaw sends action with an already encoded JSON payload and returns Ark's
// response as it came. Only transport failures are errors here; use Decode
// to read the Result or the Ark error.
func (c *Client) CallRaw(ctx context.Context, action string, payload []byte) (*Response, error) {
	if c.credentials.AccessKeyID == "" || c.credentials.SecretAccessKey == "" {
		return nil, errors.New("volcengine ark access key is not configured")
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return nil, err
	}
	endpoint.RawQuery = url.Values{"Action": {action}, "Version": {APIVersion}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	signRequest(req, payload, c.credentials, Service, c.now().UTC())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("volcengine ark %s request failed: %w", action, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("volcengine ark %s response read failed: %w", action, err)
	}
	return &Response{StatusCode: resp.StatusCode, Body: data}, nil
}

// Decode returns the Ark error of the response, or decodes its Result into
// out (when out is not nil).
func (r *Response) Decode(action string, out any) error {
	var decoded envelope
	if out != nil {
		decoded.Result = out
	}
	if err := common.Unmarshal(r.Body, &decoded); err != nil {
		if r.StatusCode >= http.StatusBadRequest {
			return &Error{Action: action, StatusCode: r.StatusCode, Message: http.StatusText(r.StatusCode)}
		}
		return fmt.Errorf("volcengine ark %s returned an invalid response: %w", action, err)
	}
	if apiErr := decoded.ResponseMetadata.Error; apiErr != nil && (apiErr.Code != "" || apiErr.Message != "") {
		return &Error{Action: action, StatusCode: r.StatusCode, Code: apiErr.Code, Message: apiErr.Message, RequestID: decoded.ResponseMetadata.RequestId}
	}
	if r.StatusCode >= http.StatusBadRequest {
		return &Error{Action: action, StatusCode: r.StatusCode, Message: http.StatusText(r.StatusCode), RequestID: decoded.ResponseMetadata.RequestId}
	}
	return nil
}

func (c *Client) endpoint() (*url.URL, error) {
	raw := strings.TrimSpace(c.credentials.Endpoint)
	if raw == "" {
		raw = "https://ark." + c.credentials.Region + ".volcengineapi.com/"
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, fmt.Errorf("invalid volcengine ark endpoint %q", raw)
	}
	if endpoint.Path == "" {
		endpoint.Path = "/"
	}
	return endpoint, nil
}

// signRequest adds X-Date and the HMAC-SHA256 Authorization header of the
// Volcengine signature (https://www.volcengine.com/docs/6369/67269), signing
// the host and x-date headers.
func signRequest(req *http.Request, payload []byte, credentials Credentials, service string, now time.Time) {
	xDate := now.Format("20060102T150405Z")
	shortDate := xDate[:8]
	req.Header.Set("X-Date", xDate)

	payloadHash := sha256.Sum256(payload)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQuery(req.URL.Query()),
		"host:" + req.URL.Host + "\nx-date:" + xDate + "\n",
		"host;x-date",
		hex.EncodeToString(payloadHash[:]),
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	scope := shortDate + "/" + credentials.Region + "/" + service + "/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])

	key := hmacSHA256([]byte(credentials.SecretAccessKey), shortDate)
	key = hmacSHA256(key, credentials.Region)
	key = hmacSHA256(key, service)
	key = hmacSHA256(key, "request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	req.Header.Set("Authorization", "HMAC-SHA256 Credential="+credentials.AccessKeyID+"/"+scope+", SignedHeaders=host;x-date, Signature="+signature)
}

func canonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQuery encodes the query with RFC 3986 escaping (space as %20)
// and keys in ASCII order.
func canonicalQuery(values url.Values) string {
	return strings.ReplaceAll(values.Encode(), "+", "%20")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
