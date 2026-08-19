package libghostty

import "fmt"

type Result int

const (
	ResultSuccess       Result = 0
	ResultOutOfMemory   Result = -1
	ResultInvalidValue  Result = -2
	ResultOutOfSpace    Result = -3
	ResultNoValue       Result = -4
	ResultIOError       Result = -5
	ResultLimitExceeded Result = -6
)

type Error struct {
	Result Result
}

func (e *Error) Error() string {
	switch e.Result {
	case ResultOutOfMemory:
		return "ghostty: out of memory"
	case ResultInvalidValue:
		return "ghostty: invalid value"
	case ResultOutOfSpace:
		return "ghostty: out of space"
	case ResultNoValue:
		return "ghostty: no value"
	case ResultIOError:
		return "ghostty: I/O error"
	case ResultLimitExceeded:
		return "ghostty: limit exceeded"
	default:
		return fmt.Sprintf("ghostty: result=%d", int(e.Result))
	}
}

func resultError(result int32) error {
	if result == int32(ResultSuccess) {
		return nil
	}

	return &Error{Result: Result(result)}
}
