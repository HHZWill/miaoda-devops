package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"app-deployment-platform/internal/deployment/domain"
)

func TestRunCapturesFailureDetails(t *testing.T) {
	script := "printf '%s\\n' '==> [执行 DDL] 开始' '  -> FAIL(rc=1)：SQLSTATE 42703' '!! [执行 DDL] 失败，DDL 执行失败'; exit 1"
	err := NewRunner().Run(context.Background(), t.TempDir(), os.Environ(), func(string) {}, "sh", "-c", script)

	var failure *ScriptFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected ScriptFailure, got %T", err)
	}
	if failure.Step != "执行 DDL" || failure.Reason != "DDL 执行失败" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if !strings.Contains(strings.Join(failure.Details, "\n"), "SQLSTATE 42703") {
		t.Fatalf("missing technical details: %#v", failure.Details)
	}

	job := domain.Job{
		Status: domain.JobRunning,
		Steps:  []domain.Step{{Key: "执行 DDL", Label: "执行 DDL", Status: domain.StepRunning}},
	}
	job.Finish(fmt.Errorf("部署中止：%w", err))
	if job.Failure == nil || job.Failure.Step != "执行 DDL" || len(job.Failure.Details) == 0 {
		t.Fatalf("structured failure was not saved: %#v", job.Failure)
	}
}
