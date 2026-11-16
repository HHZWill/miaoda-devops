package miaoda

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/larkcli"
)

func TestJSONConfigurationReachesDeploymentAPI(t *testing.T) {
	source := writeConfigurationFixture(t, configurationFixtures())
	config, err := loadDeploymentConfiguration(source)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	logPath := filepath.Join(work, "requests.jsonl")
	binary := filepath.Join(work, "lark-cli")
	script := `#!/bin/sh
if [ "$1" = contact ]; then
  printf '{"ok":true,"data":{"user":{"open_id":"ou_test"}}}\n'
  exit 0
fi
[ "$1" = api ] || exit 1
method="$2"
path="$3"
shift 3
body='{}'
while [ "$#" -gt 0 ]; do
  if [ "$1" = --data ]; then body="$2"; shift; fi
  shift
done
case "$method:$path" in
  POST:*/triggers)
    case "$body" in
      *'"trigger_condition":'*)
        printf '{"ok":false,"error":{"message":"Invalid trigger condition"}}\n'
        exit 1
        ;;
    esac
    ;;
esac
printf '{"method":"%s","path":"%s","body":%s}\n' "$method" "$path" "$body" >> "$TEST_API_LOG"
printf '{"ok":true,"data":{"items":[]}}\n'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAODA_LARK_CLI", binary)
	t.Setenv("TEST_API_LOG", logPath)
	client := &Client{Lark: larkcli.NewClient(work)}
	job := &domain.Job{TargetID: "app_test", Profile: "test-profile"}
	ctx := context.Background()
	if err := client.deployRoles(ctx, job, work, config.Roles, config.AdminID); err != nil {
		t.Fatal(err)
	}
	if err := client.deployAutomations(ctx, job, work, config.Automations); err != nil {
		t.Fatal(err)
	}
	if err := client.deployEnvironment(ctx, job, work, config.Environment); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	roles, triggers, envs := 0, 0, 0
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var request struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		}
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.UseNumber()
		if err := decoder.Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "POST" {
			continue
		}
		switch {
		case strings.HasSuffix(request.Path, "/roles"):
			roles++
			if request.Body["role_id"] != "admin" || request.Body["name"] != "管理员" {
				t.Fatal("角色请求与安装包不一致")
			}
		case strings.HasSuffix(request.Path, "/triggers"):
			triggers++
			typeName := request.Body["trigger_type"].(string)
			if _, ok := request.Body["trigger_condition"]; ok {
				t.Fatal("创建接口不接受导出结构中的 trigger_condition 字段")
			}
			condition, ok := request.Body[typeName+"_condition"].(map[string]any)
			if !ok {
				t.Fatal("自动化请求缺少类型对应的条件字段")
			}
			for _, trigger := range config.Automations {
				if trigger["name"] == request.Body["name"] && !reflect.DeepEqual(condition, trigger["trigger_condition"]) {
					t.Fatal("自动化嵌套条件在接口字段转换中被改变")
				}
			}
		case strings.HasSuffix(request.Path, "/create_or_update_env_var"):
			envs++
			switch request.Body["key"] {
			case "MULTILINE":
				if request.Body["value"] != " first\n\"quoted\"\\path\nlast " {
					t.Fatal("环境变量值在接口传输中被改变")
				}
			case "EMPTY":
				if request.Body["value"] != "" {
					t.Fatal("真实空值未正确写入")
				}
			case "SPLIT":
				expected := "development"
				if request.Body["env"] == "online" {
					expected = "production"
				}
				if request.Body["value"] != expected {
					t.Fatal("不同环境的值被相互覆盖")
				}
			case "MISSING":
				t.Fatal("未回带的值不应覆盖目标环境变量")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if roles != 1 || triggers != 4 || envs != 5 {
		t.Fatalf("配置被跳过或重复写入：roles=%d triggers=%d env_writes=%d", roles, triggers, envs)
	}
}

func TestAutomationFailureStopsDeploymentWithCause(t *testing.T) {
	work := t.TempDir()
	binary := filepath.Join(work, "lark-cli")
	script := `#!/bin/sh
if [ "$2" = GET ]; then
  printf '{"ok":true,"data":{"items":[]}}\n'
  exit 0
fi
printf '{"ok":false,"error":{"message":"Invalid trigger condition"}}\n'
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIAODA_LARK_CLI", binary)
	config, err := loadDeploymentConfiguration(writeConfigurationFixture(t, configurationFixtures()))
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{Lark: larkcli.NewClient(work)}
	err = client.deployAutomations(context.Background(), &domain.Job{TargetID: "app_test"}, work, config.Automations)
	if err == nil {
		t.Fatal("自动化创建失败不应继续发布")
	}
	for _, name := range []string{"hourly", "change", "hook", "approval"} {
		if !strings.Contains(err.Error(), name+"：Invalid trigger condition") {
			t.Fatal("失败信息没有保留触发器名称及接口原因")
		}
	}
}
