package miaoda

import (
	"context"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"time"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/larkcli"
)

func (s *Client) DeploySource(ctx context.Context, j *domain.Job, work, source string) error {
	const create = "创建/复用应用"
	const push = "提交并推送代码"
	const publish = "发布上线"

	resumeIndex := domain.StepIndex(j.Steps, j.ResumeFrom)
	setStep := func(key, status, detail string) {
		index := domain.StepIndex(j.Steps, key)
		// 保留断点前已完成步骤，仅在前置校验失败时覆盖。
		if resumeIndex > 0 && index >= 0 && index < resumeIndex && status != "failed" {
			return
		}
		s.SetStep(j, key, status, detail)
	}

	setStep(create, "running", "")
	if j.TargetID == "" {
		if err := s.checkTargetName(ctx, j); err != nil {
			return err
		}
		var created struct {
			Data struct {
				ID string `json:"app_id"`
			} `json:"data"`
		}
		if err := s.Lark.Run(ctx, work, j.Profile, &created, "apps", "+create", "--as", "user",
			"--app-type", j.Source.Type, "--name="+j.Name, "--description="+j.Source.Description, "--json"); err != nil {
			return errors.New("创建请求失败，请在妙搭检查是否已生成同名应用后再操作")
		}
		if !domain.AppIDPattern.MatchString(created.Data.ID) {
			return errors.New("妙搭未返回目标应用 ID，请在妙搭核对创建结果")
		}
		j.TargetID = created.Data.ID
		if err := s.Jobs.Save(j); err != nil {
			return errors.New("应用已创建，但无法保存目标应用 ID")
		}
	}
	setStep(create, "success", j.TargetID)
	if j.ReleaseID == "" {
		setStep(push, "running", "")
		if err := s.pushSource(ctx, j, work, source); err != nil {
			return err
		}
		setStep(push, "success", "")
	} else {
		setStep(push, "skipped", "代码已推送，继续查询上一次发布")
	}
	setStep(publish, "running", "")
	if j.ReleaseID == "" {
		var release struct {
			Data struct {
				ID string `json:"release_id"`
			} `json:"data"`
		}
		if err := s.Lark.Run(ctx, work, j.Profile, &release, "apps", "+release-create", "--as", "user",
			"--app-id", j.TargetID, "--branch", Branch, "--json"); err != nil {
			return err
		}
		if release.Data.ID == "" {
			return errors.New("妙搭未返回发布 ID，请检查目标应用发布记录")
		}
		j.ReleaseID = release.Data.ID
		if err := s.Jobs.Save(j); err != nil {
			return errors.New("无法保存发布 ID")
		}
	}
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	lastStatus := ""
	for {
		var result struct {
			Data struct {
				releaseStatus
				Release releaseStatus `json:"release"`
			} `json:"data"`
		}
		err := s.Lark.Run(pollCtx, work, j.Profile, &result, "apps", "+release-get", "--as", "user",
			"--app-id", j.TargetID, "--release-id="+j.ReleaseID, "--json")
		if err != nil {
			return err
		}
		status := result.Data.Release
		if status.Status == "" {
			status = result.Data.releaseStatus
		}
		if status.Status != lastStatus {
			log.Printf("[release] target_id=%s release_id=%s status=%q", j.TargetID, j.ReleaseID, status.Status)
			lastStatus = status.Status
		}
		switch status.Status {
		case "finished":
			if larkcli.HTTPSURL(status.URL) {
				j.OnlineURL = status.URL
			}
			setStep(publish, "success", "")
			return nil
		case "failed":
			j.ReleaseID = ""
			return errors.New("妙搭发布构建失败，请在目标应用的发布记录中查看错误")
		}
		select {
		case <-pollCtx.Done():
			return errors.New("发布状态查询超时，可重试查询同一个发布")
		case <-ticker.C:
		}
	}
}

type releaseStatus struct {
	Status string `json:"status"`
	URL    string `json:"online_url"`
}

func (s *Client) checkTargetName(ctx context.Context, j *domain.Job) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cursor := ""
	for {
		result, err := s.ListApps(ctx, j.Profile, j.Name, cursor, "")
		if err != nil {
			return err
		}
		for _, app := range result.Items {
			if app.Name == j.Name {
				return errors.New("目标租户已有同名应用，请返回导入区使用其他应用名称")
			}
		}
		next := result.NextCursor()
		if next == "" {
			return nil
		}
		if next == cursor {
			return errors.New("无法完成应用重名检查，请稍后重试")
		}
		cursor = next
	}
}

func (s *Client) pushSource(ctx context.Context, j *domain.Job, work, source string) error {
	var credentials struct {
		Data struct {
			Repo  string `json:"repository_url"`
			Name  string `json:"commit_author_name"`
			Email string `json:"commit_author_email"`
		} `json:"data"`
	}
	if err := s.Lark.Run(ctx, work, j.Profile, &credentials, "apps", "+git-credential-init", "--as", "user", "--app-id", j.TargetID, "--json"); err != nil {
		return err
	}
	repo := credentials.Data.Repo
	env, err := s.Lark.GitEnvironment(j.Profile, j.TargetID)
	if err != nil {
		return err
	}
	address, err := url.Parse(repo)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		return errors.New("妙搭未返回有效的 Git 仓库地址")
	}
	target := filepath.Join(work, "repo")
	if _, err := s.Git.CommandWithEnvironment(ctx, work, env, "clone", "--depth=1", "--", repo, target); err != nil {
		return err
	}
	if _, err := s.Git.Command(ctx, target, "checkout", Branch); err != nil {
		if _, err := s.Git.Command(ctx, target, "checkout", "-b", Branch); err != nil {
			return err
		}
	}
	if err := s.Git.ReplaceSource(ctx, source, target); err != nil {
		return err
	}
	if _, err := s.Git.Command(ctx, target, "add", "-A"); err != nil {
		return err
	}
	changed, err := s.Git.Command(ctx, target, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if changed != "" {
		name, email := credentials.Data.Name, credentials.Data.Email
		if name == "" {
			name = "Miaoda Deployment"
		}
		if email == "" {
			email = "deployment@noreply"
		}
		if _, err := s.Git.Command(ctx, target, "-c", "user.name="+name, "-c", "user.email="+email,
			"commit", "-m", "Deploy Miaoda package"); err != nil {
			return err
		}
	}
	_, err = s.Git.CommandWithEnvironment(ctx, target, env, "push", "origin", Branch)
	return err
}
