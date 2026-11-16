package httpapi

import (
	"net/http"
	"testing"
)

func TestShouldLogRequest(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		status int
		want   bool
	}{
		{name: "成功轮询", method: http.MethodGet, path: "/api/jobs", status: http.StatusOK, want: false},
		{name: "失败轮询", method: http.MethodGet, path: "/api/jobs", status: http.StatusInternalServerError, want: true},
		{name: "任务详情", method: http.MethodGet, path: "/api/jobs/id", status: http.StatusOK, want: true},
		{name: "创建任务", method: http.MethodPost, path: "/api/jobs", status: http.StatusAccepted, want: true},
	}
	// 覆盖轮询过滤边界。
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			if got := shouldLogRequest(item.method, item.path, item.status); got != item.want {
				t.Fatalf("shouldLogRequest() = %t, want %t", got, item.want)
			}
		})
	}
}
