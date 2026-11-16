package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"app-deployment-platform/internal/deployment/domain"
)

type FileJobRepository struct {
	root string
	mu   sync.Mutex
}

func NewFileJobRepository(root string) (*FileJobRepository, error) {
	// 初始化任务与导入文件目录。
	for _, name := range []string{root, filepath.Join(root, "jobs"), filepath.Join(root, "imports")} {
		if err := os.MkdirAll(name, 0700); err != nil {
			return nil, err
		}
	}
	return &FileJobRepository{root: root}, nil
}

func (r *FileJobRepository) Root() string {
	return r.root
}

func (r *FileJobRepository) JobDir(id string) string {
	return filepath.Join(r.root, "jobs", id)
}

func (r *FileJobRepository) ImportDir(id string) string {
	return filepath.Join(r.root, "imports", id)
}

func (r *FileJobRepository) Save(job *domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	job.UpdatedAt = time.Now()
	return WriteJSON(filepath.Join(r.JobDir(job.ID), "job.json"), job)
}

func (r *FileJobRepository) Get(id, profile string) (domain.Job, error) {
	var job domain.Job
	if profile == "" || !domain.ObjectIDPattern.MatchString(id) {
		return job, domain.ErrJobNotFound
	}
	if ReadJSON(filepath.Join(r.JobDir(id), "job.json"), &job) != nil || job.Profile != profile {
		return job, domain.ErrJobNotFound
	}
	return job, nil
}

func (r *FileJobRepository) List() ([]domain.Job, error) {
	entries, err := os.ReadDir(filepath.Join(r.root, "jobs"))
	if err != nil {
		return nil, err
	}
	jobs := []domain.Job{}
	// 只读取合法任务目录中的任务快照。
	for _, entry := range entries {
		var job domain.Job
		if entry.IsDir() && domain.ObjectIDPattern.MatchString(entry.Name()) &&
			ReadJSON(filepath.Join(r.JobDir(entry.Name()), "job.json"), &job) == nil {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func ReadJSON(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func WriteJSON(path string, value any) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".json-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := json.NewEncoder(tmp).Encode(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
