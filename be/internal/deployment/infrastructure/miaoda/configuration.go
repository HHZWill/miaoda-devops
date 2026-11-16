package miaoda

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var yamlBlockPattern = regexp.MustCompile("(?is)```(?:yaml|yml)\\s*(.*?)```")

type roleDefinition struct {
	RoleID      string
	Name        string
	Description string
	Admin       bool
}

type envDefinition struct {
	Key      string
	Value    string
	Envs     []string
	HasValue bool
}

// parseDocumentItems 解析部署文档中的简单 YAML 列表。
func parseDocumentItems(path, root string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	body := string(raw)
	if match := yamlBlockPattern.FindStringSubmatch(body); len(match) == 2 {
		body = match[1]
	}
	items := []map[string]any{}
	var current map[string]any
	// 按行解析部署产物约定的扁平列表。
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || line == root+":" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			if current != nil {
				items = append(items, current)
			}
			current = map[string]any{}
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
		if current == nil {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if found {
			current[strings.TrimSpace(key)] = parseDocumentValue(value)
		}
	}
	if current != nil {
		items = append(items, current)
	}
	return items, nil
}

// parseDocumentValue 解析字符串、布尔值和内联数组。
func parseDocumentValue(raw string) any {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.EqualFold(value, "true") {
		return true
	}
	if strings.EqualFold(value, "false") {
		return false
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return []string{}
		}
		values := []string{}
		// 内联数组仅允许标量值。
		for _, item := range strings.Split(inner, ",") {
			values = append(values, unquoteDocumentValue(item))
		}
		return values
	}
	return unquoteDocumentValue(value)
}

// unquoteDocumentValue 去除 YAML 标量引号。
func unquoteDocumentValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	if decoded, err := strconv.Unquote(value); err == nil {
		return decoded
	}
	return value
}

// parseRolesDocument 读取应用角色定义。
func parseRolesDocument(path string) ([]roleDefinition, string, error) {
	items, err := parseDocumentItems(path, "roles")
	if err != nil {
		return nil, "", err
	}
	roles, adminID := []roleDefinition{}, ""
	// 丢弃缺少编码或名称的非法条目。
	for _, item := range items {
		role := roleDefinition{
			RoleID:      strings.TrimSpace(toString(item["role_id"])),
			Name:        strings.TrimSpace(toString(item["name"])),
			Description: strings.TrimSpace(toString(item["description"])),
			Admin:       item["admin"] == true,
		}
		if role.RoleID == "" || role.Name == "" {
			continue
		}
		roles = append(roles, role)
		if role.Admin && adminID == "" {
			adminID = role.RoleID
		}
	}
	return roles, adminID, nil
}

// parseAutomationDocument 读取自动化触发器定义。
func parseAutomationDocument(path string) ([]map[string]any, error) {
	items, err := parseDocumentItems(path, "automations")
	if err != nil {
		return nil, err
	}
	triggers := []map[string]any{}
	// 转换为妙搭触发器接口结构。
	for _, item := range items {
		name := strings.TrimSpace(toString(item["name"]))
		if name == "" {
			continue
		}
		triggerType := defaultString(toString(item["trigger_type"]), "cron")
		trigger := map[string]any{
			"name":         name,
			"trigger_type": triggerType,
			"status":       defaultString(toString(item["status"]), "enabled"),
		}
		if description := strings.TrimSpace(toString(item["description"])); description != "" {
			trigger["description"] = description
		}
		switch triggerType {
		case "cron":
			trigger["cron_condition"] = map[string]any{
				"cron":     defaultString(toString(item["cron"]), "0 * * * *"),
				"timezone": defaultString(toString(item["timezone"]), "Asia/Shanghai"),
			}
		case "record_change":
			table, event := strings.TrimSpace(toString(item["table"])), strings.ToUpper(strings.TrimSpace(toString(item["event"])))
			if table != "" && event != "" {
				condition := map[string]any{"table": table, "event": event}
				if fields := stringSlice(item["fields"]); len(fields) > 0 {
					condition["fields"] = fields
				}
				trigger["record_change_condition"] = condition
			}
		case "webhook":
			trigger["webhook_condition"] = map[string]any{"white_ip_list": stringSlice(item["white_ip_list"])}
		case "feishu_approval":
			code, event := strings.TrimSpace(toString(item["approval_code"])), strings.TrimSpace(toString(item["event_type"]))
			if code != "" && event != "" {
				condition := map[string]any{"approval_code": code, "event_type": event}
				if event == "approval_instance" {
					condition["instance_status"] = stringSlice(item["instance_status"])
				}
				if event == "approval_task" {
					condition["task_status"] = stringSlice(item["task_status"])
				}
				trigger["feishu_approval_condition"] = condition
			}
		}
		triggers = append(triggers, trigger)
	}
	return triggers, nil
}

// parseEnvDocument 读取环境变量定义。
func parseEnvDocument(path string) ([]envDefinition, error) {
	items, err := parseDocumentItems(path, "env_vars")
	if err != nil {
		return nil, err
	}
	definitions := []envDefinition{}
	// 环境为空时默认同步到 dev 和 online。
	for _, item := range items {
		key := strings.TrimSpace(toString(item["key"]))
		if key == "" {
			continue
		}
		value := toString(item["value"])
		envs := stringSlice(item["envs"])
		if len(envs) == 0 {
			envs = []string{"dev", "online"}
		}
		valid := []string{}
		for _, env := range envs {
			if env == "dev" || env == "online" {
				valid = append(valid, env)
			}
		}
		if len(valid) == 0 {
			valid = []string{"dev", "online"}
		}
		definitions = append(definitions, envDefinition{Key: key, Value: value, Envs: valid, HasValue: value != ""})
	}
	return definitions, nil
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return strings.TrimSpace(string(mustJSON(value)))
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case string:
		values := []string{}
		// 兼容逗号分隔列表。
		for _, item := range strings.Split(typed, ",") {
			if item = strings.TrimSpace(item); item != "" {
				values = append(values, item)
			}
		}
		return values
	default:
		return nil
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
