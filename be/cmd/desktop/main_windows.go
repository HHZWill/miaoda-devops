package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	if err := launch(); err != nil {
		showError(err.Error())
	}
}

func launch() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Dir(executable)
	port := os.Getenv("PORT")
	if port == "" {
		port = "18080"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("PORT 必须为 1 至 65535 的整数")
	}
	url := "http://127.0.0.1:" + port + "/"
	// 已启动的平台直接复用，其他端口占用只提示，不结束进程。
	if healthy(url) {
		return openBrowser(url)
	}
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second); err == nil {
		connection.Close()
		return fmt.Errorf("端口 %s 已被其他程序占用，请关闭该程序后重新启动", port)
	}
	if _, err := os.Stat(filepath.Join(root, "web", "index.html")); err != nil {
		return fmt.Errorf("请完整解压 Windows 启动包后再打开程序")
	}
	logDir := filepath.Join(root, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("无法创建日志目录：%w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "startup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	command := exec.Command(filepath.Join(root, "server.exe"))
	command.Dir, command.Stderr = root, logFile
	command.Env = append(os.Environ(), "PORT="+port, "MIAODA_PROJECT_DIR="+root,
		"MIAODA_WEB_DIR="+filepath.Join(root, "web"), "MIAODA_PACKAGES_DIR="+filepath.Join(root, "packages"),
		"MIAODA_DESKTOP_STARTUP=1")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("服务启动失败：%w", err)
	}
	ready, stopped := make(chan bool, 1), make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		// 就绪信号必须来自本次启动的服务。
		for scanner.Scan() {
			if scanner.Text() == "ready" {
				ready <- true
				return
			}
		}
	}()
	go func() { stopped <- command.Wait() }()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case <-ready:
		// HTTP 复检通过后打开初始化页面。
		if !healthy(url) {
			return fmt.Errorf("服务尚未就绪，请查看 logs/server.log")
		}
		return openBrowser(url)
	case err := <-stopped:
		return fmt.Errorf("工作台启动失败：%v，请查看 logs/startup.log", err)
	case <-timer.C:
		return fmt.Errorf("启动超时，请查看 logs/server.log")
	}
}

func healthy(url string) bool {
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get(url + "api/health")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	var health struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	return response.StatusCode == 200 && json.NewDecoder(response.Body).Decode(&health) == nil &&
		health.Status == "ok" && health.Service == "miaoda-deployment-platform"
}

func openBrowser(url string) error {
	command := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := command.Start(); err != nil {
		return fmt.Errorf("工作台已启动，请在浏览器打开 %s", url)
	}
	return nil
}

func showError(message string) {
	title, _ := syscall.UTF16PtrFromString("妙搭部署平台")
	text, _ := syscall.UTF16PtrFromString(message)
	messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	messageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
