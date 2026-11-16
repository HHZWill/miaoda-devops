package application

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/persistence"
)

func TestInterruptedTimingUsesLastSnapshot(t *testing.T) {
	repository, err := persistence.NewFileJobRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	job := domain.Job{
		ID: "0123456789abcdef0123456789abcdef", Kind: "package", Profile: "test",
		Status: domain.JobQueued, QueuedAt: &base, UpdatedAt: base.Add(3 * time.Second),
		Steps: domain.NewSteps("打包"),
	}
	job.Start(base.Add(time.Second))
	job.Steps[0].SetStatus(domain.StepRunning, "", base.Add(2*time.Second))
	if err := os.Mkdir(repository.JobDir(job.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := persistence.WriteJSON(filepath.Join(repository.JobDir(job.ID), "job.json"), &job); err != nil {
		t.Fatal(err)
	}
	service := Service{Jobs: repository}
	if err := service.interruptJobs(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := repository.Get(job.ID, job.Profile)
	if err != nil {
		t.Fatal(err)
	}
	// 恢复时不得把快照之后的停机时间算入。
	if reloaded.DurationMs != 2000 || reloaded.Steps[0].DurationMs != 1000 ||
		reloaded.Status != domain.JobFailed || !reloaded.TimingEstimated || !reloaded.Steps[0].TimingEstimated {
		t.Fatalf("interrupted timing: %#v", reloaded)
	}
}
