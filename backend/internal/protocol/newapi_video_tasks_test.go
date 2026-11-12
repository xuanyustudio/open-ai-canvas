package protocol

import (
	"context"
	"testing"
)

func TestNewAPIVideoTasksCreateAndPollPaths(t *testing.T) {
	request := GenerationRequest{
		Model: "H3-768P", Prompt: "双桥村雨巷", Duration: 10, AspectRatio: "9:16", Resolution: "768P",
		GenerateAudio: true,
		Images: []MediaReference{
			{URL: "https://cdn.example/ref-a.png", Order: 1},
			{URL: "https://cdn.example/ref-b.png", Order: 2},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4", Order: 1}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Order: 1}},
	}

	t.Run("official plugin", func(t *testing.T) {
		adapter := officialPackageAdapter(t, "newapi-video-tasks-v1.yingce-plugin", "newapi-video-tasks")
		create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		if create.Method != "POST" || create.Path != "/v1/video/generations" || create.ContentType != "application/json" {
			t.Fatalf("create = %#v", create)
		}
		body := manifestTestBody(t, create)
		assertNewAPIVideoTasksCreateBody(t, body, request)

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
		legacyCreate, err := channel2.BuildCreate(context.Background(), RequestContext{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		legacyBody, _ := legacyCreate.Body.(map[string]any)
		if _, hasImages := legacyBody["images"]; hasImages {
			t.Fatalf("channel-2 must keep image_urls and must not send images: %#v", legacyBody)
		}
		images, _ := legacyBody["image_urls"].([]string)
		if len(images) != 2 || images[0] != request.Images[0].URL {
			t.Fatalf("channel-2 image_urls = %#v", legacyBody["image_urls"])
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
}

func assertNewAPIVideoTasksCreateBody(t *testing.T, body map[string]any, request GenerationRequest) {
	t.Helper()
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["seconds"] != "10" || body["aspect_ratio"] != "9:16" || body["ratio"] != "9:16" || body["resolution"] != "768P" {
		t.Fatalf("create body = %#v", body)
	}
	if body["generate_audio"] != true {
		t.Fatalf("generate_audio = %#v", body["generate_audio"])
	}
	assertStringList(t, body, "images", request.Images[0].URL, request.Images[1].URL)
	assertStringList(t, body, "image_urls", request.Images[0].URL, request.Images[1].URL)
	if body["image"] != request.Images[0].URL {
		t.Fatalf("image = %#v", body["image"])
	}
	assertStringList(t, body, "videos", request.Videos[0].URL)
	assertStringList(t, body, "audios", request.Audios[0].URL)
}

func assertStringList(t *testing.T, body map[string]any, key string, want ...string) {
	t.Helper()
	got := stringList(body[key])
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %v", key, body[key], want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %#v, want %v", key, body[key], want)
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
