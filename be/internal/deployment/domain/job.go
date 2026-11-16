package domain

import (
	"errors"
	"time"
)

type App struct {
	ID          string `json:"app_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"app_type"`
	Published   bool   `json:"is_published"`
}

type FailureInfo struct {
	Step    string   `json:"step,omitempty"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
}

type Job struct {
	ID                    string         `json:"id"`
	Kind                  string         `json:"kind"`
	Profile               string         `json:"profile"`
	Name                  string         `json:"name"`
	Source                App            `json:"source"`
	ImportID              string         `json:"importId,omitempty"`
	Status                string         `json:"status"`
	CreatedAt             time.Time      `json:"createdAt"`
	UpdatedAt             time.Time      `json:"updatedAt"`
	Steps                 []Step         `json:"steps"`
	Message               string         `json:"message,omitempty"`
	Failure               *FailureInfo   `json:"failure,omitempty"`
	ResumeFrom            string         `json:"resumeFrom,omitempty"`
	TargetID              string         `json:"targetId,omitempty"`
	Overwrite             *OverwritePlan `json:"overwrite,omitempty"`
	OverwriteConfirmation string         `json:"overwriteConfirmation,omitempty"`
	ResetDatabases        []string       `json:"resetDatabases,omitempty"`
	ReleaseID             string         `json:"releaseId,omitempty"`
	OnlineURL             string         `json:"onlineUrl,omitempty"`
	QueuedAt              *time.Time     `json:"queuedAt,omitempty"`
	StartedAt             *time.Time     `json:"startedAt,omitempty"`
	FinishedAt            *time.Time     `json:"finishedAt,omitempty"`
	DurationMs            int64          `json:"durationMs"`
	QueueDurationMs       int64          `json:"queueDurationMs"`
	TimingIncomplete      bool           `json:"timingIncomplete,omitempty"`
	TimingEstimated       bool           `json:"timingEstimated,omitempty"`
}

type failureReporter interface {
	FailureInfo() FailureInfo
}

func (j *Job) Start(now time.Time) {
	// 重复开始通知不得重置执行段。
	if j.Status == JobRunning {
		return
	}
	j.QueueDurationMs += elapsedMilliseconds(j.QueuedAt, now)
	j.Status, j.StartedAt, j.FinishedAt = JobRunning, &now, nil
}

func (j Job) ExecutionDuration(now time.Time) int64 {
	// 运行中的当前执行段尚未计入累计值。
	if j.Status == JobRunning {
		return j.DurationMs + elapsedMilliseconds(j.StartedAt, now)
	}
	return j.DurationMs
}

func (j Job) QueueDuration(now time.Time) int64 {
	// 排队等待与实际执行分别累计。
	if j.Status == JobQueued {
		return j.QueueDurationMs + elapsedMilliseconds(j.QueuedAt, now)
	}
	return j.QueueDurationMs
}

func (j *Job) Finish(err error) {
	j.FinishAt(err, time.Now())
}

func (j *Job) FinishAt(err error, now time.Time) {
	// 已结算任务不重复累计时间。
	if j.Status == JobSuccess || j.Status == JobFailed {
		return
	}
	// 仅计入实际执行，排队另行结算。
	if j.Status == JobRunning {
		j.DurationMs += elapsedMilliseconds(j.StartedAt, now)
	} else if j.Status == JobQueued {
		j.QueueDurationMs += elapsedMilliseconds(j.QueuedAt, now)
	}
	j.FinishedAt = &now
	if err == nil {
		j.Status = JobSuccess
		j.Failure = nil
		// 结算仍处于执行中的末尾步骤。
		for i := range j.Steps {
			if j.Steps[i].Status == StepRunning {
				j.Steps[i].SetStatus(StepSuccess, j.Steps[i].Detail, now)
			}
		}
		return
	}
	j.Status, j.Message = JobFailed, err.Error()
	failedStep := ""
	// 标记失败节点并阻断后续节点。
	for i := range j.Steps {
		switch j.Steps[i].Status {
		case StepRunning:
			j.Steps[i].SetStatus(StepFailed, err.Error(), now)
			failedStep = j.Steps[i].Label
		case StepPending:
			j.Steps[i].Status = StepBlocked
		}
	}
	var reporter failureReporter
	if errors.As(err, &reporter) {
		info := reporter.FailureInfo()
		j.Failure = &info
		return
	}
	j.Failure = &FailureInfo{Step: failedStep, Summary: err.Error()}
}

func (j *Job) PrepareRetry() error {
	return j.PrepareRetryAt(time.Now())
}

func (j *Job) PrepareRetryAt(now time.Time) error {
	resumeIndex := -1
	// 优先从明确失败的节点继续。
	for i := range j.Steps {
		if j.Steps[i].Status == StepFailed {
			resumeIndex = i
			break
		}
	}
	// 兼容未记录失败节点的中断任务。
	if resumeIndex < 0 {
		for i := range j.Steps {
			if j.Steps[i].Status == StepBlocked || j.Steps[i].Status == StepPending {
				resumeIndex = i
				break
			}
		}
	}
	if resumeIndex < 0 {
		return errors.New("当前任务没有可继续的中断节点")
	}
	// 旧任务重试仅能统计本次新增的执行时间。
	if j.QueuedAt == nil && j.StartedAt == nil {
		j.TimingIncomplete = true
	}
	j.ResumeFrom = j.Steps[resumeIndex].Key
	j.Status, j.Message, j.Failure, j.UpdatedAt = JobQueued, "", nil, now
	j.QueuedAt, j.StartedAt, j.FinishedAt = &now, nil, nil
	// 仅重置中断节点及其后续节点。
	for i := resumeIndex; i < len(j.Steps); i++ {
		// 旧节点的既往耗时缺失，后续统计需标记为部分记录。
		if j.Steps[i].StartedAt == nil &&
			(j.Steps[i].Status == StepFailed || j.Steps[i].Status == StepSuccess || j.Steps[i].Status == StepWarning) {
			j.Steps[i].TimingIncomplete = true
		}
		j.Steps[i].Status, j.Steps[i].Detail = StepPending, ""
		j.Steps[i].StartedAt, j.Steps[i].FinishedAt = nil, nil
	}
	return nil
}
