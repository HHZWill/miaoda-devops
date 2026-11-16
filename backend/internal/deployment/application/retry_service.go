package application

import (
	"context"
	"errors"
	"log"
	"path/filepath"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/persistence"
)

func (s *Service) RetryDeploy(ctx context.Context, id, profile string) (domain.Job, int, error) {
	if _, err := s.Lark.SelectProfile(ctx, profile, true); err != nil {
		return domain.Job{}, 401, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.Jobs.Get(id, profile)
	if err != nil || job.Kind != "deploy" || job.Status != domain.JobFailed {
		return domain.Job{}, 409, errors.New("当前任务无法重新部署")
	}
	s.readDeployState(&job)
	if job.Overwrite != nil {
		items, err := s.Jobs.List()
		if err != nil {
			return domain.Job{}, 500, errors.New("无法读取目标应用的部署记录")
		}
		for _, item := range items {
			if item.ID != job.ID && item.Kind == "deploy" && item.TargetID == job.TargetID &&
				(item.Status == domain.JobQueued || item.Status == domain.JobRunning || item.CreatedAt.After(job.CreatedAt)) {
				return domain.Job{}, 409, errors.New("目标应用已有其他或更新的部署任务，请使用最新的任务记录")
			}
		}
	}
	if err := job.PrepareRetry(); err != nil {
		return domain.Job{}, 409, err
	}
	if err := persistence.WriteJSON(filepath.Join(s.Jobs.JobDir(job.ID), "job.json"), &job); err != nil {
		return domain.Job{}, 500, errors.New("无法保存本地任务")
	}
	log.Printf("[job] retry_queued job_id=%s profile=%q resume_from=%q target_id=%s",
		job.ID, job.Profile, job.ResumeFrom, job.TargetID)
	go s.runJob(job)
	return job, 202, nil
}
