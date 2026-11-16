package miaoda

import (
	"encoding/json"
	"strings"
	"testing"
)

// 验证分号切分能跳过注释、字符串与 dollar 引用
func TestSplitSQL(t *testing.T) {
	content := `-- 头部注释
CREATE TABLE a (id bigint);
/* 块注释; 不是分隔符 */
CREATE OR REPLACE FUNCTION f() RETURNS int AS $function$
BEGIN
  RETURN 1;
END;
$function$;
INSERT INTO a VALUES ('x;y');
`
	statements := splitSQL(content)
	if len(statements) != 3 {
		t.Fatalf("期望 3 条语句，实际 %d：%#v", len(statements), statements)
	}
	if !strings.Contains(statements[1], "RETURN 1;") {
		t.Fatalf("函数体被错误切分：%s", statements[1])
	}
	if !strings.Contains(statements[2], "'x;y'") {
		t.Fatalf("字符串内分号被错误切分：%s", statements[2])
	}
}

// 验证建表、索引与 INSERT 的幂等改写
func TestMakeDDLStatement(t *testing.T) {
	table := makeDDLStatement(`CREATE TABLE "t1" (id bigint);`).sql
	if !strings.Contains(table, "CREATE TABLE IF NOT EXISTS") {
		t.Fatalf("建表未补 IF NOT EXISTS：%s", table)
	}
	index := makeDDLStatement(`CREATE UNIQUE INDEX idx_t1 ON t1 (id);`).sql
	if !strings.Contains(index, "CREATE UNIQUE INDEX IF NOT EXISTS") {
		t.Fatalf("索引未补 IF NOT EXISTS：%s", index)
	}
	sequence := makeDDLStatement(`CREATE SEQUENCE seq_id;`).sql
	if !strings.Contains(sequence, "CREATE SEQUENCE IF NOT EXISTS") {
		t.Fatalf("序列未补 IF NOT EXISTS：%s", sequence)
	}
	insert := makeDDLStatement(`INSERT INTO t1 (id) VALUES (1);`).sql
	if !strings.HasSuffix(strings.TrimSpace(insert), "ON CONFLICT DO NOTHING;") {
		t.Fatalf("INSERT 未补 ON CONFLICT：%s", insert)
	}
	// 已带 IF NOT EXISTS 的语句不得重复追加
	existedIndex := `CREATE INDEX IF NOT EXISTS idx_t1 ON t1 (id);`
	if got := makeDDLStatement(existedIndex).sql; strings.Count(strings.ToUpper(got), "IF NOT EXISTS") != 1 {
		t.Fatalf("索引重复追加 IF NOT EXISTS：%s", got)
	}
	existedTable := `CREATE TABLE IF NOT EXISTS t1 (id bigint);`
	if got := makeDDLStatement(existedTable).sql; strings.Count(strings.ToUpper(got), "IF NOT EXISTS") != 1 {
		t.Fatalf("建表重复追加 IF NOT EXISTS：%s", got)
	}
	// 已带冲突处理的语句保持原样
	kept := `INSERT INTO t1 (id) VALUES (1) ON CONFLICT DO NOTHING;`
	if got := makeDDLStatement(kept).sql; got != kept {
		t.Fatalf("已有 ON CONFLICT 不应改写：%s", got)
	}
	// 无改写规则的语句保留原始注释
	plain := "-- 注释\nCREATE VIEW v1 AS SELECT 1;"
	if got := makeDDLStatement(plain).sql; got != plain {
		t.Fatalf("普通语句不应被改写：%s", got)
	}
}

// 验证建表前自动提取函数与序列依赖。
func TestCollectPreTableDDL(t *testing.T) {
	sequence := `CREATE SEQUENCE ai_calc_snowflake_state;`
	required := `CREATE OR REPLACE FUNCTION workspace_source.ai_calc_next_id()
RETURNS bigint LANGUAGE plpgsql AS $function$
BEGIN
  RETURN nextval('ai_calc_snowflake_state');
END;
$function$;`
	unrelated := `CREATE OR REPLACE FUNCTION workspace_source.after_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END; $$;`
	trigger := `CREATE TRIGGER trg AFTER INSERT ON demo EXECUTE FUNCTION after_insert();`
	units := []ddlUnit{
		{name: "20_tables_001.sql", stmts: []string{
			`CREATE TABLE demo (id bigint DEFAULT ai_calc_next_id ());`,
		}},
		{name: "40_post.sql", stmts: []string{sequence, required, unrelated, trigger}},
	}
	early, selected := collectPreTableDDL(units)
	if len(early) != 2 || early[0] != sequence || early[1] != required {
		t.Fatalf("前置依赖错误：%#v", early)
	}
	if !selected[sequence] || !selected[required] || selected[unrelated] || selected[trigger] {
		t.Fatalf("前置依赖标记错误：%#v", selected)
	}
}

// 验证类型与约束通过存在性检查跳过，而不是 DO 块
func TestMakeDDLStatementGuard(t *testing.T) {
	typeItem := makeDDLStatement(`CREATE TYPE tabledefs AS ENUM ('A', 'B');`)
	if typeItem.guard == nil || typeItem.guard.kind != "type" || typeItem.guard.name != "tabledefs" {
		t.Fatalf("类型未生成守卫：%#v", typeItem.guard)
	}
	constraintItem := makeDDLStatement(`ALTER TABLE "ai_case" ADD CONSTRAINT "ai_case_pkey" PRIMARY KEY (id);`)
	if constraintItem.guard == nil || constraintItem.guard.kind != "constraint" ||
		constraintItem.guard.owner != "ai_case" || constraintItem.guard.name != "ai_case_pkey" {
		t.Fatalf("约束未生成守卫：%#v", constraintItem.guard)
	}
	// 带 schema 前缀的名称需要归一化
	schemaItem := makeDDLStatement(`CREATE TYPE public."tabledefs" AS ENUM ('A');`)
	if schemaItem.guard == nil || schemaItem.guard.name != "tabledefs" {
		t.Fatalf("带 schema 的类型名未归一化：%#v", schemaItem.guard)
	}
}

// 验证旧安装包内重复枚举标签可安全修复。
func TestDeduplicateEnumLabels(t *testing.T) {
	input := `CREATE TYPE tabledefs AS ENUM ('A', 'B', 'PKEY_INTERNAL', 'PKEY_INTERNAL', 'B', 'x,y', 'x,y', 'it''s', 'it''s');`
	item := makeDDLStatement(input)
	if item.repairedEnumLabelCount != 4 {
		t.Fatalf("重复标签计数错误：%d", item.repairedEnumLabelCount)
	}
	if got := item.sql; got != `CREATE TYPE tabledefs AS ENUM ('A', 'B', 'PKEY_INTERNAL', 'x,y', 'it''s');` {
		t.Fatalf("枚举修复错误：%s", got)
	}
	if item.guard == nil || item.guard.name != "tabledefs" {
		t.Fatalf("修复后丢失类型守卫：%#v", item.guard)
	}
}

// 验证正常或无法确认语义的枚举不会被改写。
func TestDeduplicateEnumLabelsKeepsValidSQL(t *testing.T) {
	inputs := []string{
		`CREATE TYPE state AS ENUM ('A', 'B');`,
		`CREATE TYPE state AS ENUM (lower('A'), 'B');`,
		`CREATE TYPE state AS ENUM ('A', 'B);`,
	}
	// 逐条确认非重复定义保持原文。
	for _, input := range inputs {
		got, removed := deduplicateEnumLabels(input)
		if got != input || removed != 0 {
			t.Fatalf("不应改写：%s -> %s (%d)", input, got, removed)
		}
	}
}

// 验证策略与触发器先删后建
func TestMakeDDLStatementRecreate(t *testing.T) {
	policy := makeDDLStatement(`CREATE POLICY "查看" ON "t1" AS PERMISSIVE FOR SELECT USING (true);`).sql
	if !strings.HasPrefix(policy, `DROP POLICY IF EXISTS "查看" ON "t1";`) {
		t.Fatalf("策略未先删除：%s", policy)
	}
	trigger := makeDDLStatement(`CREATE TRIGGER trg_a BEFORE INSERT ON "t1" FOR EACH ROW EXECUTE FUNCTION f();`).sql
	if !strings.HasPrefix(trigger, `DROP TRIGGER IF EXISTS trg_a ON "t1";`) {
		t.Fatalf("触发器未先删除：%s", trigger)
	}
}

// 平台 SQL 接口不支持 DO 块，任何改写都不得生成
func TestNoDoBlock(t *testing.T) {
	inputs := []string{
		`CREATE TYPE t AS ENUM ('A');`,
		`ALTER TABLE t ADD CONSTRAINT c PRIMARY KEY (id);`,
		`CREATE POLICY p ON t USING (true);`,
		`CREATE TABLE t (id bigint);`,
		`CREATE INDEX i ON t (id);`,
		`INSERT INTO t VALUES (1);`,
		`CREATE TRIGGER g BEFORE INSERT ON t EXECUTE FUNCTION f();`,
	}
	for _, input := range inputs {
		got := makeDDLStatement(input).sql
		if strings.Contains(strings.ToUpper(got), "DO $") {
			t.Fatalf("生成了不支持的 DO 块：%s", got)
		}
	}
}

// 验证 workspace 角色与 schema 重写
func TestRewriteWorkspace(t *testing.T) {
	input := `CREATE POLICY p ON t TO "authenticated_workspace_source1" USING (true);`
	got := rewriteWorkspace(input, "target2")
	if !strings.Contains(got, "authenticated_workspace_target2") {
		t.Fatalf("角色未重写：%s", got)
	}
	if strings.Contains(got, "source1") {
		t.Fatalf("仍残留源后缀：%s", got)
	}
	schema := rewriteWorkspace(`CREATE FUNCTION workspace_source1.f() RETURNS int`, "target2")
	if !strings.Contains(schema, "workspace_target2.f()") {
		t.Fatalf("schema 未重写：%s", schema)
	}
}

// 验证从 pg_roles 结果中选取出现最多的后缀
func TestDetectWorkspaceSuffix(t *testing.T) {
	rows := []map[string]any{
		{"rolname": "anon_workspace_abc123"},
		{"rolname": "authenticated_workspace_abc123"},
		{"rolname": "service_role_workspace_abc123"},
		{"rolname": "user_authenticated_workspace_abc123"},
		{"rolname": "anon_workspace_zzz999"},
	}
	if got := detectWorkspaceSuffix(rows); got != "abc123" {
		t.Fatalf("期望 abc123，实际 %s", got)
	}
}

// 验证 SQL 接口二次 JSON 编码结果的展开
func TestParseSQLResult(t *testing.T) {
	inner := `[{"rolname":"anon_workspace_abc"}]`
	encoded, _ := json.Marshal([]string{inner})
	rows := parseSQLResult(encoded)
	if len(rows) != 1 || rows[0]["rolname"] != "anon_workspace_abc" {
		t.Fatalf("解析结果异常：%#v", rows)
	}
	if got := parseSQLResult(json.RawMessage(`""`)); len(got) != 0 {
		t.Fatalf("空结果应返回空集合：%#v", got)
	}
}

// 验证存在性集合的键构造
func TestDDLGuardKeys(t *testing.T) {
	rows := []map[string]any{
		{"kind": "type", "owner_name": "", "object_name": "tabledefs"},
		{"kind": "constraint", "owner_name": "ai_case", "object_name": "ai_case_pkey"},
	}
	existing := ddlGuardKeys(rows)
	if !existing[ddlGuard{kind: "type", name: "tabledefs"}.key()] {
		t.Fatal("类型守卫键缺失")
	}
	if !existing[ddlGuard{kind: "constraint", owner: "ai_case", name: "ai_case_pkey"}.key()] {
		t.Fatal("约束守卫键缺失")
	}
}

// 验证平台默认策略与扩展对象被过滤
func TestDDLStatementFilters(t *testing.T) {
	if !isPlatformPolicy(`CREATE POLICY "service_role_bypass_policy" ON t USING (true);`) {
		t.Fatal("平台默认策略未被识别")
	}
	if isPlatformPolicy(`CREATE POLICY "业务策略" ON t USING (true);`) {
		t.Fatal("业务策略被误过滤")
	}
	if !isExtensionObject(`CREATE OR REPLACE VIEW pg_stat_statements AS SELECT 1;`) {
		t.Fatal("扩展视图未被识别")
	}
}

// 验证阶段顺序校验
func TestDDLPhaseOrder(t *testing.T) {
	if ddlPhaseOrder(ddlPhase("10_types.sql")) >= ddlPhaseOrder(ddlPhase("20_tables_001.sql")) {
		t.Fatal("types 必须早于 tables")
	}
	if ddlPhaseOrder(ddlPhase("40_post.sql")) >= ddlPhaseOrder(ddlPhase("90_inserts_roles.sql")) {
		t.Fatal("post 必须早于 inserts")
	}
}
