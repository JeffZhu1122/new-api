package volcark

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The example key pair of the Volcengine signature document, split so that
// secret scanners do not take it for a real key.
const (
	docExampleAccessKeyID     = "AKLTYWViMTVmZGYzM2E0NDI" + "5Mzk2MDZjNjFmMjc2MjRjMzg"
	docExampleSecretAccessKey = "WkRZeE1EQmxPVGhsWWpWak5HVmtNbU" + "UxTXpZeU9UVXlOMlE1TmpZeVlqTQ=="
)

// The GET and POST examples of the Volcengine signature document
// (https://www.volcengine.com/docs/6369/67269) with their published signatures.
func TestSignRequestMatchesTheVolcengineDocumentExamples(t *testing.T) {
	credentials := Credentials{
		AccessKeyID:     docExampleAccessKeyID,
		SecretAccessKey: docExampleSecretAccessKey,
		Region:          "cn-beijing",
	}
	signedAt := time.Date(2025, 3, 29, 18, 9, 37, 0, time.UTC)
	cases := []struct {
		name      string
		method    string
		url       string
		payload   string
		signature string
	}{
		{
			name:      "GET",
			method:    http.MethodGet,
			url:       "https://billing.volcengineapi.com/?Action=QueryBalanceAcct&Version=2022-01-01",
			signature: "1eda9e7e6b1728151a8e8791fdaf67cfbd28bd5c80d0fce2eb208746cf483105",
		},
		{
			name:      "POST JSON",
			method:    http.MethodPost,
			url:       "https://billing.volcengineapi.com/?Action=ListBill&Version=2022-01-01",
			payload:   `{"Limit":10,"BillPeriod":"2023-08"}`,
			signature: "5e8480ceea12d0000a23c054151c50dd02c1a7dec835004057d19f13d53a7658",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.url, strings.NewReader(tc.payload))
			require.NoError(t, err)

			signRequest(req, []byte(tc.payload), credentials, "billing", signedAt)

			assert.Equal(t, "20250329T180937Z", req.Header.Get("X-Date"))
			assert.Equal(t, "HMAC-SHA256 Credential="+docExampleAccessKeyID+"/20250329/cn-beijing/billing/request, SignedHeaders=host;x-date, Signature="+tc.signature, req.Header.Get("Authorization"))
		})
	}
}

func TestCallSendsASignedActionAndDecodesTheResult(t *testing.T) {
	var gotQuery, gotBody, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotAuthorization = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"ResponseMetadata":{"RequestId":"req-1","Action":"CreateAssetGroup"},"Result":{"Id":"group-1"}}`)
	}))
	defer server.Close()
	client := NewClient(Credentials{AccessKeyID: "ak", SecretAccessKey: "sk", Endpoint: server.URL}, server.Client())

	var result struct {
		Id string `json:"Id"`
	}
	err := client.Call(context.Background(), "CreateAssetGroup", map[string]any{"Name": "u1-portraits"}, &result)

	require.NoError(t, err)
	assert.Equal(t, "group-1", result.Id)
	assert.Equal(t, "Action=CreateAssetGroup&Version=2024-01-01", gotQuery)
	assert.JSONEq(t, `{"Name":"u1-portraits"}`, gotBody)
	assert.True(t, strings.HasPrefix(gotAuthorization, "HMAC-SHA256 Credential=ak/"), gotAuthorization)
	assert.Contains(t, gotAuthorization, "/cn-beijing/ark/request, SignedHeaders=host;x-date, Signature=")
}

func TestCallReturnsTheArkErrorWithItsCodeAndRequestID(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   Error
	}{
		{
			name:   "error envelope",
			status: http.StatusForbidden,
			body:   `{"ResponseMetadata":{"RequestId":"req-2","Error":{"Code":"AccessDenied","Message":"no permission"}}}`,
			want:   Error{Action: "ListAssetGroups", StatusCode: http.StatusForbidden, Code: "AccessDenied", Message: "no permission", RequestID: "req-2"},
		},
		{
			name:   "HTTP error without an envelope",
			status: http.StatusBadGateway,
			body:   `upstream down`,
			want:   Error{Action: "ListAssetGroups", StatusCode: http.StatusBadGateway, Message: "Bad Gateway"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := NewClient(Credentials{AccessKeyID: "ak", SecretAccessKey: "sk", Endpoint: server.URL}, server.Client())

			err := client.Call(context.Background(), "ListAssetGroups", nil, nil)

			var apiErr *Error
			require.True(t, errors.As(err, &apiErr), err)
			assert.Equal(t, tc.want, *apiErr)
		})
	}
}

func TestCallRefusesAMissingAccessKey(t *testing.T) {
	err := NewClient(Credentials{AccessKeyID: "ak"}, nil).Call(context.Background(), "ListAssetGroups", nil, nil)

	assert.EqualError(t, err, "volcengine ark access key is not configured")
}
