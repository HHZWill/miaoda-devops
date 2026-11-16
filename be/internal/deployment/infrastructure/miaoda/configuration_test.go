package miaoda

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"app-deployment-platform/internal/deployment/domain"
)

func configurationFixtures() map[string]string {
	return map[string]string{
		"roles.json": `{"format_version":1,"kind":"roles","roles":[{"role_id":"admin","name":"管理员","description":"完整权限","admin":true}]}`,
		"automation.json": `{"format_version":1,"kind":"automations","automations":[
			{"name":"hourly","trigger_type":"cron","status":"enabled","trigger_condition":{"cron":"0 * * * *","timezone":"Asia/Shanghai"}},
			{"name":"change","trigger_type":"record_change","status":"disabled","trigger_condition":{"table":"tasks","event":"UPDATE","fields":["status","owner"],"extra":{"limit":42949672960000001}}},
			{"name":"hook","trigger_type":"webhook","status":"disabled","trigger_condition":{"white_ip_list":["1.2.3.4"],"token_value":null}},
			{"name":"approval","trigger_type":"feishu_approval","status":"disabled","trigger_condition":{"event_type":"approval_instance","instance_status":["APPROVED"],"approval_code":"demo"}}
		]}`,
		"env.json": `{"format_version":1,"kind":"env_vars","env_vars":[
			{"key":"MULTILINE","value":" first\n\"quoted\"\\path\nlast ","envs":["dev","online"],"has_value":true},
			{"key":"EMPTY","value":"","envs":["dev"],"has_value":true},
			{"key":"SPLIT","value":"development","envs":["dev"],"has_value":true},
			{"key":"SPLIT","value":"production","envs":["online"],"has_value":true},
			{"key":"MISSING","value":"","envs":["online"],"has_value":false}
		],"manual_required_keys":["online:MISSING"]}`,
	}
}

func writeConfigurationFixture(t *testing.T, documents map[string]string) string {
	t.Helper()
	source := t.TempDir()
	docs := filepath.Join(source, assembleRelativeDir)
	if err := os.MkdirAll(docs, 0700); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{}
	for _, spec := range []struct{ key, file, root string }{
		{"roles", "roles.json", "roles"},
		{"automation", "automation.json", "automations"},
		{"env", "env.json", "env_vars"},
	} {
		raw := []byte(documents[spec.file])
		if err := os.WriteFile(filepath.Join(docs, spec.file), raw, 0600); err != nil {
			t.Fatal(err)
		}
		var document map[string]json.RawMessage
		_ = json.Unmarshal(raw, &document)
		var items []json.RawMessage
		_ = json.Unmarshal(document[spec.root], &items)
		hash := sha256.Sum256(raw)
		declaration := map[string]any{"count": len(items), "sha256": hex.EncodeToString(hash[:])}
		if spec.key == "env" {
			var manual []string
			_ = json.Unmarshal(document["manual_required_keys"], &manual)
			declaration["manual_required_keys"] = manual
		}
		config[spec.key] = declaration
	}
	manifest, err := json.Marshal(map[string]any{
		"format_version": 1, "kind": "deployment_manifest", "config": config,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestLoadDeploymentConfiguration(t *testing.T) {
	documents := configurationFixtures()
	configuration, err := loadDeploymentConfiguration(writeConfigurationFixture(t, documents))
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Roles) != 1 || configuration.AdminID != "admin" ||
		configuration.Roles[0].Name != "管理员" {
		t.Fatal("角色 JSON 配置未完整读取")
	}
	if len(configuration.Automations) != 4 {
		t.Fatal("自动化 JSON 配置丢失")
	}
	condition := configuration.Automations[1]["trigger_condition"].(map[string]any)
	if !reflect.DeepEqual(condition["fields"], []any{"status", "owner"}) ||
		condition["extra"].(map[string]any)["limit"] != json.Number("42949672960000001") {
		t.Fatal("自动化的嵌套条件、数组或数字精度被改变")
	}
	if _, exists := configuration.Automations[0]["cron_condition"]; exists {
		t.Fatal("自动化条件不应被重组为旧版字段")
	}
	env := configuration.Environment
	if len(env) != 5 || env[0].Value != " first\n\"quoted\"\\path\nlast " ||
		!reflect.DeepEqual(env[0].Envs, []string{"dev", "online"}) {
		t.Fatal("环境变量的空白、换行、引号、反斜杠或环境列表被改变")
	}
	if !env[1].HasValue || env[1].Value != "" || env[4].HasValue ||
		env[2].Value != "development" || env[3].Value != "production" {
		t.Fatal("空值、未回带值或不同环境的值未保持独立")
	}
}

func TestLoadDeploymentConfigurationEmpty(t *testing.T) {
	documents := map[string]string{
		"roles.json":      `{"format_version":1,"kind":"roles","roles":[]}`,
		"automation.json": `{"format_version":1,"kind":"automations","automations":[]}`,
		"env.json":        `{"format_version":1,"kind":"env_vars","env_vars":[],"manual_required_keys":[]}`,
	}
	configuration, err := loadDeploymentConfiguration(writeConfigurationFixture(t, documents))
	if err != nil || len(configuration.Roles)+len(configuration.Automations)+len(configuration.Environment) != 0 {
		t.Fatalf("明确声明的空配置应允许跳过：%v", err)
	}
}

func TestLoadDeploymentConfigurationRejectsInvalidDocuments(t *testing.T) {
	for _, test := range []struct{ name, file, document, expected string }{
		{"JSON损坏", "roles.json", `{`, "格式无效"},
		{"版本错误", "roles.json", `{"format_version":2,"kind":"roles","roles":[]}`, "版本不受支持"},
		{"类型错误", "roles.json", `{"format_version":1,"kind":"env_vars","roles":[]}`, "格式无效"},
		{"数组缺失", "roles.json", `{"format_version":1,"kind":"roles"}`, "缺少配置数组"},
		{"数组为null", "roles.json", `{"format_version":1,"kind":"roles","roles":null}`, "缺少配置数组"},
		{"无效角色", "roles.json", `{"format_version":1,"kind":"roles","roles":[{"name":"管理员"}]}`, "角色编码"},
		{"无效自动化", "automation.json", `{"format_version":1,"kind":"automations","automations":[{"name":"a","trigger_type":"cron","status":"enabled"}]}`, "trigger_condition"},
		{"缺少值声明", "env.json", `{"format_version":1,"kind":"env_vars","env_vars":[{"key":"k","value":"v","envs":["dev"]}]}`, "has_value"},
		{"环境错误", "env.json", `{"format_version":1,"kind":"env_vars","env_vars":[{"key":"k","value":"v","envs":["bad"],"has_value":true}]}`, "不支持的环境"},
		{"手动事项缺失", "env.json", `{"format_version":1,"kind":"env_vars","env_vars":[{"key":"k","value":"","envs":["dev"],"has_value":false}],"manual_required_keys":[]}`, "manual_required_keys"},
	} {
		t.Run(test.name, func(t *testing.T) {
			documents := configurationFixtures()
			documents[test.file] = test.document
			_, err := loadDeploymentConfiguration(writeConfigurationFixture(t, documents))
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("无效配置未被正确拒绝：%v", err)
			}
		})
	}
}

func TestLoadDeploymentConfigurationRejectsMissingAndChangedFiles(t *testing.T) {
	for _, file := range []string{"manifest.json", "roles.json", "automation.json", "env.json"} {
		t.Run(file, func(t *testing.T) {
			source := writeConfigurationFixture(t, configurationFixtures())
			if err := os.Remove(filepath.Join(source, assembleRelativeDir, file)); err != nil {
				t.Fatal(err)
			}
			_, err := loadDeploymentConfiguration(source)
			if err == nil || !strings.Contains(err.Error(), file) {
				t.Fatal("缺失的配置文件被当成空配置")
			}
			// 预检必须在创建应用、推送或执行 DDL 之前失败。
			if err := (&Client{}).DeployFullStack(context.Background(), &domain.Job{}, source, source); err == nil {
				t.Fatal("配置缺失仍允许开始部署")
			}
		})
	}
	source := writeConfigurationFixture(t, configurationFixtures())
	path := filepath.Join(source, assembleRelativeDir, "roles.json")
	if err := os.WriteFile(path, []byte(`{"format_version":1,"kind":"roles","roles":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDeploymentConfiguration(source); err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatal("配置篡改未被 manifest 校验发现")
	}
}

func TestLoadDeploymentConfigurationRejectsManifestMismatch(t *testing.T) {
	for _, change := range []string{"count", "missing_declaration", "version"} {
		t.Run(change, func(t *testing.T) {
			source := writeConfigurationFixture(t, configurationFixtures())
			path := filepath.Join(source, assembleRelativeDir, "manifest.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatal(err)
			}
			config := manifest["config"].(map[string]any)
			switch change {
			case "count":
				config["roles"].(map[string]any)["count"] = 0
			case "missing_declaration":
				delete(config, "env")
			case "version":
				manifest["format_version"] = 2
			}
			raw, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDeploymentConfiguration(source); err == nil {
				t.Fatal("manifest 配置不一致仍被接受")
			}
		})
	}
}
