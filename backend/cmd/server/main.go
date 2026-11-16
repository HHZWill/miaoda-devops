package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"app-deployment-platform/internal/deployment/application"
	httpapi "app-deployment-platform/internal/deployment/interfaces/http"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	projectDir, err := filepath.Abs("..")
	if err != nil {
		log.Fatal(err)
	}
	logDir := filepath.Join(projectDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		log.Fatalf("创建日志目录失败：%v", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatalf("打开服务日志失败：%v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(logFile, os.Stderr))

	port := os.Getenv("PORT")
	if port == "" {
		port = "18080"
	}
	service, err := application.NewService(filepath.Join(projectDir, "data"))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.RegisterRoutes(mux, service)
	mux.Handle("GET /", http.FileServer(http.Dir(filepath.Join(projectDir, "frontend", "dist"))))

	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", port),
		Handler:           httpapi.LocalOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("[service] started address=http://%s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
