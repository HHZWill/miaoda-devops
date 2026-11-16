package larkcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"app-deployment-platform/internal/deployment/infrastructure/git"
)

// GitEnvironment 只在当前子进程中绑定应用与账号，不修改用户的 Git 配置。
func (c *Client) GitEnvironment(profile, appID string) ([]string, error) {
	if profile == "" || appID == "" {
		return nil, errors.New("Git 认证缺少飞书账号或妙搭应用 ID")
	}
	binary, err := c.Binary()
	if err != nil {
		return nil, errors.New("未找到 lark-cli，无法初始化 Git 认证环境")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	env := git.NonInteractiveEnvironment(os.Environ())
	result := make([]string, 0, len(env)+9)
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		// 避免启动环境中的临时 Git 配置重新引入其他账号的 helper。
		if key == "GIT_CONFIG_COUNT" || key == "GIT_CONFIG_PARAMETERS" ||
			strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			continue
		}
		result = append(result, item)
	}
	helper := "!" + shellQuote(filepath.ToSlash(binary)) + " apps git-credential-helper --app-id " +
		shellQuote(appID) + " --profile " + shellQuote(profile)
	// 空 helper 清除系统与 URL-scoped helper，再启用当前任务的 CLI helper。
	return append(result, "GIT_CONFIG_COUNT=4",
		"GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1="+helper,
		"GIT_CONFIG_KEY_2=credential.interactive", "GIT_CONFIG_VALUE_2=false",
		// CLI 根据完整仓库 URL 查找凭据，必须保留 Git 传入 helper 的 path。
		"GIT_CONFIG_KEY_3=credential.useHttpPath", "GIT_CONFIG_VALUE_3=true"), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
