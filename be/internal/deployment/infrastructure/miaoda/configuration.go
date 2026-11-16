package miaoda

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type roleDefinition struct {
	RoleID      string `json:"role_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Admin       bool   `json:"admin"`
}

type envDefinition struct {
	Key      string
	Value    string
	Envs     []string
	HasValue bool
}

type deploymentConfiguration struct {
	Roles       []roleDefinition
	AdminID     string
	Automations []map[string]any
	Environment []envDefinition
}

type configurationDeclaration struct {
	Count              *int     `json:"count"`
	SHA256             string   `json:"sha256"`
	ManualRequiredKeys []string `json:"manual_required_keys"`
}

type configurationDocument struct {
	FormatVersion      int               `json:"format_version"`
	Kind               string            `json:"kind"`
	Roles              []json.RawMessage `json:"roles"`
	Automations        []json.RawMessage `json:"automations"`
	Environment        []json.RawMessage `json:"env_vars"`
	ManualRequiredKeys []string          `json:"manual_required_keys"`
}

// loadDeploymentConfiguration 按 manifest 校验全部配置后再允许部署。
func loadDeploymentConfiguration(source string) (deploymentConfiguration, error) {
	var result deploymentConfiguration
	docs := filepath.Join(source, assembleRelativeDir)
	raw, err := os.ReadFile(filepath.Join(docs, "manifest.json"))
	if err != nil {
		return result, fmt.Errorf("无法读取部署配置清单 manifest.json：%w", err)
	}
	var manifest struct {
		FormatVersion int                                 `json:"format_version"`
		Kind          string                              `json:"kind"`
		Config        map[string]configurationDeclaration `json:"config"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.FormatVersion != 1 || manifest.Kind != "deployment_manifest" {
		return result, errors.New("manifest.json 格式无效或版本不受支持")
	}
	for _, spec := range []struct{ key, file, kind string }{
		{"roles", "roles.json", "roles"},
		{"automation", "automation.json", "automations"},
		{"env", "env.json", "env_vars"},
	} {
		declaration, ok := manifest.Config[spec.key]
		if !ok || declaration.Count == nil || *declaration.Count < 0 {
			return result, fmt.Errorf("manifest.json 缺少有效的 config.%s 声明", spec.key)
		}
		document, items, err := readConfigurationJSON(docs, spec.file, spec.kind, declaration)
		if err != nil {
			return result, err
		}
		switch spec.key {
		case "roles":
			for i, raw := range items {
				var role roleDefinition
				if json.Unmarshal(raw, &role) != nil || strings.TrimSpace(role.RoleID) == "" || strings.TrimSpace(role.Name) == "" {
					return result, fmt.Errorf("roles.json 第 %d 项缺少有效角色编码或名称", i+1)
				}
				result.Roles = append(result.Roles, role)
				if role.Admin && result.AdminID == "" {
					result.AdminID = role.RoleID
				}
			}
		case "automation":
			for i, raw := range items {
				var trigger map[string]any
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				if decoder.Decode(&trigger) != nil {
					return result, fmt.Errorf("automation.json 第 %d 项格式无效", i+1)
				}
				for _, key := range []string{"name", "trigger_type", "status"} {
					value, ok := trigger[key].(string)
					if !ok || strings.TrimSpace(value) == "" {
						return result, fmt.Errorf("automation.json 第 %d 项缺少有效的 %s", i+1, key)
					}
				}
				if condition, ok := trigger["trigger_condition"].(map[string]any); !ok || condition == nil {
					return result, fmt.Errorf("automation.json 第 %d 项缺少有效的 trigger_condition", i+1)
				}
				if _, err := automationRequestBody(trigger); err != nil {
					return result, fmt.Errorf("automation.json 第 %d 项：%w", i+1, err)
				}
				// 保留导出结构，在请求边界转换条件字段名。
				result.Automations = append(result.Automations, trigger)
			}
		case "env":
			manual := []string{}
			for i, raw := range items {
				var item struct {
					Key      string   `json:"key"`
					Value    *string  `json:"value"`
					Envs     []string `json:"envs"`
					HasValue *bool    `json:"has_value"`
				}
				if json.Unmarshal(raw, &item) != nil || strings.TrimSpace(item.Key) == "" ||
					item.HasValue == nil || len(item.Envs) == 0 {
					return result, fmt.Errorf("env.json 第 %d 项缺少有效的 key、envs 或 has_value", i+1)
				}
				for _, env := range item.Envs {
					if env != "dev" && env != "online" {
						return result, fmt.Errorf("env.json 第 %d 项包含不支持的环境", i+1)
					}
				}
				value := ""
				if item.Value != nil {
					value = *item.Value
				}
				if (*item.HasValue && item.Value == nil) || (!*item.HasValue && value != "") {
					return result, fmt.Errorf("env.json 第 %d 项的 value 与 has_value 不一致", i+1)
				}
				result.Environment = append(result.Environment, envDefinition{
					Key: item.Key, Value: value, Envs: item.Envs, HasValue: *item.HasValue,
				})
				if !*item.HasValue {
					manual = append(manual, strings.Join(item.Envs, "/")+":"+item.Key)
				}
			}
			if !slices.Equal(document.ManualRequiredKeys, declaration.ManualRequiredKeys) ||
				!slices.Equal(document.ManualRequiredKeys, manual) {
				return result, errors.New("env.json 的 manual_required_keys 与配置或 manifest.json 不一致")
			}
		}
	}
	return result, nil
}

func readConfigurationJSON(docs, file, kind string, declaration configurationDeclaration) (configurationDocument, []json.RawMessage, error) {
	var document configurationDocument
	raw, err := os.ReadFile(filepath.Join(docs, file))
	if err != nil {
		return document, nil, fmt.Errorf("无法读取部署配置 %s：%w", file, err)
	}
	hash := sha256.Sum256(raw)
	if !strings.EqualFold(declaration.SHA256, hex.EncodeToString(hash[:])) {
		return document, nil, fmt.Errorf("%s 的 SHA256 与 manifest.json 不一致", file)
	}
	if json.Unmarshal(raw, &document) != nil || document.FormatVersion != 1 || document.Kind != kind {
		return document, nil, fmt.Errorf("%s 格式无效或版本不受支持", file)
	}
	var items []json.RawMessage
	switch kind {
	case "roles":
		items = document.Roles
	case "automations":
		items = document.Automations
	case "env_vars":
		items = document.Environment
	}
	if items == nil {
		return document, nil, fmt.Errorf("%s 缺少配置数组，空配置必须显式声明 []", file)
	}
	if len(items) != *declaration.Count {
		return document, nil, fmt.Errorf("%s 的配置数量与 manifest.json 不一致", file)
	}
	return document, items, nil
}
