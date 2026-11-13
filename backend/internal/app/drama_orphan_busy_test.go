package app

import (
	"encoding/json"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestFailJobClearsSegmentVideoBusyAfterRevisionBump(t *testing.T) {
	svc, fake := newDramaTestService(t)
	summary := createTestDrama(t, svc, DramaReviewAuto)
	project := summary.Project.ID
	episode := model.DramaEpisode{ID: "ep-orphan", ProjectID: project, Number: 5, Stage: model.DramaEpisodeVideo, Revision: 1}
	if err := fake.db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	segment := model.DramaSegment{
		ID: "seg-orphan", ProjectID: project, EpisodeID: episode.ID, SegmentIndex: 0, Revision: 2,
		PromptStatus: model.DramaMediaReady, VideoStatus: model.DramaMediaRunning, VideoPrompt: "prompt",
	}
	if err := fake.db.Create(&segment).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(dramaRevisionPayload{Revision: 1, Seconds: 5})
	job := model.DramaJob{
		ID: "job-orphan", ProjectID: project, EpisodeID: episode.ID, ShotID: segment.ID,
		Kind: model.DramaJobSegmentVideo, Status: model.DramaJobRunning, PayloadJSON: string(payload),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := fake.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	run := &dramaRun{s: svc, production: &model.DramaProduction{ProjectID: project}, project: &model.Project{ID: project, UserID: "usr-drama"}}
	if err := run.failJob(&job, "上游拒绝参考图"); err != nil {
		t.Fatalf("failJob: %v", err)
	}
	saved, err := svc.repo.DramaSegment(project, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.VideoStatus != model.DramaMediaFailed {
		t.Fatalf("video status = %q, want failed after revision mismatch", saved.VideoStatus)
	}
	if saved.VideoError == "" {
		t.Fatal("expected video error to be preserved")
	}
}

func TestReconcileOrphanBusySegmentWithoutRunningJob(t *testing.T) {
	svc, fake := newDramaTestService(t)
	summary := createTestDrama(t, svc, DramaReviewAuto)
	project := summary.Project.ID
	if err := svc.repo.UpdateDramaProduction(project, map[string]any{"status": model.DramaStatusPaused, "pause_reason": "手动暂停"}); err != nil {
		t.Fatal(err)
	}
	episode := model.DramaEpisode{ID: "ep-stuck", ProjectID: project, Number: 5, Stage: model.DramaEpisodeVideo, Revision: 1}
	if err := fake.db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	segment := model.DramaSegment{
		ID: "seg-stuck", ProjectID: project, EpisodeID: episode.ID, SegmentIndex: 0, Revision: 3,
		PromptStatus: model.DramaMediaReady, VideoStatus: model.DramaMediaRunning, VideoPrompt: "prompt",
	}
	if err := fake.db.Create(&segment).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(dramaRevisionPayload{Revision: 1, Seconds: 5})
	job := model.DramaJob{
		ID: "job-done", ProjectID: project, EpisodeID: episode.ID, ShotID: segment.ID,
		Kind: model.DramaJobSegmentVideo, Status: model.DramaJobFailed, Error: "上游 502",
		PayloadJSON: string(payload), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := fake.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	productions, err := svc.repo.RunningDramaProductions(20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range productions {
		if row.ProjectID == project {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("paused project with orphan busy segment must stay in the scheduler")
	}

	if err := svc.tickDrama(&model.DramaProduction{ProjectID: project, UserID: "usr-drama", Status: model.DramaStatusPaused}); err != nil {
		t.Fatalf("tickDrama: %v", err)
	}
	saved, err := svc.repo.DramaSegment(project, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.VideoStatus != model.DramaMediaFailed {
		t.Fatalf("video status = %q, want failed after reconcile", saved.VideoStatus)
	}

	result, err := svc.RetryFailedDrama("usr-drama", project)
	if err != nil {
		t.Fatalf("RetryFailedDrama: %v", err)
	}
	if n, _ := result["segments"].(int); n < 1 {
		t.Fatalf("RetryFailedDrama segments = %v, want at least 1", result["segments"])
	}
	retried, err := svc.repo.DramaSegment(project, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.VideoStatus != model.DramaMediaQueued {
		t.Fatalf("after retry video status = %q, want queued", retried.VideoStatus)
	}
}

func TestGenerateDramaSegmentsUnlocksOrphanBusy(t *testing.T) {
	svc, fake := newDramaTestService(t)
	summary := createTestDrama(t, svc, DramaReviewAuto)
	project := summary.Project.ID
	episode := model.DramaEpisode{ID: "ep-gen", ProjectID: project, Number: 5, Stage: model.DramaEpisodeVideo, Revision: 1, MediaReleased: true}
	if err := fake.db.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	if err := fake.db.Create(&model.DramaShot{ID: "shot-gen", ProjectID: project, EpisodeID: episode.ID, Position: 0, SegmentIndex: 0, DurationMs: 5000}).Error; err != nil {
		t.Fatal(err)
	}
	segment := model.DramaSegment{
		ID: "seg-gen", ProjectID: project, EpisodeID: episode.ID, SegmentIndex: 0, Revision: 2,
		PromptStatus: model.DramaMediaReady, VideoStatus: model.DramaMediaRunning, VideoPrompt: "ready prompt long enough",
	}
	if err := fake.db.Create(&segment).Error; err != nil {
		t.Fatal(err)
	}
	result, err := svc.GenerateDramaSegments("usr-drama", project, episode.ID, GenerateDramaSegmentsRequest{Segments: []int{0}})
	if err != nil {
		t.Fatalf("GenerateDramaSegments: %v", err)
	}
	if n, _ := result["queued"].(int); n != 1 {
		t.Fatalf("queued = %v, want 1", result["queued"])
	}
	saved, err := svc.repo.DramaSegment(project, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.VideoStatus != model.DramaMediaQueued {
		t.Fatalf("video status = %q, want queued", saved.VideoStatus)
	}
}
