package domain

import "fmt"

// Error is a safe client-facing failure. Message must not include SQL or secrets.
type Error struct {
	Status int
	Code   string
	Msg    string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func E(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Msg: msg}
}

func As(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	d, ok := err.(*Error)
	if ok {
		return d, true
	}
	return nil, false
}

func Wrap(status int, code, msg string, err error) *Error {
	if err == nil {
		return E(status, code, msg)
	}
	return E(status, code, fmt.Sprintf("%s", msg))
}
