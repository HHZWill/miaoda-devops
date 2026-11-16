package miaoda

import (
	"strings"
	"testing"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/larkcli"
)

func TestResetDatabaseStatements(t *testing.T) {
	plan := domain.OverwritePlan{
		DevTables:    []domain.ResetTable{{Name: "parent"}, {Name: "child"}},
		OnlineTables: []domain.ResetTable{{Name: "parent"}, {Name: "child"}},
		DevEnums:     []string{"state"},
	}
	statements := resetDatabaseStatements(plan)
	if !strings.Contains(statements["dev"], `DROP TABLE IF EXISTS "parent", "child" RESTRICT`) ||
		!strings.Contains(statements["dev"], `DROP TYPE IF EXISTS "state" RESTRICT`) {
		t.Fatalf("dev 初始化语句错误：%s", statements["dev"])
	}
	if strings.Contains(statements["online"], "TRUNCATE") ||
		!strings.Contains(statements["online"], `DELETE FROM "parent"; DELETE FROM "child"`) {
		t.Fatalf("online 必须使用 DML：%s", statements["online"])
	}
	if !strings.HasPrefix(statements["online"], "BEGIN;") || !strings.HasSuffix(statements["online"], "COMMIT;") {
		t.Fatalf("online 初始化缺少事务：%s", statements["online"])
	}
}

func TestMissingCLIRecord(t *testing.T) {
	if !missingCLIRecord(&larkcli.CLIError{Code: 400002516}) {
		t.Fatal("记录不存在应视为幂等成功")
	}
	if missingCLIRecord(&larkcli.CLIError{Code: 400002476}) {
		t.Fatal("其他 CLI 错误不得忽略")
	}
}
