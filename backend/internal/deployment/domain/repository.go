package domain

type JobRepository interface {
	Save(job *Job) error
	Get(id, profile string) (Job, error)
	List() ([]Job, error)
	JobDir(id string) string
	ImportDir(id string) string
}

type Manifest struct {
	Version int `json:"version"`
	App     App `json:"app"`
}

type Attachment struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Size int64    `json:"size"`
	Meta Manifest `json:"meta"`
}
