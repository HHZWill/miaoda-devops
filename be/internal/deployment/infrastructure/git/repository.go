package git

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) Command(ctx context.Context, dir string, args ...string) (string, error) {
	started := time.Now()
	operation := "unknown"
	if len(args) > 0 {
		operation = args[0]
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Dir, cmd.WaitDelay = dir, 5*time.Second
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[git] finished operation=%s status=failed duration_ms=%d error=%q",
			operation, time.Since(started).Milliseconds(), err)
		return "", errors.New("Git 操作失败，请检查当前账号的妙搭仓库权限和目标分支状态")
	}
	log.Printf("[git] finished operation=%s status=success duration_ms=%d output_bytes=%d",
		operation, time.Since(started).Milliseconds(), len(out))
	return string(out), nil
}

func preservePath(relative string) bool {
	root, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	root = strings.ToLower(root)
	return root == ".git" || root == ".spark" || root == ".spark_project" ||
		root == ".agents" || root == ".claude" || strings.HasPrefix(root, ".env")
}

func (r *Repository) ReplaceSource(ctx context.Context, source, target string) error {
	started := time.Now()
	tracked, err := r.Command(ctx, target, "ls-files", "-z")
	if err != nil {
		return err
	}
	// 删除目标仓库中可覆盖的已跟踪文件。
	for _, relative := range strings.Split(tracked, "\x00") {
		if relative == "" || preservePath(relative) {
			continue
		}
		if err := os.Remove(filepath.Join(target, relative)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.New("无法清理目标项目模板文件")
		}
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		if preservePath(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		output := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(output, 0700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(output, raw, info.Mode().Perm())
	})
	if err != nil {
		log.Printf("[git] replace_source status=failed duration_ms=%d error=%q", time.Since(started).Milliseconds(), err)
		return err
	}
	log.Printf("[git] replace_source status=success duration_ms=%d source=%q target=%q",
		time.Since(started).Milliseconds(), source, target)
	return nil
}
