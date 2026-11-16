package domain

import (
	"errors"
	"regexp"
)

var (
	ErrJobNotFound  = errors.New("任务不存在")
	ObjectIDPattern = regexp.MustCompile("^[a-f0-9]{32}$")
	AppIDPattern    = regexp.MustCompile("^app_[a-zA-Z0-9_-]{1,80}$")
)

func SupportedAppType(appType string) bool {
	return appType == "full_stack" || appType == "frontend" || appType == "html"
}
