package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPrepareRetryPreservesCompletedSteps(t *testing.T) {
	job := Job{
		Status: JobFailed,
		Steps: []Step{
			{Key: "创建应用", Status: StepSuccess},
			{Key: "执行 DDL", Status: StepFailed, Detail: "数据库错误"},
			{Key: "发布上线", Status: StepBlocked, Detail: "前序失败"},
		},
	}

	if err := job.PrepareRetry(); err != nil {
		t.Fatal(err)
	}
	if job.ResumeFrom != "执行 DDL" || job.Steps[0].Status != StepSuccess {
		t.Fatalf("completed step was reset: %#v", job)
	}
	if job.Steps[1].Status != StepPending || job.Steps[2].Status != StepPending {
		t.Fatalf("resume steps were not reset: %#v", job.Steps)
	}
}

func TestTimingAcrossFailureAndRetry(t *testing.T) {
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	job := Job{Status: JobQueued, QueuedAt: &base, Steps: NewSteps("准备", "部署", "发布")}
	job.Start(base.Add(2 * time.Second))
	job.Start(base.Add(2500 * time.Millisecond))
	job.Steps[0].SetStatus(StepRunning, "", base.Add(2*time.Second))
	job.Steps[0].SetStatus(StepRunning, "进度更新", base.Add(2500*time.Millisecond))
	job.Steps[0].SetStatus(StepSuccess, "", base.Add(3*time.Second))
	job.Steps[0].SetStatus(StepSuccess, "", base.Add(3500*time.Millisecond))
	job.Steps[1].SetStatus(StepRunning, "", base.Add(3*time.Second))
	job.FinishAt(errors.New("发布失败"), base.Add(6*time.Second))
	job.FinishAt(errors.New("重复结束"), base.Add(60*time.Second))
	// 失败任务结束后不得继续累计。
	if job.DurationMs != 4000 || job.QueueDurationMs != 2000 ||
		job.Steps[0].DurationMs != 1000 || job.Steps[1].DurationMs != 3000 {
		t.Fatalf("first attempt timing: %#v", job)
	}
	if job.Steps[1].Status != StepFailed || job.Steps[2].Status != StepBlocked {
		t.Fatalf("failed steps: %#v", job.Steps)
	}
	// 重试前的人工等待不计入执行或排队。
	if err := job.PrepareRetryAt(base.Add(120 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if job.StartedAt != nil || job.FinishedAt != nil || job.Steps[1].StartedAt != nil {
		t.Fatal("retry retained previous attempt timestamps")
	}
	if job.QueueDuration(base.Add(123*time.Second)) != 5000 {
		t.Fatal("retry queue duration missing")
	}
	job.Start(base.Add(123 * time.Second))
	job.Steps[1].SetStatus(StepRunning, "", base.Add(123*time.Second))
	// 运行中的耗时包含前轮累计与本轮执行。
	if job.ExecutionDuration(base.Add(124*time.Second)) != 5000 ||
		job.Steps[1].ExecutionDuration(base.Add(124*time.Second)) != 4000 {
		t.Fatal("live cumulative duration incorrect")
	}
	job.Steps[1].SetStatus(StepSuccess, "", base.Add(125*time.Second))
	job.Steps[2].SetStatus(StepRunning, "", base.Add(125*time.Second))
	job.FinishAt(nil, base.Add(126*time.Second))
	if job.DurationMs != 7000 || job.QueueDurationMs != 5000 ||
		job.Steps[1].DurationMs != 5000 || job.Steps[2].DurationMs != 1000 ||
		job.Steps[2].Status != StepSuccess || job.TimingIncomplete {
		t.Fatalf("retry timing: %#v", job)
	}
	if job.ExecutionDuration(base.Add(time.Hour)) != 7000 {
		t.Fatal("completed job timer kept running")
	}
}

func TestStepTimingTerminalStatuses(t *testing.T) {
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	// 成功、失败、警告和跳过都应结算执行时间。
	for _, status := range []string{StepSuccess, StepFailed, StepWarning, StepSkipped} {
		t.Run(status, func(t *testing.T) {
			step := Step{Status: StepPending}
			step.SetStatus(StepRunning, "", base)
			step.SetStatus(status, "", base.Add(1500*time.Millisecond))
			step.SetStatus(status, "更新文案", base.Add(10*time.Second))
			if step.DurationMs != 1500 || step.FinishedAt == nil ||
				step.ExecutionDuration(base.Add(time.Hour)) != 1500 {
				t.Fatalf("terminal timing: %#v", step)
			}
		})
	}
	step := Step{Status: StepPending}
	step.SetStatus(StepSkipped, "", base)
	if step.StartedAt != nil || step.FinishedAt == nil || step.DurationMs != 0 {
		t.Fatal("skipped step should record zero duration")
	}
}

func TestLegacyTimingAndClockRollback(t *testing.T) {
	var legacy Job
	if err := json.Unmarshal([]byte(`{"status":"failed","steps":[{"key":"部署","status":"failed"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	if err := legacy.PrepareRetryAt(base); err != nil {
		t.Fatal(err)
	}
	if !legacy.TimingIncomplete || !legacy.Steps[0].TimingIncomplete {
		t.Fatal("legacy retry must mark partial timing")
	}
	legacy.Start(base.Add(-time.Second))
	legacy.Steps[0].SetStatus(StepRunning, "", base)
	legacy.FinishAt(errors.New("时钟回拨"), base.Add(-2*time.Second))
	if legacy.DurationMs != 0 || legacy.QueueDurationMs != 0 || legacy.Steps[0].DurationMs != 0 {
		t.Fatal("clock rollback produced negative duration")
	}
}

func TestInterruptedQueueDoesNotCountExecution(t *testing.T) {
	base := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	job := Job{Status: JobQueued, QueuedAt: &base, Steps: NewSteps("部署")}
	job.FinishAt(errors.New("服务重启"), base.Add(5*time.Second))
	if job.DurationMs != 0 || job.QueueDurationMs != 5000 || job.StartedAt != nil {
		t.Fatalf("queue interruption timing: %#v", job)
	}
}
