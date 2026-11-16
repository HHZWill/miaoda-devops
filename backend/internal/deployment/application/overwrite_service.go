package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	"app-deployment-platform/internal/deployment/domain"
)

type OverwritePreview struct {
	Plan         domain.OverwritePlan `json:"plan"`
	Confirmation string               `json:"confirmation"`
}

func overwriteAppID(address string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Port() == "" {
		host := strings.ToLower(parsed.Hostname())
		if host == "miaoda.feishu.cn" || strings.HasSuffix(host, ".feishuapp.com") || strings.HasSuffix(host, ".aiforce.cloud") {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) >= 2 && parts[0] == "app" && domain.AppIDPattern.MatchString(parts[1]) {
				return parts[1], nil
			}
		}
	}
	return "", errors.New("请输入有效的妙搭 HTTPS 应用地址，路径需包含 /app/app_…")
}

func (s *Service) PreviewOverwrite(ctx context.Context, profile, importID, targetURL string) (OverwritePreview, int, error) {
	var preview OverwritePreview
	id, err := overwriteAppID(targetURL)
	if err != nil {
		return preview, 400, err
	}
	imported, status, err := s.deploymentPackage(importID)
	if err != nil {
		return preview, status, err
	}
	if imported.Meta.App.Type != "full_stack" {
		return preview, 400, errors.New("覆盖部署需要全栈妙搭安装包")
	}
	account, err := s.Lark.SelectProfile(ctx, profile, true)
	if err != nil {
		return preview, 401, err
	}
	work, err := os.MkdirTemp(s.Root, ".overwrite-")
	if err != nil {
		return preview, 500, errors.New("无法创建目标检查工作目录")
	}
	defer os.RemoveAll(work)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	preview.Plan, err = s.Miaoda.InspectOverwrite(ctx, account.Name, id, work)
	if err != nil {
		return preview, 422, err
	}
	data, err := json.Marshal(preview.Plan)
	if err != nil {
		return preview, 500, errors.New("无法生成覆盖确认信息")
	}
	digest := sha256.Sum256([]byte(account.Name + "\x00" + importID + "\x00" + string(data)))
	preview.Confirmation = hex.EncodeToString(digest[:])
	return preview, 200, nil
}

func (s *Service) StartOverwrite(ctx context.Context, profile, importID, targetURL, confirmedID, confirmation string) (domain.Job, int, error) {
	id, err := overwriteAppID(targetURL)
	if err != nil {
		return domain.Job{}, 400, err
	}
	if id != strings.TrimSpace(confirmedID) || len(confirmation) != 64 {
		return domain.Job{}, 400, errors.New("请检查覆盖范围并输入目标应用 ID 确认")
	}
	preview, status, err := s.PreviewOverwrite(ctx, profile, importID, targetURL)
	if err != nil {
		return domain.Job{}, status, err
	}
	if preview.Confirmation != confirmation {
		return domain.Job{}, 409, errors.New("目标资源已变化，请重新检查覆盖范围")
	}
	imported, status, err := s.deploymentPackage(importID)
	if err != nil {
		return domain.Job{}, status, err
	}
	labels := []string{"校验并解压安装包"}
	for _, step := range domain.FullDeploySteps {
		if step == "提交并推送代码" {
			labels = append(labels, domain.ResetTargetStep)
		}
		labels = append(labels, step)
	}
	job := domain.Job{
		Kind: "deploy", Profile: profile, Name: preview.Plan.App.Name, Source: imported.Meta.App,
		ImportID: importID, TargetID: preview.Plan.App.ID, Overwrite: &preview.Plan, Steps: domain.NewSteps(labels...),
		OverwriteConfirmation: preview.Confirmation,
	}
	return s.newJob(job)
}
