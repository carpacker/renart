package service

type ServiceAPIError struct {
	Status  int
	Code    string
	Message string
}

func newServiceAPIError(status int, code, message string) *ServiceAPIError {
	return &ServiceAPIError{Status: status, Code: code, Message: message}
}
