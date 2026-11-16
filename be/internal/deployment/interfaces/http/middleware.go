package httpapi

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var requestSequence atomic.Uint64

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(value []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(value)
	r.bytes += written
	return written, err
}

func LocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := requestSequence.Add(1)
		recorder := &responseRecorder{ResponseWriter: w}
		recorder.Header().Set("X-Request-ID", fmtRequestID(requestID))
		defer func() {
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			// 成功的前端任务轮询不写访问日志。
			if !shouldLogRequest(r.Method, r.URL.Path, status) {
				return
			}
			log.Printf("[http] request_id=%s method=%s path=%q status=%d bytes=%d duration_ms=%d",
				fmtRequestID(requestID), r.Method, r.URL.Path, status, recorder.bytes, time.Since(started).Milliseconds())
		}()

		host := r.Host
		if name, _, err := net.SplitHostPort(host); err == nil {
			host = name
		}
		if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
			http.Error(recorder, "仅支持从本机访问", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			recorder.Header().Set("X-Content-Type-Options", "nosniff")
			recorder.Header().Set("Cache-Control", "no-store")
			if r.Method == http.MethodPost && r.Header.Get("X-Platform-Request") != "1" {
				failJSON(recorder, 403, errors.New("请从平台页面操作"))
				return
			}
		}
		next.ServeHTTP(recorder, r)
	})
}

func shouldLogRequest(method, path string, status int) bool {
	// 异常轮询仍需保留以便排查。
	return method != http.MethodGet || (path != "/api/jobs" && path != "/api/environment/install") || status >= http.StatusBadRequest
}

func fmtRequestID(sequence uint64) string {
	return fmt.Sprintf("req-%06d", sequence)
}
