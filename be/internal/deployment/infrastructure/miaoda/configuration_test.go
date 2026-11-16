package miaoda

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDeploymentDocuments(t *testing.T) {
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.md")
	automationPath := filepath.Join(dir, "automation.md")
	envPath := filepath.Join(dir, "env.md")

	rolesText := "```yaml\nroles:\n  - role_id: system_admin\n    name: 系统管理员\n    description: \"完整权限\"\n    admin: true\n```\n"
	automationText := "```yaml\nautomations:\n  - name: hourly\n    description: 每小时执行\n    trigger_type: cron\n    cron: \"0 * * * *\"\n    timezone: Asia/Shanghai\n    status: enabled\n```\n"
	envText := "```yaml\nenv_vars:\n  - key: API_KEY\n    value: secret\n    envs: [dev, online]\n```\n"
	// 写入三类部署文档。
	for path, content := range map[string]string{
		rolesPath: rolesText, automationPath: automationText, envPath: envText,
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	roles, adminID, err := parseRolesDocument(rolesPath)
	if err != nil || len(roles) != 1 || adminID != "system_admin" {
		t.Fatalf("角色解析异常：roles=%#v admin=%s err=%v", roles, adminID, err)
	}
	triggers, err := parseAutomationDocument(automationPath)
	if err != nil || len(triggers) != 1 {
		t.Fatalf("自动化解析异常：triggers=%#v err=%v", triggers, err)
	}
	condition, ok := triggers[0]["cron_condition"].(map[string]any)
	if !ok || condition["cron"] != "0 * * * *" {
		t.Fatalf("cron 条件解析异常：%#v", triggers[0])
	}
	envs, err := parseEnvDocument(envPath)
	if err != nil || len(envs) != 1 || len(envs[0].Envs) != 2 || !envs[0].HasValue {
		t.Fatalf("环境变量解析异常：envs=%#v err=%v", envs, err)
	}
}

func TestParseDeploymentDocumentsMissingFile(t *testing.T) {
	roles, adminID, err := parseRolesDocument(filepath.Join(t.TempDir(), "missing.md"))
	if err != nil || len(roles) != 0 || adminID != "" {
		t.Fatalf("缺失文档应返回空结果：roles=%#v admin=%s err=%v", roles, adminID, err)
	}
}
