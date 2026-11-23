package protocol

import (
	"context"
	"testing"
)

func TestOfficialOpenAIVideosStillMapsAspectRatioToSize(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-videos.yingce-plugin", "newapi")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "sora-2", Prompt: "a walking cat", Duration: 4, AspectRatio: "16:9", Resolution: "768p",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/v1/videos" || spec.ContentType != "multipart/form-data" {
		t.Fatalf("create = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["size"] != "16:9" || body["resolution_name"] != "768p" || body["seconds"] != "4" {
		t.Fatalf("official openai-videos body changed: %#v", body)
	}
	if _, ok := body["metadata"]; ok {
		t.Fatalf("official openai-videos should not emit metadata: %#v", body)
	}
}

func TestOhMyBBOpenAIVideosCompatOmitsSizeAndUsesMetadata(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-videos-ohmybb-compat.yingce-plugin", "openai-videos-ohmybb")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "mb-minimax-h3-f-768p", Prompt: "洗头发的女子", Duration: 4, AspectRatio: "16:9", Resolution: "768p",
		GenerateAudio: true, Watermark: false,
		Images: []MediaReference{{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1}, {URL: "https://cdn.example/ref.png", Role: "reference_image", Order: 2}},
		Videos: []MediaReference{{URL: "https://cdn.example/ref.mp4", Role: "reference_video", Order: 1}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Role: "reference_voice", Order: 1}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/v1/videos" || spec.ContentType != "application/json" || spec.Auth.Type != "bearer" {
		t.Fatalf("create = %#v", spec)
	}
	if len(spec.Files) != 0 {
		t.Fatalf("compat plugin should not use multipart references: %#v", spec.Files)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "mb-minimax-h3-f-768p" || body["prompt"] != "洗头发的女子" || body["seconds"] != "4" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["size"]; ok {
		t.Fatalf("size must not be sent (16:9 is not a WxH enum): %#v", body)
	}
	if _, ok := body["resolution_name"]; ok {
		t.Fatalf("resolution_name must not be sent: %#v", body)
	}
	metadata, _ := body["metadata"].(map[string]any)
	if metadata["ratio"] != "16:9" || metadata["resolution"] != "768p" || metadata["duration"] != float64(4) || metadata["generate_audio"] != true || metadata["watermark"] != false {
		t.Fatalf("metadata = %#v", metadata)
	}
	content, _ := metadata["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("content = %#v", content)
	}
	first, _ := content[0].(map[string]any)
	image, _ := first["image_url"].(map[string]any)
	if first["type"] != "image_url" || first["role"] != "first_frame" || image["url"] != "https://cdn.example/first.png" {
		t.Fatalf("first image = %#v", first)
	}
	second, _ := content[1].(map[string]any)
	if second["role"] != "reference_image" {
		t.Fatalf("second image = %#v", second)
	}
	video, _ := content[2].(map[string]any)
	videoURL, _ := video["video_url"].(map[string]any)
	if video["type"] != "video_url" || videoURL["url"] != "https://cdn.example/ref.mp4" {
		t.Fatalf("video = %#v", video)
	}
	audio, _ := content[3].(map[string]any)
	if audio["type"] != "audio_url" {
		t.Fatalf("audio = %#v", audio)
	}

	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"video_abc","status":"processing"}`))
	if err != nil || created.TaskID != "video_abc" {
		t.Fatalf("created = %#v err=%v", created, err)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: created.TaskID})
	if err != nil || poll.Method != "GET" || poll.Path != "/v1/videos/video_abc" {
		t.Fatalf("poll = %#v err=%v", poll, err)
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("missing ResultAdapter")
	}
	result, err := resultAdapter.BuildResult(context.Background(), PollContext{TaskID: created.TaskID})
	if err != nil || result.Method != "GET" || result.Path != "/v1/videos/video_abc/content" || result.Auth.Type != "bearer" {
		t.Fatalf("result = %#v err=%v", result, err)
	}

	numberRequest := GenerationRequest{
		Model: "mb-minimax-h3-f-768p", Prompt: "参考图1起身", Duration: 4,
		Images: []MediaReference{{ID: "actor", URL: "https://cdn.example/ref.png", Role: "reference_image", Order: 1}},
	}
	numberSpec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: numberRequest})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := adapter.Metadata().VideoReferences.Bind(numberRequest, &numberSpec)
	if err != nil || bound.Tokens["actor"] == "" {
		t.Fatalf("metadata-content numbering failed: tokens=%v err=%v", bound.Tokens, err)
	}
}

func TestOhMyBBOpenAIVideosCompatOmitsEmptyMetadata(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-videos-ohmybb-compat.yingce-plugin", "openai-videos-ohmybb")
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "wb-seedance-2-fast", Prompt: "a landscape", Duration: 5,
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if body["seconds"] != "5" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["size"]; ok {
		t.Fatalf("unexpected size: %#v", body)
	}
	metadata, _ := body["metadata"].(map[string]any)
	if metadata["duration"] != float64(5) {
		t.Fatalf("duration should still be in metadata: %#v", metadata)
	}
	if _, ok := metadata["ratio"]; ok {
		t.Fatalf("empty ratio should be omitted: %#v", metadata)
	}
	if _, ok := metadata["content"]; ok {
		t.Fatalf("empty content should be omitted: %#v", metadata)
	}
}
