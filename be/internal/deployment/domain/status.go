package domain

const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobSuccess = "success"
	JobFailed  = "failed"

	StepPending = "pending"
	StepRunning = "running"
	StepSuccess = "success"
	StepFailed  = "failed"
	StepBlocked = "blocked"
	StepSkipped = "skipped"
	StepWarning = "warning"
)

var FullDeploySteps = []string{
	"创建/复用应用", "拉取应用代码", "覆盖工程目录", "提交并推送代码",
	"执行 DDL", "创建应用角色", "创建自动化", "设置环境变量", "发布上线",
}
