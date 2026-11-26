package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadMigaPlugin(t *testing.T) *jsplugin.LoadedPlugin {
	t.Helper()
	source, err := builtinplugins.Source("miga")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "miga"})
	require.NoError(t, err)
	return plugin
}

// callMiga calls a top-level hook, or a native member when members are given,
// and returns the result as plain JSON.
func callMiga(t *testing.T, plugin *jsplugin.LoadedPlugin, members []string, args ...any) any {
	t.Helper()
	var value any
	var err error
	if len(members) > 0 {
		value, err = plugin.Engine.CallPath(t.Context(), "native", members, args...)
	} else {
		value, err = plugin.Engine.Call(t.Context(), args[0].(string), args[1:]...)
	}
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	return decoded
}

func migaBody(changes map[string]any) map[string]any {
	body := map[string]any{
		"model":       "magi-2",
		"parts":       []any{map[string]any{"type": "text", "text": "a cat walks"}},
		"duration":    4,
		"aspectRatio": "16:9",
	}
	for key, value := range changes {
		if value == nil {
			delete(body, key)
			continue
		}
		body[key] = value
	}
	return body
}

func TestMigaRefusesRequestsDMCWouldReject(t *testing.T) {
	plugin := loadMigaPlugin(t)
	image := map[string]any{"type": "reference_image", "image": map[string]any{"url": "https://example.com/a.jpg"}}
	text := map[string]any{"type": "text", "text": "a cat walks"}

	intent := callMiga(t, plugin, []string{"createGeneration"}, map[string]any{"body": map[string]any{"kind": "json", "value": migaBody(nil)}}).(map[string]any)
	assert.Equal(t, "submit", intent["kind"])
	assert.Equal(t, "magi-2", intent["model"])
	assert.Equal(t, "text_to_video", intent["action"])
	intent = callMiga(t, plugin, []string{"createGeneration"}, map[string]any{"body": map[string]any{"kind": "json", "value": migaBody(map[string]any{"parts": []any{text, image}, "seed": 7, "generateAudio": false})}}).(map[string]any)
	assert.Equal(t, "image_to_video", intent["action"])

	// The body is forwarded as the client sent it, so a value of another
	// JSON type is refused here, before billing, instead of upstream.
	for name, testCase := range map[string]struct {
		changes map[string]any
		want    string
	}{
		"duration as a string":   {map[string]any{"duration": "5"}, "duration must be an integer from 4 to 30 seconds"},
		"duration above 30":      {map[string]any{"duration": 31}, "duration must be an integer from 4 to 30 seconds"},
		"aspect ratio padded":    {map[string]any{"aspectRatio": " 16:9"}, "aspectRatio must be one of"},
		"seed as a boolean":      {map[string]any{"seed": true}, "seed must be an integer"},
		"seed as a string":       {map[string]any{"seed": "7"}, "seed must be an integer"},
		"text as a number":       {map[string]any{"parts": []any{map[string]any{"type": "text", "text": 123}}}, "parts[0].text must be a non-empty string"},
		"padded reference URL":   {map[string]any{"parts": []any{text, map[string]any{"type": "reference_image", "image": map[string]any{"url": " https://example.com/a.jpg"}}}}, "parts[1].image.url must be an https URL"},
		"http reference URL":     {map[string]any{"parts": []any{text, map[string]any{"type": "reference_video", "video": map[string]any{"url": "http://example.com/a.mp4"}}}}, "parts[1].video.url must be an https URL"},
		"two text items":         {map[string]any{"parts": []any{text, text}}, "parts must contain exactly one text item"},
		"resolution other than":  {map[string]any{"resolution": "720p"}, "resolution must be 1080p"},
		"model missing":          {map[string]any{"model": nil}, "model must be magi-2"},
		"generateAudio a string": {map[string]any{"generateAudio": "yes"}, "generateAudio must be a boolean"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := plugin.Engine.CallPath(t.Context(), "native", []string{"createGeneration"}, map[string]any{"body": map[string]any{"kind": "json", "value": migaBody(testCase.changes)}})
			require.ErrorContains(t, err, testCase.want)
		})
	}
}

func TestMigaSubmitsTheClientBodyWithTheMappedModel(t *testing.T) {
	plugin := loadMigaPlugin(t)
	body := migaBody(map[string]any{"model": "magi-2-alias", "seed": 7})
	submit := func(headers map[string]any) map[string]any {
		return callMiga(t, plugin, nil, "buildSubmitRequest", map[string]any{
			"requestBody":    map[string]any{"model": "magi-2-alias", "vendorBody": body},
			"model":          "magi-2-alias",
			"upstreamModel":  "magi-2-dmc",
			"baseUrl":        "https://dmc.example/",
			"apiKey":         "dmc-key",
			"requestHeaders": headers,
		}).(map[string]any)
	}

	request := submit(map[string]any{"Content-Type": "application/json", "Idempotency-Key": "retry-1"})
	assert.Equal(t, "https://dmc.example/miga/v2/generations", request["url"])
	assert.Equal(t, "POST", request["method"])
	assert.Equal(t, "text_to_video", request["action"])
	assert.Equal(t, map[string]any{"Content-Type": "application/json", "Accept": "application/json", "Authorization": "Bearer dmc-key", "Idempotency-Key": "retry-1"}, request["headers"])
	sent := request["body"].(map[string]any)
	assert.Equal(t, "magi-2-dmc", sent["model"], "a channel mapping reaches DMC")
	assert.Equal(t, float64(4), sent["duration"])
	assert.Equal(t, float64(7), sent["seed"])

	assert.NotContains(t, submit(map[string]any{"Content-Type": "application/json"})["headers"], "Idempotency-Key")

	facts := callMiga(t, plugin, nil, "extractUsage", map[string]any{"requestBody": map[string]any{"vendorBody": migaBody(map[string]any{"duration": 10})}, "usagePurpose": "facts"})
	assert.Equal(t, map[string]any{"seconds": float64(10)}, facts)
	assert.Nil(t, callMiga(t, plugin, nil, "extractUsage", map[string]any{"requestBody": map[string]any{"vendorBody": migaBody(nil)}, "usagePurpose": "billing_ratios"}))
}

func TestMigaPollingAndStatusAgree(t *testing.T) {
	plugin := loadMigaPlugin(t)
	const videoURL = "https://hub.dmc.example/video.mp4"
	for _, testCase := range []struct {
		body map[string]any
		want map[string]any
	}{
		{map[string]any{"status": "queued"}, map[string]any{"code": float64(0), "status": "QUEUED", "progress": "20%"}},
		{map[string]any{"status": "processing"}, map[string]any{"code": float64(0), "status": "IN_PROGRESS", "progress": "50%"}},
		{map[string]any{"status": "succeeded", "output": map[string]any{"videoUrl": videoURL}}, map[string]any{"code": float64(0), "status": "SUCCESS", "progress": "100%"}},
		// DMC may report succeeded before it hands out the video.
		{map[string]any{"status": "succeeded", "output": map[string]any{"videoUrl": nil}}, map[string]any{"code": float64(0), "status": "IN_PROGRESS", "progress": "90%"}},
		{map[string]any{"status": "failed", "error": map[string]any{"code": "moderation", "message": "prompt rejected"}}, map[string]any{"code": float64(0), "status": "FAILURE", "progress": "100%", "reason": "prompt rejected"}},
		{map[string]any{"status": "canceled"}, map[string]any{"code": float64(0), "status": "FAILURE", "progress": "100%", "reason": "task canceled"}},
		{map[string]any{"code": "not_found"}, map[string]any{"status": "UNKNOWN", "reason": "unrecognized status: not_found"}},
	} {
		assert.Equal(t, testCase.want, callMiga(t, plugin, nil, "parseTaskResult", map[string]any{}, testCase.body), testCase.body)
	}

	render := func(task map[string]any, artifacts map[string]any) map[string]any {
		return callMiga(t, plugin, []string{"generationStatus"}, map[string]any{"artifacts": artifacts}, task).(map[string]any)
	}
	pending := render(map[string]any{"task_id": "task_pub", "status": "IN_PROGRESS", "data": map[string]any{"status": "succeeded", "output": map[string]any{"videoUrl": nil}}}, map[string]any{})
	assert.Equal(t, "processing", pending["status"], "succeeded is shown only with a video to download")
	assert.Nil(t, pending["output"].(map[string]any)["videoUrl"])

	const signed = "https://gateway.example/v1/tasks/task_pub/artifacts/video/content?access=sig"
	done := render(map[string]any{"task_id": "task_pub", "status": "SUCCESS", "data": map[string]any{"id": "task_pub", "status": "succeeded", "output": map[string]any{"videoUrl": videoURL}, "createdAt": "2026-11-26T20:27:55.000Z"}},
		map[string]any{"video": map[string]any{"key": "video", "type": "video", "url": signed}})
	assert.Equal(t, map[string]any{"id": "task_pub", "status": "succeeded", "output": map[string]any{"videoUrl": signed, "urlExpiresAt": nil}, "error": nil, "createdAt": "2026-11-26T20:27:55.000Z"}, done)

	failed := render(map[string]any{"task_id": "task_pub", "status": "FAILURE", "fail_reason": "poll failures", "data": map[string]any{"status": "processing"}}, map[string]any{})
	assert.Equal(t, "failed", failed["status"])
	assert.Equal(t, map[string]any{"code": "task_failed", "message": "poll failures"}, failed["error"])

	artifacts := callMiga(t, plugin, nil, "listArtifacts", map[string]any{"status": "SUCCESS", "data": map[string]any{"output": map[string]any{"videoUrl": videoURL}}})
	assert.Equal(t, []any{map[string]any{"key": "video", "type": "video", "mimeType": "video/mp4"}}, artifacts)
	content := callMiga(t, plugin, nil, "buildContentRequest", map[string]any{"artifactKey": "video", "baseUrl": "https://dmc.example", "apiKey": "dmc-key", "upstreamTaskId": "task_up", "clientRequest": map[string]any{"method": "HEAD"}})
	assert.Equal(t, map[string]any{"url": "https://dmc.example/v1/tasks/task_up/artifacts/video/content", "method": "HEAD", "headers": map[string]any{"Authorization": "Bearer dmc-key"}}, content)
}
