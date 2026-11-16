package application

import (
	"context"

	"app-deployment-platform/internal/deployment/domain"
)

func (s *Service) deployFullStack(ctx context.Context, job *domain.Job, work, source string) error {
	return s.Miaoda.DeployFullStack(ctx, job, work, source)
}
