package httpapi

import (
	"net/http"

	environment "app-deployment-platform/internal/environment/application"
)

func registerEnvironment(mux *http.ServeMux, service *environment.Service) {
	// 初始化接口在账号连接前可用，仍受本机访问中间件保护。
	mux.HandleFunc("GET /api/environment", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, service.Status(r.Context()))
	})
	mux.HandleFunc("GET /api/environment/install", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, service.Installation())
	})
	mux.HandleFunc("POST /api/environment/install", func(w http.ResponseWriter, r *http.Request) {
		job, err := service.Start(r.Context())
		if err != nil {
			failJSON(w, 409, err)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	})
}
