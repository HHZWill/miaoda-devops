package miaoda

import (
	"errors"
	"fmt"
)

// automationRequestBody 将导出字段映射为创建接口字段，条件对象保持完整。
func automationRequestBody(trigger map[string]any) (map[string]any, error) {
	typeName, _ := trigger["trigger_type"].(string)
	switch typeName {
	case "cron", "record_change", "webhook", "feishu_approval":
	default:
		return nil, fmt.Errorf("不支持的自动化类型：%s", typeName)
	}
	condition, ok := trigger["trigger_condition"].(map[string]any)
	if !ok || condition == nil {
		return nil, errors.New("自动化缺少有效的 trigger_condition")
	}
	body := make(map[string]any, len(trigger))
	for key, value := range trigger {
		if key != "trigger_condition" {
			body[key] = value
		}
	}
	body[typeName+"_condition"] = condition
	return body, nil
}
