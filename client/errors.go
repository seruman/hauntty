package client

import "fmt"

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.Code)
}

func (e *ExitError) ExitCode() int {
	return e.Code
}

// ServerError is returned when the daemon responds with an Error message.
type ServerError struct {
	Op      string
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}
