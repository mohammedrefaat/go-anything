package main

import (
	"errors"
	"fmt"
)

// Standard CLI Exit Codes:
//
//	0: Success / OK
//	1: Failed check / gate failure / diff mismatch
//	2: Usage error (invalid flags, missing arguments)
//	3: Internal error
const (
	ExitCodeOK       = 0
	ExitCodeFail     = 1
	ExitCodeUsage    = 2
	ExitCodeInternal = 3
)

// CodedError represents an error carrying a CLI process exit code.
type CodedError interface {
	error
	ExitCode() int
}

// CLIError wraps an error with a specific exit code.
type CLIError struct {
	Code int
	Err  error
}

func (e *CLIError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *CLIError) Unwrap() error {
	return e.Err
}

func (e *CLIError) ExitCode() int {
	return e.Code
}

// exitErrorf creates a new CLIError with formatted message.
func exitErrorf(code int, format string, args ...any) error {
	return &CLIError{
		Code: code,
		Err:  fmt.Errorf(format, args...),
	}
}

// exitWrap wraps an existing error with a specific exit code.
func exitWrap(code int, err error) error {
	if err == nil {
		return nil
	}
	return &CLIError{
		Code: code,
		Err:  err,
	}
}

// GetExitCode extracts the exit code from an error, returning ExitCodeOK for nil
// and defaulting to ExitCodeFail for unclassified errors.
func GetExitCode(err error) int {
	if err == nil {
		return ExitCodeOK
	}
	var coded CodedError
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return ExitCodeFail
}
