package service

import "net/http"

// APIError is the single error shape services return to HTTP handlers.
// Status is the HTTP status code, Code a stable machine-readable
// identifier, and Message a human-readable description.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func newAPIError(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

func badRequestError(code, message string) *APIError {
	return newAPIError(http.StatusBadRequest, code, message)
}

func internalError(code, message string) *APIError {
	return newAPIError(http.StatusInternalServerError, code, message)
}
