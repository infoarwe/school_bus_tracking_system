// Package httpx holds the shared JSON response format for every API endpoint.
//
// Success:  {"data": ...}                     (lists add "meta": {"page","page_size","total"})
// Error:    {"error": {"code": "...", "message": "...", "fields": {...}}}
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type ErrorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type PageMeta struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, map[string]any{"data": data})
}

func List(w http.ResponseWriter, data any, meta PageMeta) {
	write(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func Error(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]any{"error": ErrorBody{Code: code, Message: message}})
}

func ValidationError(w http.ResponseWriter, fields map[string]string) {
	write(w, http.StatusBadRequest, map[string]any{"error": ErrorBody{
		Code: "validation_failed", Message: "One or more fields are invalid.", Fields: fields,
	}})
}

func write(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write json response", "err", err)
	}
}
