package domain

import "time"

type Step struct {
	Key              string     `json:"key"`
	Label            string     `json:"label"`
	Status           string     `json:"status"`
	Detail           string     `json:"detail,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	DurationMs       int64      `json:"durationMs"`
	TimingIncomplete bool       `json:"timingIncomplete,omitempty"`
	TimingEstimated  bool       `json:"timingEstimated,omitempty"`
}

func (s *Step) SetStatus(status, detail string, now time.Time) {
	// 重复进度通知只更新文案，不重新计时。
	if s.Status == status {
		s.Detail = detail
		return
	}
	// 结束当前执行段并累计耗时。
	if s.Status == StepRunning {
		s.DurationMs += elapsedMilliseconds(s.StartedAt, now)
		s.FinishedAt = &now
	}
	// 新执行段保留此前累计耗时。
	if status == StepRunning {
		s.StartedAt, s.FinishedAt = &now, nil
	}
	// 未执行的跳过节点记录零耗时。
	if status == StepSkipped && s.StartedAt == nil {
		s.FinishedAt = &now
	}
	s.Status, s.Detail = status, detail
}

func (s Step) ExecutionDuration(now time.Time) int64 {
	// 运行中叠加尚未结算的执行段。
	if s.Status == StepRunning {
		return s.DurationMs + elapsedMilliseconds(s.StartedAt, now)
	}
	return s.DurationMs
}

func NewSteps(labels ...string) []Step {
	steps := make([]Step, 0, len(labels))
	// 初始化任务的全部执行步骤。
	for _, label := range labels {
		steps = append(steps, Step{Key: label, Label: label, Status: StepPending})
	}
	return steps
}

func StepIndex(steps []Step, key string) int {
	// 按稳定步骤键查找位置。
	for i := range steps {
		if steps[i].Key == key {
			return i
		}
	}
	return -1
}

func FullDeployStepIndex(key string) int {
	// 旧任务的独立处理节点统一从覆盖工程目录恢复。
	if key == "合并 package.json" || key == "修复 package.json 钩子兼容性" {
		key = "覆盖工程目录"
	}
	// 按全栈流程定义查找位置。
	for i, step := range FullDeploySteps {
		if step == key {
			return i
		}
	}
	return -1
}
