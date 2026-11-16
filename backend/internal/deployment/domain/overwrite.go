package domain

const ResetTargetStep = "初始化目标应用"

type ResetTable struct {
	Name string `json:"name"`
	Rows int64  `json:"estimatedRows"`
}

type ResetRole struct {
	ID          string `json:"role_id"`
	Name        string `json:"name"`
	Users       int    `json:"users"`
	Departments int    `json:"departments"`
	Chats       int    `json:"chats"`
}

type OverwritePlan struct {
	App          App          `json:"app"`
	DevTables    []ResetTable `json:"devTables"`
	OnlineTables []ResetTable `json:"onlineTables"`
	DevEnums     []string     `json:"devEnums"`
	Roles        []ResetRole  `json:"roles"`
	Automations  []string     `json:"automations"`
	DevEnv       []string     `json:"devEnv"`
	OnlineEnv    []string     `json:"onlineEnv"`
}
