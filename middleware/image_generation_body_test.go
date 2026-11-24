package middleware

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The image generation bodies clients send besides a JSON body with its
// Content-Type reach the model lookup and the relay as that JSON body, or are
// refused when they upload a file.
func TestNormalizeImageGenerationBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	multipartBody := func(fields map[string]string, file bool) (string, string) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for key, value := range fields {
			require.NoError(t, writer.WriteField(key, value))
		}
		if file {
			part, err := writer.CreateFormFile("image", "cat.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("png bytes"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		return writer.FormDataContentType(), body.String()
	}
	formType, formBody := multipartBody(map[string]string{
		"model": "gpt-image-2.5-sunburst", "prompt": "a cat", "n": "2", "size": "1024x1024",
		"stream": "false", "output_compression": "80", "parameters": `{"seed":1}`,
	}, false)
	fileType, fileBody := multipartBody(map[string]string{"model": "gpt-image-2.5-sunburst", "prompt": "a cat"}, true)
	badNType, badNBody := multipartBody(map[string]string{"model": "gpt-image-2.5-sunburst", "prompt": "a cat", "n": "two"}, false)
	jsonBody := `{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":1}`

	for _, tc := range []struct {
		name        string
		path        string
		contentType string
		body        string
		// wantJSON is the body the handlers read; "" keeps the client's body.
		wantJSON  string
		wantModel string
		// wantCount is the image count billed from the body the relay reads.
		wantCount int
		wantError string
	}{
		{
			name: "multipart form", path: "/v1/images/generations", contentType: formType, body: formBody,
			wantJSON:  `{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":2,"size":"1024x1024","stream":false,"output_compression":80,"parameters":{"seed":1}}`,
			wantModel: "gpt-image-2.5-sunburst", wantCount: 2,
		},
		{
			name: "urlencoded form", path: "/v1/images/generations", contentType: "application/x-www-form-urlencoded",
			body:      url.Values{"model": {"gpt-image-2.5-sunburst"}, "prompt": {"a cat"}, "n": {"1"}}.Encode(),
			wantJSON:  `{"model":"gpt-image-2.5-sunburst","prompt":"a cat","n":1}`,
			wantModel: "gpt-image-2.5-sunburst", wantCount: 1,
		},
		{name: "JSON without a Content-Type", path: "/v1/images/generations", body: "\n " + jsonBody, wantModel: "gpt-image-2.5-sunburst", wantCount: 1},
		{name: "JSON with its Content-Type", path: "/v1/images/generations", contentType: "application/json", body: jsonBody, wantModel: "gpt-image-2.5-sunburst", wantCount: 1},
		{name: "multipart form uploading a file", path: "/v1/images/generations", contentType: fileType, body: fileBody, wantError: "send image inputs to /v1/images/edits"},
		{name: "form with a non-integer n", path: "/v1/images/generations", contentType: badNType, body: badNBody, wantError: "n must be an integer"},
		{name: "body without a Content-Type that is not JSON", path: "/v1/images/generations", body: "model=gpt-image-2.5-sunburst", wantError: "Content-Type: application/json"},
		{name: "image edit upload", path: "/v1/images/edits", contentType: fileType, body: fileBody, wantModel: "gpt-image-2.5-sunburst"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var readBody, readType string
			router := gin.New()
			router.POST(tc.path, NormalizeImageGenerationBody(), func(c *gin.Context) {
				readType = c.Request.Header.Get("Content-Type")
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				raw, err := storage.Bytes()
				require.NoError(t, err)
				readBody = string(raw)
				modelRequest, _, err := getModelRequest(c)
				if err != nil {
					abortWithOpenAiMessage(c, invalidRequestStatus(err), err.Error())
					return
				}
				assert.Equal(t, tc.wantModel, modelRequest.Model)
				if tc.path == "/v1/images/generations" {
					imageRequest, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
					require.NoError(t, err)
					assert.Equal(t, tc.wantModel, imageRequest.Model)
					assert.Equal(t, "a cat", imageRequest.Prompt)
					count, err := imageRequest.ImageCount(false)
					require.NoError(t, err)
					assert.Equal(t, tc.wantCount, count)
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if tc.wantError != "" {
				assert.Equal(t, http.StatusBadRequest, recorder.Code)
				assert.Contains(t, recorder.Body.String(), tc.wantError)
				return
			}
			require.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
			switch {
			case tc.path != "/v1/images/generations":
				assert.Equal(t, tc.contentType, readType, "an edit upload is left as sent")
				assert.Equal(t, tc.body, readBody)
			case tc.wantJSON != "":
				assert.Equal(t, "application/json", readType)
				assert.JSONEq(t, tc.wantJSON, readBody)
			default:
				assert.Equal(t, "application/json", readType)
				assert.Equal(t, tc.body, readBody, "a JSON body is read as sent")
			}
		})
	}
}
