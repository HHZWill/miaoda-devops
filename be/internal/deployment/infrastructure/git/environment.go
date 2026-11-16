package git

import "strings"

// NonInteractiveEnvironment 禁止终端、图形 askpass 和凭据管理器询问密码。
func NonInteractiveEnvironment(env []string) []string {
	result := make([]string, 0, len(env)+4)
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "SSH_ASKPASS", "GCM_INTERACTIVE":
			continue
		}
		result = append(result, item)
	}
	return append(result, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/usr/bin/false",
		"SSH_ASKPASS=/usr/bin/false", "GCM_INTERACTIVE=never")
}
