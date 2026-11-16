package persistence

import (
	"os"
	"testing"
	"time"

	"app-deployment-platform/internal/deployment/domain"
)

func TestTimingSurvivesPersistence(t *testing.T) {
	repository, err := NewFileJobRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	job := domain.Job{
		ID: "0123456789abcdef0123456789abcdef", Profile: "test", Status: domain.JobQueued,
		QueuedAt: &base, Steps: domain.NewSteps("部署"),
	}
	if err := os.Mkdir(repository.JobDir(job.ID), 0700); err != nil {
		t.Fatal(err)
	}
	job.Start(base.Add(time.Second))
	job.Steps[0].SetStatus(domain.StepRunning, "", base.Add(time.Second))
	if err := repository.Save(&job); err != nil {
		t.Fatal(err)
	}
	reloaded, err := repository.Get(job.ID, job.Profile)
	if err != nil {
		t.Fatal(err)
	}
	// JSON 重载后的执行段仍能正确结算。
	reloaded.FinishAt(nil, base.Add(3*time.Second))
	if err := repository.Save(&reloaded); err != nil {
		t.Fatal(err)
	}
	items, err := repository.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].DurationMs != 2000 || items[0].QueueDurationMs != 1000 ||
		items[0].Steps[0].DurationMs != 2000 || items[0].FinishedAt == nil {
		t.Fatalf("persisted timing: %#v", items)
	}
}
