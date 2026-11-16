package miaoda

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDDLUnitsPreFunctions(t *testing.T) {
	dir := t.TempDir()
	files := []struct {
		name  string
		sql   string
		order int
	}{
		{"10_types.sql", "CREATE TYPE state AS ENUM ('ready');", 10},
		{"15_pre_functions_001.sql", "CREATE SEQUENCE demo_ids;", 15},
		{"15_pre_functions_002.sql", "CREATE FUNCTION next_id() RETURNS bigint LANGUAGE sql AS $$ SELECT nextval('demo_ids'); $$;", 15},
		{"20_tables_001.sql", "CREATE TABLE demo (id bigint DEFAULT next_id());", 20},
		{"20_tables_002.sql", "CREATE TABLE other (state state);", 20},
		{"30_foreign_keys_001.sql", "ALTER TABLE other ADD COLUMN demo_id bigint REFERENCES demo(id);", 30},
		{"40_post.sql", "CREATE VIEW demo_view AS SELECT * FROM demo;", 40},
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(file.sql), 0600); err != nil {
			t.Fatal(err)
		}
	}
	names, err := ddlFileNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	units, err := loadDDLUnits(dir, names)
	if err != nil {
		t.Fatalf("有效的前置函数与建表顺序被拒绝：%v", err)
	}
	if len(units) != len(files) {
		t.Fatalf("DDL 文件丢失：得到 %d，期望 %d", len(units), len(files))
	}
	for i, file := range files {
		if units[i].name != file.name || len(units[i].stmts) != 1 || units[i].stmts[0] != file.sql {
			t.Fatalf("DDL 顺序或内容被改变：%#v", units[i])
		}
		if order := ddlPhaseOrder(ddlPhase(file.name)); order != file.order {
			t.Fatalf("%s 阶段序号错误：得到 %d，期望 %d", file.name, order, file.order)
		}
	}
	early, selected := collectPreTableDDL(units)
	if len(early) != 0 || len(selected) != 0 {
		t.Fatal("独立前置文件中的函数与序列不应被提取后重复执行")
	}
}

func TestLoadDDLUnitsRejectsPreFunctionsAfterTables(t *testing.T) {
	dir := t.TempDir()
	names := []string{"20_tables_001.sql", "15_pre_functions_001.sql"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := loadDDLUnits(dir, names)
	if err == nil || !strings.Contains(err.Error(), "DDL 阶段顺序错误：15_pre_functions_001.sql") {
		t.Fatalf("真正倒退的 DDL 阶段必须被拒绝，得到：%v", err)
	}
}
