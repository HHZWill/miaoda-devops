package miaoda

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/larkcli"
)

var roleIDPattern = regexp.MustCompile("^[A-Za-z0-9_-]{1,64}$")

type overwritePage[T any] struct {
	Data struct {
		Items []T    `json:"items"`
		More  bool   `json:"has_more"`
		Token string `json:"page_token"`
		Next  string `json:"next_page_token"`
	} `json:"data"`
}

func overwriteItems[T any](ctx context.Context, c *Client, profile, dir, appID, command string, flags ...string) ([]T, error) {
	var items []T
	var cursor string
	seen := map[string]bool{}
	for {
		args := []string{"apps", command, "--app-id", appID, "--as", "user", "--json"}
		args = append(args, flags...)
		if cursor != "" {
			args = append(args, "--page-token="+cursor)
		}
		var page overwritePage[T]
		if err := c.Lark.Run(ctx, dir, profile, &page, args...); err != nil {
			return nil, err
		}
		if page.Data.Items == nil {
			return nil, fmt.Errorf("%s 未返回有效的项目清单", command)
		}
		items = append(items, page.Data.Items...)
		if !page.Data.More {
			return items, nil
		}
		cursor = page.Data.Next
		if cursor == "" {
			cursor = page.Data.Token
		}
		if cursor == "" || seen[cursor] {
			return nil, fmt.Errorf("%s 清单分页不完整", command)
		}
		seen[cursor] = true
	}
}

// InspectOverwrite 只读取待初始化的应用资源，返回可展示的影响范围。
func (c *Client) InspectOverwrite(ctx context.Context, profile, appID, work string) (domain.OverwritePlan, error) {
	var plan domain.OverwritePlan
	if !domain.AppIDPattern.MatchString(appID) {
		return plan, errors.New("目标应用 ID 无效")
	}
	var detail struct {
		Data struct {
			App domain.App `json:"app"`
		} `json:"data"`
	}
	if err := c.Lark.Run(ctx, work, profile, &detail, "apps", "+get", "--app-id", appID, "--as", "user", "--json"); err != nil {
		return plan, fmt.Errorf("当前租户无法读取目标应用：%w", err)
	}
	plan.App = detail.Data.App
	if plan.App.ID != appID || plan.App.Name == "" || plan.App.Type != "full_stack" {
		return plan, errors.New("目标必须是当前租户可访问的全栈妙搭应用")
	}
	// 目标为双环境应用。平台返回的表名与估算行数构成确认清单。
	for _, env := range []string{"dev", "online"} {
		tables, err := c.inspectTables(ctx, profile, appID, env, work)
		if err != nil {
			return plan, fmt.Errorf("查询 %s 数据库失败：%w", env, err)
		}
		keys, err := c.inspectEnvKeys(ctx, profile, appID, env, work)
		if err != nil {
			return plan, fmt.Errorf("查询 %s 环境变量失败：%w", env, err)
		}
		if env == "dev" {
			plan.DevTables, plan.DevEnv = tables, keys
		} else {
			plan.OnlineTables, plan.OnlineEnv = tables, keys
		}
	}

	var enums struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.Lark.Run(ctx, work, profile, &enums, "apps", "+db-execute", "--app-id", appID,
		"--environment=dev", "--sql="+resetEnumQuery, "--as", "user", "--yes", "--json"); err != nil {
		return plan, fmt.Errorf("查询开发库枚举失败：%w", err)
	}
	if enums.Data == nil {
		return plan, errors.New("妙搭未返回有效的枚举清单")
	}
	for _, row := range enums.Data {
		if !validResetIdentifier(row.Name) {
			return plan, errors.New("妙搭返回无效的枚举名")
		}
		plan.DevEnums = append(plan.DevEnums, row.Name)
	}
	sort.Strings(plan.DevEnums)
	var err error
	plan.Roles, err = c.inspectRoles(ctx, profile, appID, work)
	if err != nil {
		return plan, fmt.Errorf("查询角色及成员失败：%w", err)
	}
	plan.Automations, err = c.inspectAutomations(ctx, profile, appID, work)
	if err != nil {
		return plan, fmt.Errorf("查询自动化失败：%w", err)
	}
	return plan, nil
}

const resetEnumQuery = `SELECT t.typname AS name
FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = current_schema() AND t.typtype = 'e'
AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_type'::regclass
  AND d.objid = t.oid AND d.deptype = 'e')
ORDER BY t.typname`

func (c *Client) inspectTables(ctx context.Context, profile, appID, env, work string) ([]domain.ResetTable, error) {
	type table struct {
		Name string `json:"name"`
		Rows int64  `json:"estimated_row_count"`
	}
	items, err := overwriteItems[table](ctx, c, profile, work, appID, "+db-table-list", "--page-size=100", "--environment="+env)
	if err != nil {
		return nil, err
	}
	var tables []domain.ResetTable
	for _, item := range items {
		if !validResetIdentifier(item.Name) {
			return nil, errors.New("妙搭返回无效的表名")
		}
		switch strings.ToLower(item.Name) {
		case "auth", "users":
			continue
		}
		tables = append(tables, domain.ResetTable{Name: item.Name, Rows: item.Rows})
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}

func (c *Client) inspectEnvKeys(ctx context.Context, profile, appID, env, work string) ([]string, error) {
	type variable struct {
		Key string `json:"key"`
	}
	items, err := overwriteItems[variable](ctx, c, profile, work, appID, "+env-list", "--environment="+env)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, item := range items {
		if item.Key == "" {
			return nil, errors.New("妙搭返回无效的环境变量名称")
		}
		if !reservedEnvKeys[item.Key] {
			keys = append(keys, item.Key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (c *Client) inspectRoles(ctx context.Context, profile, appID, work string) ([]domain.ResetRole, error) {
	type role struct {
		ID     string `json:"role_id"`
		Name   string `json:"name"`
		System bool   `json:"is_system"`
	}
	items, err := overwriteItems[role](ctx, c, profile, work, appID, "+role-list", "--page-size=100")
	if err != nil {
		return nil, err
	}
	var roles []domain.ResetRole
	for _, item := range items {
		if item.System {
			continue
		}
		if !roleIDPattern.MatchString(item.ID) || item.Name == "" {
			return nil, errors.New("妙搭返回无效的角色")
		}
		var members struct {
			Data struct {
				Users       []any `json:"users"`
				Departments []any `json:"departments"`
				Chats       []any `json:"chats"`
			} `json:"data"`
		}
		if err := c.Lark.Run(ctx, work, profile, &members, "apps", "+role-member-list",
			"--app-id", appID, "--role-id", item.ID, "--as", "user", "--json"); err != nil {
			return nil, err
		}
		roles = append(roles, domain.ResetRole{ID: item.ID, Name: item.Name,
			Users: len(members.Data.Users), Departments: len(members.Data.Departments), Chats: len(members.Data.Chats)})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles, nil
}

func (c *Client) inspectAutomations(ctx context.Context, profile, appID, work string) ([]string, error) {
	type automation struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	triggers, err := overwriteItems[automation](ctx, c, profile, work, appID, "+automation-list", "--page-size=100")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, trigger := range triggers {
		if trigger.Status == "enabled" {
			if trigger.Name == "" {
				return nil, errors.New("妙搭返回无效的自动化名称")
			}
			names = append(names, trigger.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func validResetIdentifier(value string) bool {
	return value != "" && len(value) <= 63 && !strings.ContainsRune(value, 0)
}

func quotedResetNames(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, `"`+strings.ReplaceAll(name, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, ", ")
}

func resetTableNames(tables []domain.ResetTable) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	return names
}

func resetRoleIDs(roles []domain.ResetRole) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}
	return ids
}

func resetSubset(current, approved []string) bool {
	for _, item := range current {
		if !slices.Contains(approved, item) {
			return false
		}
	}
	return true
}

func resetScopeMatches(current, approved domain.OverwritePlan) bool {
	return current.App.ID == approved.App.ID &&
		resetSubset(resetTableNames(current.DevTables), resetTableNames(approved.DevTables)) &&
		resetSubset(resetTableNames(current.OnlineTables), resetTableNames(approved.OnlineTables)) &&
		resetSubset(current.DevEnums, approved.DevEnums) &&
		resetSubset(resetRoleIDs(current.Roles), resetRoleIDs(approved.Roles)) &&
		resetSubset(current.Automations, approved.Automations) &&
		resetSubset(current.DevEnv, approved.DevEnv) &&
		resetSubset(current.OnlineEnv, approved.OnlineEnv)
}

func missingCLIRecord(err error) bool {
	var cliErr *larkcli.CLIError
	return errors.As(err, &cliErr) && cliErr.Code == 400002516
}

func resetDatabaseStatements(plan domain.OverwritePlan) map[string]string {
	statements := map[string]string{}
	var devSQL []string
	if len(plan.DevTables) > 0 {
		devSQL = append(devSQL, "DROP TABLE IF EXISTS "+quotedResetNames(resetTableNames(plan.DevTables))+" RESTRICT")
	}
	if len(plan.DevEnums) > 0 {
		devSQL = append(devSQL, "DROP TYPE IF EXISTS "+quotedResetNames(plan.DevEnums)+" RESTRICT")
	}
	if len(devSQL) > 0 {
		statements["dev"] = "BEGIN; " + strings.Join(devSQL, "; ") + "; COMMIT;"
	}
	if len(plan.OnlineTables) > 0 {
		deletes := make([]string, 0, len(plan.OnlineTables))
		// online 禁止 DDL，使用事务内 DML 清空数据。
		for _, name := range resetTableNames(plan.OnlineTables) {
			deletes = append(deletes, "DELETE FROM "+quotedResetNames([]string{name}))
		}
		statements["online"] = "BEGIN; " + strings.Join(deletes, "; ") + "; COMMIT;"
	}
	return statements
}

// ResetOverwriteTarget initializes only resources captured in the confirmed plan.
func (c *Client) ResetOverwriteTarget(ctx context.Context, job *domain.Job, work, source string) error {
	if job.Overwrite == nil || job.TargetID != job.Overwrite.App.ID {
		return errors.New("覆盖任务缺少已确认的目标")
	}
	if err := validateOverwritePackage(source); err != nil {
		return err
	}
	current, err := c.InspectOverwrite(ctx, job.Profile, job.TargetID, work)
	if err != nil {
		return err
	}
	if !resetScopeMatches(current, *job.Overwrite) {
		return errors.New("目标应用有新的资源，请重新检查覆盖范围后提交")
	}

	statements := resetDatabaseStatements(current)
	for _, env := range job.ResetDatabases {
		delete(statements, env)
	}

	// 先核验生成的请求，再停用自动化并执行已确认的清理。
	for _, env := range []string{"dev", "online"} {
		if sql := statements[env]; sql != "" {
			if err := c.resetSQL(ctx, job, work, env, sql, "--dry-run"); err != nil {
				return fmt.Errorf("预览 %s 数据库初始化失败：%w", env, err)
			}
		}
	}
	for _, name := range current.Automations {
		if err := c.Lark.Run(ctx, work, job.Profile, nil, "apps", "+automation-disable",
			"--app-id", job.TargetID, "--name="+name, "--as", "user", "--json"); err != nil {
			return fmt.Errorf("停用自动化 %s 失败：%w", name, err)
		}
	}
	for _, env := range []string{"dev", "online"} {
		if sql := statements[env]; sql != "" {
			if err := c.resetSQL(ctx, job, work, env, sql, "--yes"); err != nil {
				return fmt.Errorf("初始化 %s 数据库失败：%w", env, err)
			}
			job.ResetDatabases = append(job.ResetDatabases, env)
			if err := c.Jobs.Save(job); err != nil {
				return fmt.Errorf("%s 数据库已初始化，但保存进度失败：%w", env, err)
			}
		}
	}
	for _, role := range current.Roles {
		if err := c.Lark.Run(ctx, work, job.Profile, nil, "apps", "+role-delete", "--app-id", job.TargetID,
			"--role-id", role.ID, "--as", "user", "--yes", "--json"); err != nil {
			return fmt.Errorf("删除角色 %s 失败：%w", role.Name, err)
		}
	}
	for _, env := range []string{"dev", "online"} {
		keys := current.DevEnv
		if env == "online" {
			keys = current.OnlineEnv
		}
		if len(keys) == 0 {
			continue
		}
		args := []string{"apps", "+env-delete", "--app-id", job.TargetID, "--environment=" + env, "--as", "user", "--yes", "--json"}
		for _, key := range keys {
			args = append(args, "--key="+key)
		}
		if err := c.Lark.Run(ctx, work, job.Profile, nil, args...); err != nil {
			// 环境变量可能跨环境共用记录，重复删除按成功处理。
			if missingCLIRecord(err) {
				log.Printf("[overwrite] env_already_absent job_id=%s env=%s keys=%d", job.ID, env, len(keys))
				continue
			}
			return fmt.Errorf("删除 %s 环境变量失败：%w", env, err)
		}
	}
	return nil
}

func (c *Client) resetSQL(ctx context.Context, job *domain.Job, work, env, sql, mode string) error {
	return c.Lark.Run(ctx, work, job.Profile, nil, "apps", "+db-execute",
		"--app-id", job.TargetID, "--environment="+env, "--sql="+sql, "--as", "user", mode, "--json")
}

func validateOverwritePackage(source string) error {
	if _, err := readPackageJSON(source); err != nil {
		return err
	}
	path := filepath.Join(source, filepath.FromSlash(ddlRelativeDir))
	names, err := ddlFileNames(path)
	if err != nil {
		return err
	}
	if _, err := loadDDLUnits(path, names); err != nil {
		return err
	}
	_, err = loadDeploymentConfiguration(source)
	return err
}
