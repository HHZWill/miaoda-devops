package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

type profileRequest struct {
	Profile string `json:"profile"`
}

type packageRequest struct {
	Profile string `json:"profile"`
	AppID   string `json:"appId"`
}

type deployRequest struct {
	Profile      string `json:"profile"`
	ImportID     string `json:"importId"`
	Name         string `json:"name"`
	TargetURL    string `json:"targetUrl"`
	ConfirmedID  string `json:"confirmedId"`
	Confirmation string `json:"confirmation"`
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(dest); err != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("请求参数无效")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("写入 HTTP 响应失败：%v", err)
	}
}

func failJSON(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"message": err.Error()})
}
