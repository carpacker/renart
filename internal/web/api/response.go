// Package api provides HTTP API utilities for the Bruin web server.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Response represents a standardized API response envelope.
type Response struct {
	Status string `json:"status"` // "ok" or "error"
	Data   any    `json:"data,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error represents a structured error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteSuccess writes a successful JSON response.
func WriteSuccess(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, Response{
		Status: "ok",
		Data:   data,
	})
}

// WriteError writes a standardized error response.
// If message is empty, it defaults to the HTTP status text.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}

	WriteJSON(w, status, Response{
		Status: "error",
		Error: &Error{
			Code:    code,
			Message: message,
		},
	})
}

// WriteErrorWithDetails writes an error response with additional details.
func WriteErrorWithDetails(w http.ResponseWriter, status int, code, message string, details any) {
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(status)
	}

	WriteJSON(w, status, Response{
		Status: "error",
		Error: &Error{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

// WriteBadRequest is a convenience function for 400 Bad Request errors.
func WriteBadRequest(w http.ResponseWriter, code, message string) {
	WriteError(w, http.StatusBadRequest, code, message)
}

// WriteNotFound is a convenience function for 404 Not Found errors.
func WriteNotFound(w http.ResponseWriter, code, message string) {
	WriteError(w, http.StatusNotFound, code, message)
}

// WriteConflict is a convenience function for 409 Conflict errors.
func WriteConflict(w http.ResponseWriter, code, message string) {
	WriteError(w, http.StatusConflict, code, message)
}

// WriteInternalError is a convenience function for 500 Internal Server errors.
func WriteInternalError(w http.ResponseWriter, code, message string) {
	WriteError(w, http.StatusInternalServerError, code, message)
}
