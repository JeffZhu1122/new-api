package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/requestpatch"

	"github.com/gin-gonic/gin"
)

// The form fields of an image generation whose JSON value is not a string.
var (
	imageGenerationIntegerFields = []string{"n", "output_compression", "partial_images", "frame_count", "frame_duration_ms", "seed"}
	imageGenerationBoolFields    = []string{"stream", "watermark", "watermark_enabled"}
	imageGenerationJSONFields    = []string{"parameters", "extra_fields"}
)

// imageGenerationJSONSniffBytes is how much of a body without a Content-Type
// is read to tell whether it is a JSON object.
const imageGenerationJSONSniffBytes = 512

// NormalizeImageGenerationBody turns the other bodies clients send to
// POST /v1/images/generations into the JSON body that the model lookup, the
// relay and the openai_image plugins read. Without it those requests lose
// their model:
//
//   - a JSON object without a Content-Type is marked application/json;
//   - a multipart or urlencoded form becomes a JSON object, with the numeric,
//     boolean and JSON fields typed as the JSON API takes them;
//   - a multipart form that uploads a file is refused: image inputs belong to
//     /v1/images/edits, and dropping the file would generate an unrelated
//     image.
//
// It runs after authentication, so an anonymous body is never parsed.
func NormalizeImageGenerationBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || !strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
			c.Next()
			return
		}
		mediaType, _, _ := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
		var values map[string][]string
		var err error
		switch mediaType {
		case "":
			// Marked as JSON only when it starts with a JSON object; any other
			// body is left for the model lookup to refuse.
			var storage common.BodyStorage
			var reader io.ReadCloser
			if storage, err = common.GetBodyStorage(c); err != nil {
				break
			}
			if reader, err = storage.NewReader(); err != nil {
				break
			}
			prefix := make([]byte, imageGenerationJSONSniffBytes)
			n, readErr := io.ReadFull(reader, prefix)
			_ = reader.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
				err = readErr
				break
			}
			if bytes.HasPrefix(bytes.TrimLeft(prefix[:n], " \t\r\n\ufeff"), []byte("{")) {
				c.Request.Header.Set("Content-Type", gin.MIMEJSON)
			}
		case gin.MIMEMultipartPOSTForm:
			form, formErr := common.ParseMultipartFormReusable(c)
			if formErr != nil {
				err = fmt.Errorf("failed to parse multipart form: %w", formErr)
				break
			}
			files := len(form.File)
			values = form.Value
			_ = form.RemoveAll()
			if files > 0 {
				err = errors.New("/v1/images/generations does not take files; send image inputs to /v1/images/edits")
			}
		case gin.MIMEPOSTForm:
			var storage common.BodyStorage
			var raw []byte
			if storage, err = common.GetBodyStorage(c); err != nil {
				break
			}
			if raw, err = storage.Bytes(); err != nil {
				break
			}
			if values, err = url.ParseQuery(string(raw)); err != nil {
				err = fmt.Errorf("failed to parse form: %w", err)
			}
		}
		if err == nil && values != nil {
			// The converted JSON becomes the body of every later reader,
			// including the upstream request, which copies the client's
			// Content-Type.
			var body []byte
			var stored common.BodyStorage
			body, err = imageGenerationFormJSON(values)
			if err == nil && int64(len(body)) > requestpatch.MaxBodyBytes() {
				err = common.ErrRequestBodyTooLarge
			}
			if err == nil {
				stored, err = common.CreateBodyStorage(body)
			}
			if err == nil {
				if previous, ok := c.Get(common.KeyBodyStorage); ok {
					if storage, ok := previous.(common.BodyStorage); ok {
						_ = storage.Close()
					}
				}
				c.Set(common.KeyBodyStorage, stored)
				c.Set(common.KeyRequestBody, nil)
				c.Request.Body = io.NopCloser(stored)
				c.Request.ContentLength = int64(len(body))
				c.Request.MultipartForm = nil
				c.Request.PostForm = nil
				c.Request.Header.Set("Content-Type", gin.MIMEJSON)
			}
		}
		if err != nil {
			abortWithOpenAiMessage(c, invalidRequestStatus(err), i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		c.Next()
	}
}

// imageGenerationFormJSON converts the fields of a form to the JSON object the
// image generation API takes. A repeated field becomes an array of strings,
// and an empty typed field is left out.
func imageGenerationFormJSON(values map[string][]string) ([]byte, error) {
	object := make(map[string]any, len(values))
	for key, list := range values {
		if len(list) != 1 {
			object[key] = list
			continue
		}
		value := list[0]
		trimmed := strings.TrimSpace(value)
		switch {
		case slices.Contains(imageGenerationIntegerFields, key):
			if trimmed == "" {
				continue
			}
			number, err := strconv.ParseInt(trimmed, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be an integer", key)
			}
			object[key] = number
		case slices.Contains(imageGenerationBoolFields, key):
			if trimmed == "" {
				continue
			}
			flag, err := strconv.ParseBool(trimmed)
			if err != nil {
				return nil, fmt.Errorf("%s must be true or false", key)
			}
			object[key] = flag
		case slices.Contains(imageGenerationJSONFields, key):
			if trimmed == "" {
				continue
			}
			var parsed any
			if err := common.Unmarshal([]byte(trimmed), &parsed); err != nil {
				return nil, fmt.Errorf("%s must be JSON", key)
			}
			object[key] = common.RawMessage(trimmed)
		default:
			object[key] = value
		}
	}
	return common.Marshal(object)
}
