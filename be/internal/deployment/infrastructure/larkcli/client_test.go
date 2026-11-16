package larkcli

import "testing"

func TestCLIErrorFailedPrecondition(t *testing.T) {
	err := (&CLIError{
		Subtype: "failed_precondition",
		Message: "forbid ddl/dcl operation in online env",
	}).Error()
	if err != "online 环境禁止 DDL/DCL 操作，请改用数据操作或通过 dev 发布迁移。" {
		t.Fatalf("错误信息未保留失败前置条件：%s", err)
	}
}

func TestCLIErrorPreservesAPIMessage(t *testing.T) {
	err := (&CLIError{Type: "api", Subtype: "unknown", Code: 400002516, Message: "记录不存在"}).Error()
	if err != "记录不存在" {
		t.Fatalf("API 原始错误被覆盖：%s", err)
	}
}
