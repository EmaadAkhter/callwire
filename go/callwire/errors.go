package callwire

import "fmt"

type WireError struct {
	ErrorType string
	Message   string
}

func (e *WireError) Error() string {
	return fmt.Sprintf("%s: %s", e.ErrorType, e.Message)
}
