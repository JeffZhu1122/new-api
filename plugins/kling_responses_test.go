package plugins_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKlingResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "kling",
		model:     "kling-v3",
		requestBody: map[string]any{
			"model": "kling-v3",
			"input": []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "camera orbit"},
				map[string]any{"type": "input_image", "image_url": "https://cdn.example/frame.png"},
			}}},
			"seconds": 10,
			"metadata": map[string]any{
				"mode": "pro",
			},
		},
		wantAction: "image_to_video",
		wantRequest: map[string]any{
			"model":    "kling-v3",
			"prompt":   "camera orbit",
			"image":    "https://cdn.example/frame.png",
			"duration": float64(10),
			"metadata": map[string]any{"mode": "pro"},
		},
		wantUsageKeys:  []string{"units"},
		wantVendorName: "kling",
	})

	source, err := builtinplugins.Source("kling")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "kling"})
	require.NoError(t, err)
	submit := func(t *testing.T, decoder []string, model string, body map[string]any) (*taskplugin.TaskAdaptor, *gin.Context, *relaycommon.RelayInfo, *dto.TaskError) {
		t.Helper()
		value, err := plugin.Engine.CallPath(t.Context(), decoder[0], decoder[1:], map[string]any{"model": model, "body": map[string]any{"kind": "json", "value": body}})
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api-singapore.klingai.com", ApiKey: "ak|sk", UpstreamModelName: model},
			OriginModelName: model,
			TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		}
		adaptor := taskplugin.New(plugin)
		adaptor.Init(info)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		c.Set("task_request", alibabaObject(t, value)["requestBody"])
		return adaptor, c, info, adaptor.ValidateRequestAndSetAction(c, info)
	}
	sentBody := func(t *testing.T, adaptor *taskplugin.TaskAdaptor, c *gin.Context, info *relaycommon.RelayInfo) map[string]any {
		t.Helper()
		reader, err := adaptor.BuildRequestBody(c, info)
		require.NoError(t, err)
		encoded, err := io.ReadAll(reader)
		require.NoError(t, err)
		var sent map[string]any
		require.NoError(t, common.Unmarshal(encoded, &sent))
		return sent
	}
	native := []string{"native", "decodeSubmit"}
	openaiVideo := []string{"protocols", "openai_video", "decodeRequest"}

	// Kling bills the duration it sends, and metadata.duration (the whole body
	// on native routes) overrides the top-level one, so the plugin bounds that
	// value to a whole number of seconds up to 3600 before the host reserves
	// quota. Kling checks the durations of each model itself.
	t.Run("bounds the billed duration before billing", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			decoder []string
			model   string
			body    map[string]any
		}{
			{"native duration above an hour", native, "kling-v3", map[string]any{"model_name": "kling-v3", "prompt": "a cat", "duration": 1e12}},
			{"native fractional duration", native, "kling-v3", map[string]any{"model_name": "kling-v3", "prompt": "a cat", "duration": "5.5"}},
			{"metadata duration behind valid seconds", openaiVideo, "kling-v3", map[string]any{"model": "kling-v3", "prompt": "a cat", "seconds": "5", "metadata": map[string]any{"duration": 3601}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := plugin.Engine.CallPath(t.Context(), tc.decoder[0], tc.decoder[1:], map[string]any{"model": tc.model, "body": map[string]any{"kind": "json", "value": tc.body}})
				if err == nil {
					_, _, _, taskErr := submit(t, tc.decoder, tc.model, tc.body)
					require.NotNil(t, taskErr)
					assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
					err = taskErr.Error
				}
				assert.ErrorContains(t, err, "duration must be a positive integer of at most 3600")
			})
		}

		adaptor, c, info, taskErr := submit(t, openaiVideo, "kling-v3", map[string]any{"model": "kling-v3", "prompt": "a cat", "seconds": "5", "metadata": map[string]any{"duration": "10"}})
		require.Nil(t, taskErr)
		assert.Equal(t, "10", sentBody(t, adaptor, c, info)["duration"])
		facts, err := adaptor.ExtractUsageFactsValidated(c, info)
		require.NoError(t, err)
		assert.InDelta(t, 6.0, facts["units"], 1e-9, "kling-v3 std costs 0.6 units per second")

		adaptor, c, info, taskErr = submit(t, openaiVideo, "kling-v2-6", map[string]any{"model": "kling-v2-6", "prompt": "a cat", "seconds": "7"})
		require.Nil(t, taskErr)
		assert.Equal(t, "7", sentBody(t, adaptor, c, info)["duration"], "Kling checks the model's durations")
		facts, err = adaptor.ExtractUsageFactsValidated(c, info)
		require.NoError(t, err)
		assert.InDelta(t, 2.1, facts["units"], 1e-9)
	})

	// A host-protocol size gets the nearest of Kling's text-to-video aspect
	// ratios.
	t.Run("sizes take the nearest aspect ratio", func(t *testing.T) {
		for size, want := range map[string]string{"1792x1024": "16:9", "720x720": "1:1", "1024x1792": "9:16", "2560x1080": "16:9"} {
			adaptor, c, info, taskErr := submit(t, openaiVideo, "kling-v3", map[string]any{"model": "kling-v3", "prompt": "a cat", "size": size})
			require.Nil(t, taskErr)
			assert.Equal(t, want, sentBody(t, adaptor, c, info)["aspect_ratio"], size)
		}
		_, _, _, taskErr := submit(t, openaiVideo, "kling-v3", map[string]any{"model": "kling-v3", "prompt": "a cat", "size": "large"})
		require.NotNil(t, taskErr)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		assert.ErrorContains(t, taskErr.Error, "size must be WIDTHxHEIGHT")
	})

	// A native request reaches Kling as the client sent it, for Kling to check:
	// no model alias, no default aspect ratio, cfg_scale, mode or duration. The
	// reservation follows Kling's units for the mode and audio at Kling's
	// defaults, and the dearest units the plugin knows for a mode or audio the
	// model's table lacks.
	t.Run("native bodies and their units", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			model     string
			body      map[string]any
			wantUnits float64
		}{
			{
				name: "defaults", model: "kling-v2-5-turbo",
				body:      map[string]any{"model_name": "kling-v2-5-turbo", "prompt": "a cat"},
				wantUnits: 1.5,
			},
			{
				name: "2.6 pro with audio", model: "kling-v2-6",
				body:      map[string]any{"model_name": "kling-v2-6", "prompt": "a cat", "mode": "pro", "sound": "on", "duration": "10", "aspect_ratio": "9:16"},
				wantUnits: 10,
			},
			{
				name: "2.6 with a reference voice", model: "kling-v2-6",
				body:      map[string]any{"model_name": "kling-v2-6", "image": "https://cdn.example/a.png", "mode": "pro", "sound": "on", "voice_list": []any{map[string]any{"voice_id": "v1"}}},
				wantUnits: 6,
			},
			{
				name: "v3 in 4k", model: "kling-v3",
				body:      map[string]any{"model_name": "kling-v3", "prompt": "a cat", "mode": "4k", "duration": 15},
				wantUnits: 45,
			},
			{
				name: "a duration the model does not list", model: "kling-v2-6",
				body:      map[string]any{"model_name": "kling-v2-6", "prompt": "a cat", "duration": "8"},
				wantUnits: 2.4,
			},
			{
				name: "audio on a model without an audio price", model: "kling-v2-5-turbo",
				body:      map[string]any{"model_name": "kling-v2-5-turbo", "prompt": "a cat", "sound": "on"},
				wantUnits: 4.5,
			},
			{
				name: "audio in a mode the model's audio table lacks", model: "kling-v2-6",
				body:      map[string]any{"model_name": "kling-v2-6", "prompt": "a cat", "sound": "on"},
				wantUnits: 5,
			},
			{
				name: "a mode the model does not list", model: "kling-v2-6",
				body:      map[string]any{"model_name": "kling-v2-6", "prompt": "a cat", "mode": "4k"},
				wantUnits: 2.5,
			},
			{
				name: "a model the plugin does not list", model: "kling-v9",
				body:      map[string]any{"model_name": "kling-v9", "prompt": "a cat"},
				wantUnits: 4.5,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				adaptor, c, info, taskErr := submit(t, native, tc.model, tc.body)
				require.Nil(t, taskErr)
				assert.Equal(t, decodePluginValue(t, tc.body), sentBody(t, adaptor, c, info))
				facts, err := adaptor.ExtractUsageFactsValidated(c, info)
				require.NoError(t, err)
				assert.InDelta(t, tc.wantUnits, facts["units"], 1e-9)
			})
		}

		for _, tc := range []struct {
			name    string
			body    map[string]any
			wantErr string
		}{
			{"model is not model_name", map[string]any{"model": "kling-v3", "prompt": "a cat"}, "model_name is required"},
			{"callbacks carry Kling's task ids", map[string]any{"model_name": "kling-v3", "prompt": "a cat", "callback_url": "https://client.example/hook"}, "callback_url is not supported"},
			{"external ids are not looked up", map[string]any{"model_name": "kling-v3", "prompt": "a cat", "external_task_id": "mine-1"}, "external_task_id is not supported"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := plugin.Engine.CallPath(t.Context(), "native", []string{"decodeSubmit"}, map[string]any{"body": map[string]any{"kind": "json", "value": tc.body}})
				assert.ErrorContains(t, err, tc.wantErr)
			})
		}
	})

	// A /v1/videos client cannot pass a body off as a native one.
	t.Run("a vendorBody field is not a native body", func(t *testing.T) {
		adaptor, c, info, taskErr := submit(t, openaiVideo, "kling-v2-6", map[string]any{"model": "kling-v2-6", "prompt": "a cat",
			"vendorBody": map[string]any{"prompt": "x", "callback_url": "https://client.example/hook"}})
		require.Nil(t, taskErr)
		assert.Equal(t, map[string]any{"model_name": "kling-v2-6", "prompt": "a cat", "mode": "std", "duration": "5"}, sentBody(t, adaptor, c, info))
	})

	t.Run("native replies keep Kling's envelope", func(t *testing.T) {
		processing := map[string]any{"code": float64(0), "message": "SUCCEED", "request_id": "req-1", "data": map[string]any{"task_id": "task_public", "task_status": "processing"}}
		failed, err := plugin.Engine.CallPath(t.Context(), "native", []string{"taskStatus"}, map[string]any{},
			map[string]any{"task_id": "task_public", "status": "FAILURE", "fail_reason": "task timed out", "data": processing})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"code": float64(0), "message": "SUCCEED", "request_id": "req-1",
			"data": map[string]any{"task_id": "task_public", "task_status": "failed", "task_status_msg": "task timed out"},
		}, alibabaObject(t, failed), "a task the gateway failed does not read as running")

		rendered, err := plugin.Engine.CallPath(t.Context(), "native", []string{"error"}, map[string]any{},
			map[string]any{"code": "invalid_request", "message": "duration must be one of 5, 10", "httpStatus": 400, "requestId": "req-2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"code": float64(1201), "message": "duration must be one of 5, 10", "request_id": "req-2"}, alibabaObject(t, rendered))
	})
}

func TestKlingModelPath(t *testing.T) {
	source, err := builtinplugins.Source("kling")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "kling"})
	require.NoError(t, err)
	call := func(t *testing.T, name string, args ...any) map[string]any {
		t.Helper()
		value, err := plugin.Engine.CallPath(t.Context(), name, nil, args...)
		require.NoError(t, err)
		return alibabaObject(t, value)
	}
	type submission struct {
		url   string
		body  map[string]any
		units float64
		state map[string]any
	}
	// Kling's model-in-path API takes an API key, not the earlier API's
	// accessKey|secretKey JWT.
	const apiKey = "kling-api-key"
	decode := func(t *testing.T, decoder []string, model string, body map[string]any) (any, error) {
		t.Helper()
		return plugin.Engine.CallPath(t.Context(), decoder[0], decoder[1:], map[string]any{"model": model, "params": map[string]any{"model": model}, "body": map[string]any{"kind": "json", "value": body}})
	}
	validateWith := func(t *testing.T, key string, decoder []string, model string, body map[string]any) (*taskplugin.TaskAdaptor, *gin.Context, *relaycommon.RelayInfo, any, *dto.TaskError) {
		t.Helper()
		value, err := decode(t, decoder, model, body)
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api-singapore.klingai.com", ApiKey: key, UpstreamModelName: model},
			OriginModelName: model,
			TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		}
		adaptor := taskplugin.New(plugin)
		adaptor.Init(info)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		requestBody := alibabaObject(t, value)["requestBody"]
		c.Set("task_request", requestBody)
		return adaptor, c, info, requestBody, adaptor.ValidateRequestAndSetAction(c, info)
	}
	validate := func(t *testing.T, decoder []string, model string, body map[string]any) (*taskplugin.TaskAdaptor, *gin.Context, *relaycommon.RelayInfo, any, *dto.TaskError) {
		t.Helper()
		return validateWith(t, apiKey, decoder, model, body)
	}
	submit := func(t *testing.T, decoder []string, model string, body map[string]any) submission {
		t.Helper()
		adaptor, c, info, requestBody, taskErr := validate(t, decoder, model, body)
		require.Nil(t, taskErr)
		reader, err := adaptor.BuildRequestBody(c, info)
		require.NoError(t, err)
		encoded, err := io.ReadAll(reader)
		require.NoError(t, err)
		var sent map[string]any
		require.NoError(t, common.Unmarshal(encoded, &sent))
		url, err := adaptor.BuildRequestURL(info)
		require.NoError(t, err)
		facts, err := adaptor.ExtractUsageFactsValidated(c, info)
		require.NoError(t, err)
		parsed := call(t, "parseSubmitResponse", map[string]any{"model": model, "requestBody": requestBody},
			map[string]any{"statusCode": 200, "body": map[string]any{"code": 0, "message": "SUCCEED", "data": map[string]any{"id": "kling_4", "status": "submitted"}}})
		assert.Equal(t, "kling_4", parsed["taskId"])
		state, _ := parsed["state"].(map[string]any)
		return submission{url: url, body: sent, units: facts["units"].(float64), state: state}
	}
	textToVideo := []string{"native", "pathTextToVideo"}
	imageToVideo := []string{"native", "pathImageToVideo"}
	omniVideo := []string{"native", "pathOmniVideo"}
	endpoints := map[string]string{"pathTextToVideo": "text-to-video", "pathImageToVideo": "image-to-video", "pathOmniVideo": "omni-video"}

	// Kling 4.0 and 4.0 Flash bill units per second of a total duration: the
	// output, or, with an input video, twice the output or input plus output.
	// Kling picks their length when the request has none, and only Kling knows
	// how long an input video is, so the longest of each is reserved. The
	// other models bill the output at the rate of their resolution and of
	// audio, a reference voice or a reference video, for their default
	// duration when the request has none.
	t.Run("native bodies and their reservation", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			decoder   []string
			model     string
			body      map[string]any
			wantUnits float64
		}{
			{"4.0 text at Kling's defaults", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat"}, 45},
			{"4.0 text at 720p for 10 seconds", textToVideo, "kling-4.0",
				map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "720p", "aspect_ratio": "9:16", "duration": 10}}, 10},
			{"4.0 image at 1080p for 5 seconds", imageToVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "a cat"}, map[string]any{"type": "first_frame", "url": "https://cdn.example/a.png"}},
					"settings": map[string]any{"duration": "5"}}, 7.5},
			{"4.0 omni with reference images only", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "<<<image_1>>> walks"}, map[string]any{"type": "refer_image", "url": "https://cdn.example/a.png", "id": "image_1"}},
					"settings": map[string]any{"resolution": "720p", "duration": 10, "aspect_ratio": "adaptive"}}, 10},
			{"4.0 omni with a reference video", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "in the style of <<<video_1>>>"}, map[string]any{"type": "feature_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"duration": 10}}, 36},
			{"4.0 omni editing a video of unknown length", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "make it night"}, map[string]any{"type": "base_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"resolution": "720p"}}, 36},
			{"4.0 omni with an element", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "<<<element_1>>> waves"}, map[string]any{"type": "element", "element_id": "123", "id": "element_1"}},
					"settings": map[string]any{"resolution": "720p", "duration": 30}}, 36},
			{"4.0 text in 4K for 3 seconds", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "4k", "duration": 3}}, 10.5},
			{"4.0 text in 4K at Kling's length", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "4k"}}, 105},
			{"4.0 omni in 4K with a reference video", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "like <<<video_1>>>"}, map[string]any{"type": "feature_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"resolution": "4k", "duration": 3}}, 69.3},
			{"4.0 HDR at 1080p for 3 seconds", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"dynamic_range": "hdr", "duration": 3}}, 5.4},
			{"4.0 HDR in 4K for 3 seconds", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "4k", "dynamic_range": "hdr", "duration": 3}}, 12},
			{"4.0 HDR in 4K with a reference video, HDR on the output alone", omniVideo, "kling-4.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "like <<<video_1>>>"}, map[string]any{"type": "feature_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"resolution": "4k", "dynamic_range": "hdr", "duration": 3}}, 70.8},
			{"4.0 SDR named", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"dynamic_range": "sdr", "duration": 3}}, 4.5},
			{"4.0 Flash text at Kling's defaults", textToVideo, "kling-4.0-flash", map[string]any{"prompt": "a cat"}, 12},
			{"4.0 Flash omni with a reference video", omniVideo, "kling-4.0-flash",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "like @video_1"}, map[string]any{"type": "feature_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"duration": 10}}, 12},
			{"3.0 text at Kling's defaults", textToVideo, "kling-3.0", map[string]any{"prompt": "a cat"}, 3},
			{"3.0 text with audio at 1080p", textToVideo, "kling-3.0",
				map[string]any{"prompt": "a cat", "settings": map[string]any{"audio": "native", "resolution": "1080p", "duration": 10}}, 12},
			{"3.0 image in 4k", imageToVideo, "kling-3.0",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "a cat"}, map[string]any{"type": "first_frame", "url": "https://cdn.example/a.png"}},
					"settings": map[string]any{"resolution": "4k"}}, 15},
			{"3.0 Omni with a reference video, whatever the audio", omniVideo, "kling-3.0-omni",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "like @video_1"}, map[string]any{"type": "feature_video", "url": "https://cdn.example/a.mp4", "id": "video_1"}},
					"settings": map[string]any{"resolution": "1080p", "audio": "original"}}, 6},
			{"3.0 Omni with audio and a reference image", omniVideo, "kling-3.0-omni",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "@image_1 walks"}, map[string]any{"type": "refer_image", "url": "https://cdn.example/a.png", "id": "image_1"}},
					"settings": map[string]any{"audio": "native"}}, 4},
			{"3.0 Turbo at 1080p", textToVideo, "kling-3.0-turbo", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "1080p"}}, 5},
			{"2.6 image with audio and a reference voice", imageToVideo, "kling-2.6",
				map[string]any{"contents": []any{map[string]any{"type": "prompt", "text": "a cat"}, map[string]any{"type": "first_frame", "url": "https://cdn.example/a.png"}, map[string]any{"type": "voice", "voice_id": "v1", "id": "voice_1"}},
					"settings": map[string]any{"audio": "native", "resolution": "1080p", "duration": 10}}, 12},
			{"2.6 audio at 720p, which Kling prices only at 1080p", textToVideo, "kling-2.6", map[string]any{"prompt": "a cat", "settings": map[string]any{"audio": "native"}}, 5},
			{"2.5 Turbo at 1080p for 10 seconds", textToVideo, "kling-2.5-turbo", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "1080p", "duration": 10}}, 5},
		} {
			t.Run(tc.name, func(t *testing.T) {
				sent := submit(t, tc.decoder, tc.model, tc.body)
				assert.Equal(t, "https://api-singapore.klingai.com/"+endpoints[tc.decoder[1]]+"/"+tc.model, sent.url)
				assert.Equal(t, decodePluginValue(t, tc.body), sent.body, "Kling receives the body as the client wrote it")
				assert.InDelta(t, tc.wantUnits, sent.units, 1e-9)
			})
		}

		for _, tc := range []struct {
			name    string
			decoder []string
			model   string
			body    map[string]any
			wantErr string
		}{
			{"callbacks carry Kling's task ids", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "options": map[string]any{"callback_url": "https://client.example/hook"}}, "options.callback_url is not supported"},
			{"external ids are not looked up", textToVideo, "kling-3.0", map[string]any{"prompt": "a cat", "options": map[string]any{"external_task_id": "mine-1"}}, "options.external_task_id is not supported"},
			{"a duration above Kling 4.0's 30 seconds", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": 31}}, "duration must be an integer from 3 to 30"},
			{"a duration below its 3 seconds", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": "2"}}, "duration must be an integer from 3 to 30"},
			{"a fractional duration", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": "5.5"}}, "duration must be an integer from 3 to 30"},
			{"a duration above Kling 4.0 Flash's 20 seconds", textToVideo, "kling-4.0-flash", map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": 21}}, "duration must be an integer from 3 to 20"},
			{"a duration above Kling 3.0's 15 seconds", imageToVideo, "kling-3.0", map[string]any{"contents": []any{}, "settings": map[string]any{"duration": 16}}, "duration must be an integer from 3 to 15"},
			{"a duration Kling 2.6 does not make", textToVideo, "kling-2.6", map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": 7}}, "duration must be one of 5, 10"},
			{"a resolution 3.0 Turbo does not make", textToVideo, "kling-3.0-turbo", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "4k"}}, "settings.resolution must be one of 720p, 1080p"},
			{"a resolution 4.0 does not make", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "2k"}}, "settings.resolution must be one of 1080p, 720p, 4k"},
			{"a resolution 4.0 Flash does not make", textToVideo, "kling-4.0-flash", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "1080p"}}, "settings.resolution must be one of 720p"},
			{"HDR at 720p", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "720p", "dynamic_range": "hdr"}}, "settings.dynamic_range hdr needs settings.resolution 1080p or 4k"},
			{"HDR on 4.0 Flash", textToVideo, "kling-4.0-flash", map[string]any{"prompt": "a cat", "settings": map[string]any{"dynamic_range": "hdr"}}, "settings.dynamic_range hdr is not available for this model"},
			{"a dynamic range Kling does not make", textToVideo, "kling-4.0", map[string]any{"prompt": "a cat", "settings": map[string]any{"dynamic_range": "dolby"}}, "settings.dynamic_range must be sdr or hdr"},
			{"a model the endpoint does not serve", textToVideo, "kling-3.0-omni", map[string]any{"prompt": "a cat"}, "Kling does not serve kling-3.0-omni at /text-to-video"},
			{"an earlier API model name", omniVideo, "kling-v3", map[string]any{"contents": []any{}}, "Kling does not serve kling-v3 at /omni-video"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := decode(t, tc.decoder, tc.model, tc.body)
				assert.ErrorContains(t, err, tc.wantErr)
			})
		}
	})

	// The model-in-path API refuses the earlier API's accessKey|secretKey JWT,
	// so a channel with such a key is told to use an API key before anything
	// is sent; the earlier models still sign one.
	t.Run("an accessKey|secretKey channel", func(t *testing.T) {
		_, _, _, _, taskErr := validateWith(t, "ak|sk", textToVideo, "kling-3.0", map[string]any{"prompt": "a cat"})
		require.NotNil(t, taskErr)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		assert.ErrorContains(t, taskErr.Error, "this Kling model takes an API key, not accessKey|secretKey")
		_, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", map[string]any{"taskId": "kling_4", "baseUrl": "https://api-singapore.klingai.com", "apiKey": "ak|sk", "state": map[string]any{"resolution": "1080p"}})
		assert.ErrorContains(t, err, "this Kling model takes an API key")
		query := call(t, "buildQueryRequest", map[string]any{"taskId": "old_1", "action": "text_to_video", "baseUrl": "https://api-singapore.klingai.com", "apiKey": "ak|sk"})
		assert.NotEqual(t, "Bearer ak|sk", query["headers"].(map[string]any)["Authorization"], "the earlier API gets a signed JWT")
	})

	// A model-in-path task is looked up at GET /tasks?task_ids=, which answers
	// a list; the state the submission saved marks the task as one.
	t.Run("query and result", func(t *testing.T) {
		query := call(t, "buildQueryRequest", map[string]any{"taskId": "kling_4", "baseUrl": "https://api-singapore.klingai.com", "apiKey": apiKey, "state": map[string]any{"resolution": "1080p"}})
		assert.Equal(t, "https://api-singapore.klingai.com/tasks?task_ids=kling_4", query["url"])
		assert.Equal(t, "GET", query["method"])
		assert.Equal(t, "Bearer "+apiKey, query["headers"].(map[string]any)["Authorization"])
		query = call(t, "buildQueryRequest", map[string]any{"taskId": "task_x", "baseUrl": "https://gateway.example", "apiKey": "sk-relay", "state": map[string]any{"resolution": "720p"}})
		assert.Equal(t, "https://gateway.example/kling/tasks?task_ids=task_x", query["url"], "another gateway serves Kling under /kling")
		query = call(t, "buildQueryRequest", map[string]any{"taskId": "old_1", "action": "text_to_video", "baseUrl": "https://api-singapore.klingai.com", "apiKey": "ak|sk"})
		assert.Equal(t, "https://api-singapore.klingai.com/v1/videos/text2video/old_1", query["url"], "earlier models keep their query path")

		// A gateway Base URL that already ends with /kling gets no second one.
		for _, baseURL := range []string{"https://api.ephone.ai/kling", "https://api.ephone.ai/kling/", "https://api.ephone.ai/KLING"} {
			query = call(t, "buildQueryRequest", map[string]any{"taskId": "task_x", "baseUrl": baseURL, "apiKey": "sk-ep-relay", "state": map[string]any{"resolution": "1080p"}})
			assert.Equal(t, strings.TrimSuffix(baseURL, "/")+"/tasks?task_ids=task_x", query["url"], baseURL)
			submitted := call(t, "buildSubmitRequest", map[string]any{"baseUrl": baseURL, "apiKey": "sk-ep-relay", "model": "kling-4.0",
				"requestBody": map[string]any{"model": "kling-4.0", "endpoint": "text-to-video", "vendorBody": map[string]any{"prompt": "a cat"}}})
			assert.Equal(t, strings.TrimSuffix(baseURL, "/")+"/text-to-video/kling-4.0", submitted["url"], baseURL)
			legacy := call(t, "buildQueryRequest", map[string]any{"taskId": "old_1", "action": "text_to_video", "baseUrl": baseURL, "apiKey": "sk-ep-relay"})
			assert.Equal(t, strings.TrimSuffix(baseURL, "/")+"/v1/videos/text2video/old_1", legacy["url"], baseURL)
		}
		submitted := call(t, "buildSubmitRequest", map[string]any{"baseUrl": "https://api.ephone.ai/", "apiKey": "sk-ep-relay", "model": "kling-4.0",
			"requestBody": map[string]any{"model": "kling-4.0", "endpoint": "text-to-video", "vendorBody": map[string]any{"prompt": "a cat"}}})
		assert.Equal(t, "https://api.ephone.ai/kling/text-to-video/kling-4.0", submitted["url"], "a gateway root still gets /kling")

		// A page instead of Kling's JSON, as a Base URL off Kling's API
		// answers, names what came back.
		_, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", map[string]any{"model": "kling-4.0"},
			map[string]any{"statusCode": 200, "headers": map[string]any{"Content-Type": []any{"text/html; charset=utf-8"}}, "body": "<!doctype html><html></html>"})
		assert.ErrorContains(t, err, "the upstream answered HTTP 200 text/html; charset=utf-8 instead of Kling's JSON; check the channel Base URL")

		ctx := map[string]any{"taskId": "kling_4", "model": "kling-4.0", "state": map[string]any{"resolution": "1080p"}}
		reply := func(entry map[string]any) map[string]any {
			return map[string]any{"code": 0, "message": "SUCCEED", "data": []any{map[string]any{"id": "other", "status": "processing"}, entry}}
		}
		parsed := call(t, "parseTaskResult", ctx, reply(map[string]any{"id": "kling_4", "status": "succeeded",
			"outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4", "duration": "10"}},
			"billing": []any{map[string]any{"charge_type": "unit", "amount": "14.5", "package_type": "video"}},
		}), map[string]any{"status": 200})
		assert.Equal(t, "SUCCESS", parsed["status"])
		assert.Equal(t, "kling_4", parsed["taskId"], "the task's own entry is read, not the first")
		assert.Equal(t, "https://cdn.example/out.mp4", parsed["url"])
		assert.Equal(t, float64(15), parsed["completionTokens"])
		assert.Equal(t, float64(15), parsed["totalTokens"])

		for _, tc := range []struct{ status, want string }{{"submitted", "SUBMITTED"}, {"processing", "IN_PROGRESS"}, {"failed", "FAILURE"}, {"paused", "UNKNOWN"}} {
			parsed := call(t, "parseTaskResult", ctx, reply(map[string]any{"id": "kling_4", "status": tc.status, "message": "content check failed"}), map[string]any{"status": 200})
			assert.Equal(t, tc.want, parsed["status"], tc.status)
			assert.Nil(t, parsed["url"])
			assert.Nil(t, parsed["completionTokens"])
		}
		failed := call(t, "parseTaskResult", ctx, reply(map[string]any{"id": "kling_4", "status": "failed", "message": "content check failed"}), map[string]any{"status": 200})
		assert.Equal(t, "content check failed", failed["reason"])

		task := map[string]any{"status": "SUCCESS", "taskId": "kling_4", "state": map[string]any{"resolution": "1080p"},
			"data": reply(map[string]any{"id": "kling_4", "status": "succeeded", "outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4"}}})}
		artifacts, err := plugin.Engine.Call(t.Context(), "listArtifacts", task)
		require.NoError(t, err)
		encoded, err := common.Marshal(artifacts)
		require.NoError(t, err)
		assert.JSONEq(t, `[{"key":"video","type":"video"}]`, string(encoded))
		content := call(t, "buildContentRequest", map[string]any{"artifactKey": "video", "taskId": "kling_4", "state": task["state"], "data": task["data"], "clientRequest": map[string]any{"method": "GET"}})
		assert.Equal(t, "https://cdn.example/out.mp4", content["url"])
	})

	// Settles on the deducted units: a package's amount, or a CNY cash
	// deduction's list price, since a unit lists at one yuan. Without one that
	// converts, Kling 4.0 and 4.0 Flash settle on the durations Kling bills and
	// the other models keep their reservation.
	t.Run("settlement", func(t *testing.T) {
		// usdBalance is how Kling reports a USD account's deduction, measured on
		// a 3-second 4K task; it has no rate to units, and the reply names
		// neither the resolution nor the dynamic range.
		usdBalance := []any{map[string]any{"charge_type": "cash", "cash_type": "balance", "currency": "USD", "amount": "1.647", "list_price": "1.647"}}
		for _, tc := range []struct {
			name       string
			model      string
			resolution string
			entry      map[string]any
			wantUnits  any
			// hdr marks a task submitted with settings.dynamic_range "hdr".
			hdr bool
		}{
			{"resource package units", "kling-4.0", "1080p", map[string]any{"billing": []any{
				map[string]any{"charge_type": "unit", "amount": "6", "package_type": "video"},
				map[string]any{"charge_type": "unit", "amount": "4.5", "package_type": "video"},
			}}, 10.5, false},
			{"cash without an input video", "kling-4.0", "720p", map[string]any{"billing": []any{map[string]any{"charge_type": "cash", "amount": "10", "currency": "CNY"}}}, 10.0, false},
			{"no billing at 1080p", "kling-4.0", "1080p", map[string]any{}, 15.0, false},
			{"part cash, input shorter than the output", "kling-4.0", "1080p", map[string]any{"usage": map[string]any{"input_duration": "4"}, "billing": []any{
				map[string]any{"charge_type": "unit", "amount": "3", "package_type": "video"},
				map[string]any{"charge_type": "cash", "amount": "15", "currency": "CNY"},
			}}, 18.0, false},
			{"cash, input longer than the output", "kling-4.0", "1080p", map[string]any{"usage": map[string]any{"input_duration": 25}, "billing": []any{map[string]any{"charge_type": "cash", "amount": "31.5", "currency": "CNY"}}}, 31.5, false},
			{"cash at 720p with an input video", "kling-4.0", "720p", map[string]any{"usage": map[string]any{"input_duration": "10"}}, 12.0, false},
			{"balance at its list price, not the reported duration", "kling-4.0", "720p", map[string]any{
				"outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4", "duration": "3.041"}},
				"billing": []any{map[string]any{"charge_type": "cash", "cash_type": "balance", "amount": "2.55", "list_price": "3", "currency": "CNY"}},
			}, 3.0, false},
			{"a USD list price has no rate to units", "kling-4.0", "720p", map[string]any{
				"usage":   map[string]any{"output_duration": "5"},
				"billing": []any{map[string]any{"charge_type": "cash", "cash_type": "balance", "amount": "0.70", "list_price": "0.785", "currency": "USD"}},
			}, 5.0, false},
			{"the billed duration, not the video's own", "kling-4.0", "1080p", map[string]any{
				"usage":   map[string]any{"output_duration": "10"},
				"outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4", "duration": "10.041"}},
			}, 15.0, false},
			{"4.0 Flash on its billed duration", "kling-4.0-flash", "720p", map[string]any{"usage": map[string]any{"output_duration": "10"}}, 6.0, false},
			{"4K on its billed duration", "kling-4.0", "4k", map[string]any{"usage": map[string]any{"output_duration": "3"}, "billing": usdBalance}, 10.5, false},
			{"4K with a shorter input video", "kling-4.0", "4k", map[string]any{"usage": map[string]any{"output_duration": "3", "input_duration": "3"}, "billing": usdBalance}, 12.6, false},
			{"HDR at 1080p on its billed duration", "kling-4.0", "1080p", map[string]any{"usage": map[string]any{"output_duration": "3"}, "billing": usdBalance}, 5.4, true},
			{"HDR in 4K on its billed duration", "kling-4.0", "4k", map[string]any{"usage": map[string]any{"output_duration": "3"}, "billing": usdBalance}, 12.0, true},
			{"HDR in 4K with a longer input video, HDR on the output alone", "kling-4.0", "4k", map[string]any{"usage": map[string]any{"output_duration": "5", "input_duration": "6"}}, 25.6, true},
			{"HDR in package units as deducted", "kling-4.0", "4k", map[string]any{"billing": []any{map[string]any{"charge_type": "unit", "amount": "12", "package_type": "video"}}}, 12.0, true},
			{"3.0 on its package units", "kling-3.0", "1080p", map[string]any{"billing": []any{map[string]any{"charge_type": "unit", "amount": "4", "package_type": "video"}}}, 4.0, false},
			{"3.0 without a deduction keeps its reservation", "kling-3.0", "1080p", map[string]any{}, nil, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				entry := map[string]any{"id": "kling_4", "status": "succeeded", "outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4", "duration": "10"}}}
				for key, value := range tc.entry {
					entry[key] = value
				}
				state := map[string]any{"resolution": tc.resolution}
				if tc.hdr {
					state["dynamicRange"] = "hdr"
				}
				facts := call(t, "extractUsageOnComplete", map[string]any{"taskId": "kling_4", "model": tc.model, "state": state}, map[string]any{"status": "SUCCESS"},
					map[string]any{"code": 0, "data": []any{entry}})
				if tc.wantUnits == nil {
					assert.Nil(t, facts)
					return
				}
				assert.InDelta(t, tc.wantUnits, facts["units"], 1e-9)
			})
		}
		running := call(t, "extractUsageOnComplete", map[string]any{"taskId": "kling_4", "model": "kling-4.0", "state": map[string]any{"resolution": "1080p"}}, map[string]any{"status": "IN_PROGRESS"},
			map[string]any{"code": 0, "data": []any{map[string]any{"id": "kling_4", "status": "processing"}}})
		assert.Nil(t, running, "an unfinished task keeps its reservation")
	})

	// The host protocols send text-to-video, or image-to-video with the first
	// frame; size picks the resolution. Without a duration Kling 4.0 picks the
	// length, so the longest is reserved.
	t.Run("host protocols", func(t *testing.T) {
		openaiVideo := []string{"protocols", "openai_video", "decodeRequest"}
		sent := submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat"})
		assert.Equal(t, "https://api-singapore.klingai.com/text-to-video/kling-4.0", sent.url)
		assert.Equal(t, map[string]any{"prompt": "a cat"}, sent.body)
		assert.InDelta(t, 45.0, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "1080p"}, sent.state)

		sent = submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": "720x1280", "seconds": "10", "metadata": map[string]any{"mode": "pro", "settings": map[string]any{"duration": 10}}})
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": float64(10)}}, sent.body, "metadata adds Kling's own fields")
		assert.InDelta(t, 15.0, sent.units, 1e-9)

		sent = submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": "1280x720", "seconds": 5})
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "720p", "aspect_ratio": "16:9", "duration": float64(5)}}, sent.body)
		assert.InDelta(t, 5.0, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "720p"}, sent.state)

		// Any size gets the nearest of Kling 4.0's aspect ratios, and 720p or
		// 1080p by its shorter side; image-to-video has no aspect ratio.
		for _, tc := range []struct {
			size     string
			image    string
			settings map[string]any
		}{
			{"720x720", "", map[string]any{"resolution": "720p", "aspect_ratio": "1:1", "duration": float64(5)}},
			{"1792x1024", "", map[string]any{"resolution": "1080p", "aspect_ratio": "16:9", "duration": float64(5)}},
			{"1024x1792", "", map[string]any{"resolution": "1080p", "aspect_ratio": "9:16", "duration": float64(5)}},
			{"2560x1080", "", map[string]any{"resolution": "1080p", "aspect_ratio": "21:9", "duration": float64(5)}},
			{"1440x1080", "", map[string]any{"resolution": "1080p", "aspect_ratio": "4:3", "duration": float64(5)}},
			{"1080x1440", "", map[string]any{"resolution": "1080p", "aspect_ratio": "3:4", "duration": float64(5)}},
			{"720x1280", "https://cdn.example/a.png", map[string]any{"resolution": "720p", "duration": float64(5)}},
		} {
			body := map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": tc.size, "seconds": 5}
			if tc.image != "" {
				body["input_reference"] = tc.image
			}
			sent = submit(t, openaiVideo, "kling-4.0", body)
			assert.Equal(t, tc.settings, sent.body["settings"], tc.size)
		}

		// A 4K size asks Kling 4.0 for 4K. HDR has no host-protocol field, so it
		// comes in metadata.settings, which replaces the computed settings and
		// is billed and checked like a native body.
		sent = submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": "3840x2160", "seconds": 5})
		assert.Equal(t, map[string]any{"resolution": "4k", "aspect_ratio": "16:9", "duration": float64(5)}, sent.body["settings"])
		assert.InDelta(t, 17.5, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "4k"}, sent.state)
		sent = submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat",
			"metadata": map[string]any{"settings": map[string]any{"resolution": "4k", "dynamic_range": "hdr", "duration": 3}}})
		assert.Equal(t, map[string]any{"resolution": "4k", "dynamic_range": "hdr", "duration": float64(3)}, sent.body["settings"])
		assert.InDelta(t, 12.0, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "4k", "dynamicRange": "hdr"}, sent.state, "settlement needs the HDR the reply does not report")
		_, _, _, _, hdrErr := validate(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": "1280x720",
			"metadata": map[string]any{"settings": map[string]any{"resolution": "720p", "dynamic_range": "hdr"}}})
		require.NotNil(t, hdrErr)
		assert.Equal(t, http.StatusBadRequest, hdrErr.StatusCode)
		assert.ErrorContains(t, hdrErr.Error, "settings.dynamic_range hdr needs settings.resolution 1080p or 4k")

		// Kling 4.0 makes 3 to 30 seconds; another duration is refused before
		// anything is reserved.
		for _, seconds := range []any{2, 31, "45"} {
			_, _, _, _, taskErr := validate(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "seconds": seconds})
			require.NotNil(t, taskErr, seconds)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.ErrorContains(t, taskErr.Error, "duration must be an integer from 3 to 30")
		}
		_, _, _, _, taskErr := validate(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "size": "720p"})
		require.NotNil(t, taskErr)
		assert.ErrorContains(t, taskErr.Error, "size must be WIDTHxHEIGHT")

		sent = submit(t, openaiVideo, "kling-4.0", map[string]any{"model": "kling-4.0", "prompt": "a cat", "input_reference": "https://cdn.example/a.png", "seconds": 5,
			"metadata": map[string]any{"image_tail": "https://cdn.example/b.png"}})
		assert.Equal(t, "https://api-singapore.klingai.com/image-to-video/kling-4.0", sent.url)
		assert.Equal(t, map[string]any{"contents": []any{
			map[string]any{"type": "prompt", "text": "a cat"},
			map[string]any{"type": "first_frame", "url": "https://cdn.example/a.png"},
			map[string]any{"type": "last_frame", "url": "https://cdn.example/b.png"},
		}, "settings": map[string]any{"duration": float64(5)}}, sent.body)
		assert.InDelta(t, 7.5, sent.units, 1e-9)

		sent = submit(t, []string{"protocols", "openai_responses", "decodeRequest"}, "kling-4.0", map[string]any{"model": "kling-4.0", "input": "a cat", "seconds": 10})
		assert.Equal(t, "https://api-singapore.klingai.com/text-to-video/kling-4.0", sent.url)
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": float64(10)}}, sent.body)
		assert.InDelta(t, 15.0, sent.units, 1e-9)

		// The 2.x and 3.x models make their default duration without one, so
		// it is sent; the earlier API's pro and 4k modes ask for that
		// resolution and sound "on" for native audio.
		sent = submit(t, openaiVideo, "kling-3.0", map[string]any{"model": "kling-3.0", "prompt": "a cat"})
		assert.Equal(t, "https://api-singapore.klingai.com/text-to-video/kling-3.0", sent.url)
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"duration": float64(5)}}, sent.body)
		assert.InDelta(t, 3.0, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "720p"}, sent.state)

		sent = submit(t, openaiVideo, "kling-3.0", map[string]any{"model": "kling-3.0", "prompt": "a cat", "mode": "pro", "seconds": 10, "metadata": map[string]any{"sound": "on"}})
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "1080p", "audio": "native", "duration": float64(10)}}, sent.body)
		assert.InDelta(t, 12.0, sent.units, 1e-9)
		assert.Equal(t, map[string]any{"resolution": "1080p"}, sent.state)

		sent = submit(t, openaiVideo, "kling-3.0", map[string]any{"model": "kling-3.0", "prompt": "a cat", "size": "3840x2160"})
		assert.Equal(t, map[string]any{"resolution": "4k", "aspect_ratio": "16:9", "duration": float64(5)}, sent.body["settings"])
		assert.InDelta(t, 15.0, sent.units, 1e-9)

		// 3.0 Omni has only the omni endpoint, which takes the same contents.
		sent = submit(t, openaiVideo, "kling-3.0-omni", map[string]any{"model": "kling-3.0-omni", "prompt": "a cat", "input_reference": "https://cdn.example/a.png"})
		assert.Equal(t, "https://api-singapore.klingai.com/omni-video/kling-3.0-omni", sent.url)
		assert.Equal(t, map[string]any{"contents": []any{
			map[string]any{"type": "prompt", "text": "a cat"},
			map[string]any{"type": "first_frame", "url": "https://cdn.example/a.png"},
		}, "settings": map[string]any{"duration": float64(5)}}, sent.body)
		assert.InDelta(t, 3.0, sent.units, 1e-9)

		// Kling 4.0 Flash makes only 720p and picks its length like 4.0.
		sent = submit(t, openaiVideo, "kling-4.0-flash", map[string]any{"model": "kling-4.0-flash", "prompt": "a cat", "size": "1920x1080"})
		assert.Equal(t, "https://api-singapore.klingai.com/text-to-video/kling-4.0-flash", sent.url)
		assert.Equal(t, map[string]any{"prompt": "a cat", "settings": map[string]any{"resolution": "720p", "aspect_ratio": "16:9"}}, sent.body)
		assert.InDelta(t, 12.0, sent.units, 1e-9)

		_, _, _, _, taskErr = validate(t, openaiVideo, "kling-2.6", map[string]any{"model": "kling-2.6", "prompt": "a cat", "seconds": 7})
		require.NotNil(t, taskErr)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		assert.ErrorContains(t, taskErr.Error, "duration must be one of 5, 10")

		rendered, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "render"}, map[string]any{}, map[string]any{"task_id": "task_public", "status": "SUCCESS", "progress": "100%",
			"data": map[string]any{"code": 0, "message": "SUCCEED", "data": []any{map[string]any{"id": "kling_4", "status": "succeeded", "create_time": 1700000000000, "update_time": 1700000060000,
				"outputs": []any{map[string]any{"type": "video", "url": "https://cdn.example/out.mp4", "duration": "10"}}}}}})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"id": "task_public", "object": "video", "model": "", "status": "completed", "progress": float64(100),
			"created_at": float64(1700000000000), "completed_at": float64(1700000060000), "seconds": "10"}, alibabaObject(t, rendered))
	})

	// Native replies keep the model-in-path envelope with the gateway's task
	// id.
	t.Run("native replies", func(t *testing.T) {
		created, err := plugin.Engine.CallPath(t.Context(), "native", []string{"pathTaskCreated"}, map[string]any{}, map[string]any{"task_id": "task_public",
			"data": map[string]any{"code": 0, "message": "SUCCEED", "request_id": "req-1", "data": map[string]any{"id": "kling_4", "status": "submitted"}}})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"code": float64(0), "message": "SUCCEED", "request_id": "req-1", "data": map[string]any{"id": "task_public", "status": "submitted"}}, alibabaObject(t, created))

		tasks := func(task map[string]any) map[string]any {
			t.Helper()
			value, err := plugin.Engine.CallPath(t.Context(), "native", []string{"pathTasks"}, map[string]any{}, task)
			require.NoError(t, err)
			return alibabaObject(t, value)
		}
		processing := map[string]any{"code": 0, "message": "SUCCEED", "request_id": "req-2", "data": []any{map[string]any{"id": "kling_4", "status": "processing", "create_time": 1700000000000}}}
		assert.Equal(t, map[string]any{"code": float64(0), "message": "SUCCEED", "request_id": "req-2", "data": []any{
			map[string]any{"id": "task_public", "status": "failed", "message": "task timed out", "create_time": float64(1700000000000), "update_time": float64(1700000060000)},
		}}, tasks(map[string]any{"task_id": "task_public", "status": "FAILURE", "fail_reason": "task timed out", "created_at": 1700000000, "updated_at": 1700000060, "data": processing}),
			"a task the gateway failed does not read as running")
		assert.Equal(t, map[string]any{"code": float64(0), "message": "", "request_id": "", "data": []any{
			map[string]any{"id": "task_public", "status": "submitted", "create_time": float64(1700000000000), "update_time": float64(1700000000000)},
		}}, tasks(map[string]any{"task_id": "task_public", "status": "SUBMITTED", "created_at": 1700000000}), "a task without a reply yet")

		status, err := plugin.Engine.CallPath(t.Context(), "native", []string{"taskStatus"}, map[string]any{},
			map[string]any{"task_id": "task_public", "status": "IN_PROGRESS", "created_at": 1700000000, "data": processing})
		require.NoError(t, err)
		data, _ := alibabaObject(t, status)["data"].(map[string]any)
		assert.Equal(t, "processing", data["task_status"], "the earlier query route does not pass a model-in-path reply off as its own")
	})
}
