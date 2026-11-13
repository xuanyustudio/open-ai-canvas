package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testVideoTasksImageA = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	testVideoTasksImageB = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	testVideoTasksVideo  = "data:video/mp4;base64,AAAA"
	testVideoTasksAudio  = "data:audio/mpeg;base64,AAAA"
)

func TestNewAPIVideoTasksCreateAndPollPaths(t *testing.T) {
	request := GenerationRequest{
		Model: "H3-768P", Prompt: "双桥村雨巷", Duration: 10, AspectRatio: "9:16",
		Images: []MediaReference{
			{DataURL: testVideoTasksImageA, Order: 1},
			{DataURL: testVideoTasksImageB, Order: 2},
		},
		Videos: []MediaReference{{DataURL: testVideoTasksVideo, Order: 1}},
		Audios: []MediaReference{{DataURL: testVideoTasksAudio, Order: 1}},
	}

	t.Run("official plugin", func(t *testing.T) {
		adapter := officialPackageAdapter(t, "newapi-video-tasks-v1.yingce-plugin", "newapi-video-tasks")
		if adapter.Metadata().RequiresPublicMediaURLs {
			t.Fatal("newapi-video-tasks must embed media and must not require public URLs")
		}
		prepared := request
		if err := PrepareNewAPIVideoTasksMedia(context.Background(), &prepared); err != nil {
			t.Fatal(err)
		}
		create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: prepared})
		if err != nil {
			t.Fatal(err)
		}
		if create.Method != "POST" || create.Path != "/v1/video/generations" || create.ContentType != "application/json" {
			t.Fatalf("create = %#v", create)
		}
		body := manifestTestBody(t, create)
		assertNewAPIVideoTasksCreateBody(t, body, prepared)

		created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"0d781e26038ab144","status":"queued"}`))
		if err != nil || created.TaskID != "0d781e26038ab144" || created.Status != StatusPending {
			t.Fatalf("created = %#v err=%v", created, err)
		}
		poll, err := adapter.BuildPoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID})
		if err != nil || poll.Method != "GET" || poll.Path != "/v1/video/tasks/0d781e26038ab144" {
			t.Fatalf("poll = %#v err=%v", poll, err)
		}
	})

	t.Run("builtin host adapter", func(t *testing.T) {
		adapter, ok := Builtins().Get("newapi-video-tasks")
		if !ok {
			t.Fatal("builtin newapi-video-tasks missing")
		}
		if adapter.Metadata().RequiresPublicMediaURLs {
			t.Fatal("builtin newapi-video-tasks must not require public URLs")
		}
		create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		if create.Method != "POST" || create.Path != "/v1/video/generations" {
			t.Fatalf("builtin create = %#v", create)
		}
		body, ok := create.Body.(map[string]any)
		if !ok {
			t.Fatalf("builtin body type = %T", create.Body)
		}
		assertNewAPIVideoTasksCreateBody(t, body, request)
		poll, err := adapter.BuildPoll(context.Background(), PollContext{Model: request.Model, TaskID: "0d781e26038ab144"})
		if err != nil || poll.Method != "GET" || poll.Path != "/v1/video/tasks/0d781e26038ab144" {
			t.Fatalf("builtin poll = %#v err=%v", poll, err)
		}
		channel2, ok := Builtins().Get("newapi-channel-2")
		if !ok {
			t.Fatal("builtin newapi-channel-2 missing")
		}
		legacyRequest := GenerationRequest{
			Model: "H3-768P", Prompt: "双桥村雨巷", Duration: 10, AspectRatio: "9:16",
			Images: []MediaReference{
				{URL: "https://cdn.example/ref-a.png", Order: 1},
				{URL: "https://cdn.example/ref-b.png", Order: 2},
			},
			Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4", Order: 1}},
			Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Order: 1}},
		}
		legacyCreate, err := channel2.BuildCreate(context.Background(), RequestContext{Request: legacyRequest})
		if err != nil {
			t.Fatal(err)
		}
		legacyBody, _ := legacyCreate.Body.(map[string]any)
		if _, hasImages := legacyBody["images"]; hasImages {
			t.Fatalf("channel-2 must keep image_urls and must not send images: %#v", legacyBody)
		}
		images, _ := legacyBody["image_urls"].([]string)
		if len(images) != 2 || images[0] != legacyRequest.Images[0].URL {
			t.Fatalf("channel-2 image_urls = %#v", legacyBody["image_urls"])
		}
		if strings.HasPrefix(images[0], "data:") {
			t.Fatalf("channel-2 must stay URL-based: %#v", legacyBody["image_urls"])
		}
		legacyPoll, err := channel2.BuildPoll(context.Background(), PollContext{TaskID: "0d781e26038ab144"})
		if err != nil || legacyPoll.Path != "/v1/video/generations/0d781e26038ab144" {
			t.Fatalf("channel-2 poll changed = %#v err=%v", legacyPoll, err)
		}
	})
}

func TestNewAPIVideoTasksParsesGatewayPollPayloads(t *testing.T) {
	adapter := officialPackageAdapter(t, "newapi-video-tasks-v1.yingce-plugin", "newapi-video-tasks")
	builtin, ok := Builtins().Get("newapi-video-tasks")
	if !ok {
		t.Fatal("builtin newapi-video-tasks missing")
	}
	tests := []struct {
		name    string
		payload string
		status  Status
		wantURL string
		wantMsg string
	}{
		{name: "succeeded data.0.url", payload: `{"id":"0d781e26038ab144","status":"succeeded","data":[{"url":"https://cdn.example/succeeded.mp4"}]}`, status: StatusSucceeded, wantURL: "https://cdn.example/succeeded.mp4"},
		{name: "completed data.0.url", payload: `{"id":"0d781e26038ab144","status":"completed","data":[{"url":"https://cdn.example/completed.mp4"}]}`, status: StatusSucceeded, wantURL: "https://cdn.example/completed.mp4"},
		{name: "SUCCESS data.0.url", payload: `{"id":"0d781e26038ab144","status":"SUCCESS","data":[{"url":"https://cdn.example/success.mp4"}]}`, status: StatusSucceeded, wantURL: "https://cdn.example/success.mp4"},
		{name: "failed error string", payload: `{"id":"0d781e26038ab144","status":"failed","error":"upstream rejected"}`, status: StatusFailed, wantMsg: "upstream rejected"},
		{name: "cancelled", payload: `{"id":"0d781e26038ab144","status":"cancelled"}`, status: StatusCancelled},
		{name: "running in_progress", payload: `{"id":"0d781e26038ab144","status":"in_progress"}`, status: StatusProcessing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, item := range []struct {
				name    string
				adapter Adapter
			}{{"official", adapter}, {"builtin", builtin}} {
				t.Run(item.name, func(t *testing.T) {
					state, err := item.adapter.ParsePoll(context.Background(), PollContext{TaskID: "0d781e26038ab144"}, []byte(test.payload))
					if err != nil {
						t.Fatal(err)
					}
					if state.Status != test.status {
						t.Fatalf("status = %q, want %q (%#v)", state.Status, test.status, state)
					}
					if test.wantURL != "" && (state.Result == nil || len(state.Result.Videos) != 1 || state.Result.Videos[0].URL != test.wantURL) {
						t.Fatalf("state = %#v, want succeeded %s", state, test.wantURL)
					}
					if test.wantMsg != "" && state.Message != test.wantMsg {
						t.Fatalf("message = %q, want %q (%#v)", state.Message, test.wantMsg, state)
					}
				})
			}
		})
	}
}

func TestNewAPIVideoTasksOmitsEmptyMedia(t *testing.T) {
	request := GenerationRequest{Model: "H3-768P", Prompt: "空镜", Duration: 8, AspectRatio: "16:9"}
	adapter := officialPackageAdapter(t, "newapi-video-tasks-v1.yingce-plugin", "newapi-video-tasks")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, create)
	for _, key := range []string{"images", "image", "image_urls", "videos", "audios", "video_urls", "audio_urls"} {
		if _, ok := body[key]; ok {
			t.Fatalf("empty media field %s should be omitted: %#v", key, body)
		}
	}
	if body["seconds"] != "8" || body["aspect_ratio"] != "16:9" || body["ratio"] != "16:9" {
		t.Fatalf("body = %#v", body)
	}
	builtin, ok := Builtins().Get("newapi-video-tasks")
	if !ok {
		t.Fatal("builtin newapi-video-tasks missing")
	}
	builtinCreate, err := builtin.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	builtinBody, _ := builtinCreate.Body.(map[string]any)
	for _, key := range []string{"images", "videos", "audios"} {
		if _, ok := builtinBody[key]; ok {
			t.Fatalf("builtin empty media field %s should be omitted: %#v", key, builtinBody)
		}
	}
}

func TestNewAPIVideoTasksDefaultRatioIs9x16(t *testing.T) {
	request := GenerationRequest{Model: "minimax-h3-2K", Prompt: "空镜", Duration: 6}
	adapter, ok := Builtins().Get("newapi-video-tasks")
	if !ok {
		t.Fatal("builtin newapi-video-tasks missing")
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := create.Body.(map[string]any)
	if body["aspect_ratio"] != "9:16" || body["ratio"] != "9:16" || body["seconds"] != "6" {
		t.Fatalf("default ratio body = %#v", body)
	}
	plugin := officialPackageAdapter(t, "newapi-video-tasks-v1.yingce-plugin", "newapi-video-tasks")
	pluginCreate, err := plugin.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	pluginBody := manifestTestBody(t, pluginCreate)
	if pluginBody["aspect_ratio"] != "9:16" || pluginBody["ratio"] != "9:16" {
		t.Fatalf("plugin default ratio body = %#v", pluginBody)
	}
}

func TestNewAPIVideoTasksEmbedsHTTPImagesAsDataURI(t *testing.T) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, noisyTestImage(64, 48), &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	payload := buffer.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	signed := server.URL + "/api/public/resources/demo/file?expires=1&signature=secret-token"
	request := GenerationRequest{
		Model: "H3-768P", Prompt: "参考图人物", Duration: 6, AspectRatio: "9:16",
		Images: []MediaReference{{URL: signed, MIMEType: "image/jpeg", Order: 1}},
	}
	adapter, ok := Builtins().Get("newapi-video-tasks")
	if !ok {
		t.Fatal("builtin newapi-video-tasks missing")
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := create.Body.(map[string]any)
	images := stringList(body["images"])
	if len(images) != 1 || !strings.HasPrefix(images[0], "data:image/jpeg;base64,") {
		t.Fatalf("images = %#v", body["images"])
	}
	if strings.Contains(images[0], server.URL) || strings.Contains(images[0], "signature=") {
		t.Fatalf("public URL leaked into images: %s", images[0][:min(80, len(images[0]))])
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(images[0], "data:image/jpeg;base64,"))
	if err != nil || len(got) == 0 {
		t.Fatalf("invalid data URI: %v", err)
	}
}

func TestNewAPIVideoTasksPrefersLocalLoaderOverHTTP(t *testing.T) {
	local := []byte("local-bytes")
	NewAPIVideoTasksLocalLoader = func(ctx context.Context, media MediaReference) ([]byte, string, error) {
		if media.Metadata["storageKey"] != "resource:local-1" {
			t.Fatalf("storageKey = %#v", media.Metadata)
		}
		return local, "image/png", nil
	}
	t.Cleanup(func() { NewAPIVideoTasksLocalLoader = nil })

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(server.Close)

	request := GenerationRequest{
		Model: "H3-768P", Prompt: "本地素材", Duration: 6,
		Images: []MediaReference{{
			URL:      server.URL + "/api/public/resources/local-1/file?signature=secret",
			Metadata: map[string]any{"storageKey": "resource:local-1"},
		}},
	}
	adapter, _ := Builtins().Get("newapi-video-tasks")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("HTTP GET should not run when local loader returns bytes")
	}
	body, _ := create.Body.(map[string]any)
	images := stringList(body["images"])
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(local)
	if len(images) != 1 || images[0] != want {
		t.Fatalf("images = %#v, want %s", images, want)
	}
}

func TestNewAPIVideoTasksEnforcesMediaCaps(t *testing.T) {
	images := make([]MediaReference, 0, 12)
	for i := 0; i < 12; i++ {
		images = append(images, MediaReference{DataURL: testVideoTasksImageA, Order: i + 1})
	}
	videos := []MediaReference{
		{DataURL: testVideoTasksVideo, Order: 1},
		{DataURL: testVideoTasksVideo, Order: 2},
		{DataURL: testVideoTasksVideo, Order: 3},
		{DataURL: testVideoTasksVideo, Order: 4},
	}
	audios := []MediaReference{
		{DataURL: testVideoTasksAudio, Order: 1},
		{DataURL: testVideoTasksAudio, Order: 2},
		{DataURL: testVideoTasksAudio, Order: 3},
		{DataURL: testVideoTasksAudio, Order: 4},
	}
	request := GenerationRequest{Model: "H3-768P", Prompt: "多图", Duration: 6, Images: images, Videos: videos, Audios: audios}
	adapter, _ := Builtins().Get("newapi-video-tasks")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := create.Body.(map[string]any)
	if got := stringList(body["images"]); len(got) != 9 {
		t.Fatalf("images cap = %d, want 9", len(got))
	}
	if got := stringList(body["videos"]); len(got) != 3 {
		t.Fatalf("videos cap = %d, want 3", len(got))
	}
	if got := stringList(body["audios"]); len(got) != 3 {
		t.Fatalf("audios cap = %d, want 3", len(got))
	}
}

func TestNewAPIVideoTasksCompressesWhenOverBudget(t *testing.T) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, noisyTestImage(1600, 1200), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	original := buffer.Bytes()
	if len(original) < 20_000 {
		t.Fatalf("fixture too small to test compression: %d", len(original))
	}
	oldOrig, oldJSON := newAPIVideoTasksOriginalBudget, newAPIVideoTasksJSONBudget
	newAPIVideoTasksOriginalBudget = int64(len(original) / 3)
	newAPIVideoTasksJSONBudget = int64(newAPIVideoTasksMaxJSONBytes)
	t.Cleanup(func() {
		newAPIVideoTasksOriginalBudget, newAPIVideoTasksJSONBudget = oldOrig, oldJSON
	})

	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(original)
	request := GenerationRequest{
		Model: "H3-768P", Prompt: "压缩测试", Duration: 6,
		Images: []MediaReference{{DataURL: dataURL, MIMEType: "image/jpeg"}},
	}
	adapter, _ := Builtins().Get("newapi-video-tasks")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := create.Body.(map[string]any)
	images := stringList(body["images"])
	if len(images) != 1 || !strings.HasPrefix(images[0], "data:image/jpeg;base64,") {
		t.Fatalf("images = %#v", body["images"])
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(images[0], "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) > newAPIVideoTasksOriginalBudget {
		t.Fatalf("compressed %d still over budget %d (original %d)", len(got), newAPIVideoTasksOriginalBudget, len(original))
	}
	if len(got) >= len(original) {
		t.Fatalf("expected compression, original=%d compressed=%d", len(original), len(got))
	}
}

func TestNewAPIVideoTasksRedactsSignedQueryInErrors(t *testing.T) {
	if _, _, err := fetchNewAPIVideoTasksURL(context.Background(), "http://127.0.0.1:1/api/public/resources/x/file?signature=super-secret"); err == nil {
		t.Fatal("expected fetch error")
	} else if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "signature=") {
		t.Fatalf("signed query leaked in error: %v", err)
	}
}

func assertNewAPIVideoTasksCreateBody(t *testing.T, body map[string]any, request GenerationRequest) {
	t.Helper()
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["seconds"] != "10" || body["aspect_ratio"] != "9:16" || body["ratio"] != "9:16" {
		t.Fatalf("create body = %#v", body)
	}
	for _, key := range []string{"image", "image_urls", "video_urls", "audio_urls", "resolution", "generate_audio"} {
		if _, ok := body[key]; ok {
			t.Fatalf("official adapter must not send extra field %s: %#v", key, body)
		}
	}
	if len(body) != 8 {
		t.Fatalf("create body keys = %#v, want official field set only", body)
	}
	assertDataURIList(t, body, "images", 2, "data:image/")
	assertDataURIList(t, body, "videos", 1, "data:video/")
	assertDataURIList(t, body, "audios", 1, "data:audio/")
}

func assertDataURIList(t *testing.T, body map[string]any, key string, want int, prefix string) {
	t.Helper()
	got := stringList(body[key])
	if len(got) != want {
		t.Fatalf("%s = %#v, want %d data URIs", key, body[key], want)
	}
	for _, item := range got {
		if !strings.HasPrefix(item, prefix) || !strings.Contains(item, ";base64,") {
			t.Fatalf("%s item = %q, want %s data URI", key, item, prefix)
		}
	}
}

func stringList(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			text, _ := item.(string)
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func noisyTestImage(width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 13 * y),
				G: uint8(y * 29 * x),
				B: uint8((x + y) * 47),
				A: 255,
			})
		}
	}
	return img
}
