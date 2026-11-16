package executor

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"app-deployment-platform/internal/deployment/domain"
)

var (
	ansi         = regexp.MustCompile("\x1b\\[[0-9;]*m")
	scriptStep   = regexp.MustCompile(`^(?:==>|!!) \[([^\]]+)\](?: [^，,]*[，,]?\s*(.*))?$`)
	bearerSecret = regexp.MustCompile(`(?i)\bBearer\s+\S+`)
	namedSecret  = regexp.MustCompile(`(?i)\b(token|secret|password|api[_-]?key)(\s*[:=]\s*)(\S+)`)
)

const maxFailureLines = 24

type ScriptFailure struct {
	Reason  string
	Step    string
	Details []string
}

func (e *ScriptFailure) Error() string {
	return e.Reason
}

func (e *ScriptFailure) FailureInfo() domain.FailureInfo {
	return domain.FailureInfo{Step: e.Step, Summary: e.Reason, Details: e.Details}
}

type progressWriter struct {
	pending []byte
	consume func(string)
}

func (p *progressWriter) Write(value []byte) (int, error) {
	length := len(value)
	// 按行交付脚本输出并限制单行长度。
	for len(value) > 0 {
		line, rest, end := bytes.Cut(value, []byte{'\n'})
		if len(p.pending)+len(line) <= 8192 {
			p.pending = append(p.pending, line...)
		}
		if end {
			p.consume(strings.TrimSpace(ansi.ReplaceAllString(string(p.pending), "")))
			p.pending = p.pending[:0]
		}
		value = rest
	}
	return length, nil
}

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) Run(ctx context.Context, dir string, env []string, callback func(string), name string, args ...string) error {
	started := time.Now()
	log.Printf("[script] started command=%q work_dir=%q", name, dir)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = dir, env, 5*time.Second
	reason, currentStep := "", ""
	recent := make([]string, 0, maxFailureLines)
	output := &progressWriter{consume: func(line string) {
		callback(line)
		// 跟踪当前步骤并保留末尾输出。
		if match := scriptStep.FindStringSubmatch(line); match != nil {
			currentStep = strings.TrimSpace(match[1])
			if strings.HasPrefix(line, "!!") && len(match) > 2 && strings.TrimSpace(match[2]) != "" {
				reason = strings.TrimSpace(match[2])
			}
		}
		if line != "" {
			line = bearerSecret.ReplaceAllString(line, "Bearer [已脱敏]")
			line = namedSecret.ReplaceAllString(line, "$1$2[已脱敏]")
			runes := []rune(line)
			if len(runes) > 800 {
				line = string(runes[:800]) + "…"
			}
			recent = append(recent, line)
			if len(recent) > maxFailureLines {
				recent = recent[len(recent)-maxFailureLines:]
			}
			log.Printf("[script] output command=%q line=%q", name, line)
		}
		switch {
		case strings.Contains(line, "缺少依赖命令"):
			reason = "服务器缺少打包部署工具依赖，请检查 README 中列出的命令。"
		case strings.Contains(line, "同名应用"):
			reason = "目标租户已有同名应用，请使用其他应用名称。"
		case strings.Contains(line, "missing_scope"):
			reason = "当前账号缺少妙搭权限，请为该租户重新授权。"
		case strings.Contains(line, "keychain Get failed"):
			reason = "无法访问 CLI 凭据，请检查系统钥匙串。"
		case strings.Contains(line, "发布超时"):
			reason = "发布状态查询超时，请在妙搭后台确认发布状态后再重试。"
		case strings.Contains(line, "发布失败"):
			reason = "妙搭发布构建失败，请在目标应用的发布记录中查看错误。"
		}
	}}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			reason = "执行超时或已中断"
		}
		if reason == "" {
			reason = "请检查当前步骤所需的租户权限、工具依赖和应用配置。"
		}
		failure := &ScriptFailure{Reason: reason, Step: currentStep, Details: recent}
		log.Printf("[script] finished command=%q status=failed duration_ms=%d step=%q error=%q",
			name, time.Since(started).Milliseconds(), currentStep, failure)
		return failure
	}
	log.Printf("[script] finished command=%q status=success duration_ms=%d", name, time.Since(started).Milliseconds())
	return nil
}
