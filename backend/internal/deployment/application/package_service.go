package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"app-deployment-platform/internal/deployment/domain"
	"app-deployment-platform/internal/deployment/infrastructure/artifact"
	"app-deployment-platform/internal/deployment/infrastructure/persistence"
)

func (s *Service) ImportPackage(name string, input io.Reader) (domain.Attachment, int, error) {
	item := domain.Attachment{ID: newID(), Name: name}
	log.Printf("[artifact] import_started import_id=%s filename=%q", item.ID, name)
	dir := s.Jobs.ImportDir(item.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		return item, 500, errors.New("无法保存导入文件")
	}
	saved := false
	defer func() {
		if !saved {
			os.RemoveAll(dir)
		}
	}()
	filename := filepath.Join(dir, "package.zip")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return item, 500, errors.New("无法保存导入文件")
	}
	item.Size, err = io.Copy(file, io.LimitReader(input, artifact.MaxUpload+1))
	closeErr := file.Close()
	if item.Size > artifact.MaxUpload {
		log.Printf("[artifact] import_failed import_id=%s size=%d reason=size_limit", item.ID, item.Size)
		return item, 413, errors.New("安装包大小超过 256 MB")
	}
	if err != nil || closeErr != nil {
		return item, 400, errors.New("上传中断或写入失败")
	}
	item.Meta, err = s.Archive.Inspect(filename)
	if err != nil {
		log.Printf("[artifact] import_failed import_id=%s size=%d error=%q", item.ID, item.Size, err)
		return item, 400, err
	}
	if err := persistence.WriteJSON(filepath.Join(dir, "import.json"), item); err != nil {
		return item, 500, errors.New("无法保存安装包信息")
	}
	saved = true
	log.Printf("[artifact] import_finished import_id=%s size=%d app_type=%s app_id=%s",
		item.ID, item.Size, item.Meta.App.Type, item.Meta.App.ID)
	return item, 201, nil
}

func (s *Service) PackagePath(id, profile string) (domain.Job, string, error) {
	job, err := s.Jobs.Get(id, profile)
	if err != nil || job.Kind != "package" || job.Status != domain.JobSuccess {
		return domain.Job{}, "", errors.New("安装包不存在或尚未完成")
	}
	return job, filepath.Join(s.Jobs.JobDir(job.ID), "package.zip"), nil
}

func (s *Service) packageJob(ctx context.Context, job *domain.Job) error {
	const check = "检查应用权限"
	const source = "获取应用源码"
	const config = "导出部署配置"
	const build = "生成安装包"
	const verify = "校验安装包"

	dir := s.Jobs.JobDir(job.ID)
	log.Printf("[package] started job_id=%s source_app_id=%s", job.ID, job.Source.ID)
	s.Miaoda.SetStep(job, check, domain.StepRunning, "")
	var result struct {
		Data struct {
			ID          string     `json:"app_id"`
			Name        string     `json:"name"`
			Description string     `json:"description"`
			Type        string     `json:"app_type"`
			Published   bool       `json:"is_published"`
			App         domain.App `json:"app"`
		} `json:"data"`
	}
	if err := s.Lark.Run(ctx, dir, job.Profile, &result,
		"apps", "+get", "--app-id", job.Source.ID, "--as", "user", "--json"); err != nil {
		return err
	}
	app := result.Data.App
	if app.Name == "" {
		app = domain.App{
			ID: result.Data.ID, Name: result.Data.Name, Description: result.Data.Description,
			Type: result.Data.Type, Published: result.Data.Published,
		}
	}
	app.ID = job.Source.ID
	if app.Name == "" || !domain.SupportedAppType(app.Type) {
		return errors.New("妙搭未返回可打包的应用类型或应用名称")
	}
	job.Source, job.Name = app, app.Name
	log.Printf("[package] app_loaded job_id=%s app_id=%s app_type=%s name=%q", job.ID, app.ID, app.Type, app.Name)
	s.Miaoda.SetStep(job, check, domain.StepSuccess, "")

	work, err := os.MkdirTemp(dir, "export-")
	if err != nil {
		return errors.New("无法创建打包目录")
	}
	defer os.RemoveAll(work)
	rawArchive := filepath.Join(work, "source.zip")
	s.Miaoda.SetStep(job, source, domain.StepRunning, "")
	if app.Type == "full_stack" {
		entry, err := skillEntry("MIAODA_ASSEMBLE_SCRIPT", "miaoda-app-assemble", "export-miaoda-app.sh")
		if err != nil {
			return err
		}
		env, err := s.Lark.ScopedEnvironment(work, job.Profile)
		if err != nil {
			return err
		}
		onLine := func(line string) {
			switch {
			case strings.Contains(line, "Step 3/5"):
				s.Miaoda.SetStep(job, source, domain.StepSuccess, "")
				s.Miaoda.SetStep(job, config, domain.StepRunning, "")
			case strings.Contains(line, "Step 4/5"):
				s.Miaoda.SetStep(job, config, domain.StepSuccess, "")
				s.Miaoda.SetStep(job, build, domain.StepRunning, "")
			}
		}
		if err := s.Runner.Run(ctx, work, env, onLine, "bash", entry, app.ID, "--out-dir", work); err != nil {
			return fmt.Errorf("妙搭打包失败。%w", err)
		}
		archives, err := filepath.Glob(filepath.Join(work, "*.zip"))
		if err != nil || len(archives) != 1 {
			return errors.New("打包脚本未生成唯一的 ZIP 安装包")
		}
		rawArchive = archives[0]
		s.Miaoda.SetStep(job, source, domain.StepSuccess, "")
		s.Miaoda.SetStep(job, config, domain.StepSuccess, "")
	} else {
		if err := s.Lark.Run(ctx, work, job.Profile, nil,
			"apps", "+export", "--app-id", app.ID, "--output", "source.zip", "--as", "user"); err != nil {
			return err
		}
		s.Miaoda.SetStep(job, source, domain.StepSuccess, "")
		s.Miaoda.SetStep(job, config, domain.StepSkipped, "此应用类型只需打包源码")
	}
	s.Miaoda.SetStep(job, build, domain.StepRunning, "")
	target := filepath.Join(dir, "package.zip")
	if err := s.Archive.Create(rawArchive, target, app); err != nil {
		os.Remove(target)
		return err
	}
	s.Miaoda.SetStep(job, build, domain.StepSuccess, "")
	s.Miaoda.SetStep(job, verify, domain.StepRunning, "")
	info, err := os.Stat(target)
	if err != nil || info.Size() > artifact.MaxUpload {
		os.Remove(target)
		return errors.New("安装包无法读取或超过可导入的 256 MB 上限")
	}
	if _, err := s.Archive.Inspect(target); err != nil {
		os.Remove(target)
		return err
	}
	s.Miaoda.SetStep(job, verify, domain.StepSuccess, "")
	log.Printf("[package] finished job_id=%s archive=%q size=%d", job.ID, target, info.Size())
	return nil
}

func skillEntry(env, skill, script string) (string, error) {
	if path := os.Getenv(env); path != "" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return filepath.Abs(path)
		}
		return "", fmt.Errorf("请检查 %s 指向的脚本路径", env)
	}
	home, _ := os.UserHomeDir()
	// 查找已安装的打包 Skill。
	for _, root := range []string{".agents", ".codex"} {
		path := filepath.Join(home, root, "skills", skill, "scripts", script)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 %s 技能脚本，请安装该技能或设置 %s", skill, env)
}
