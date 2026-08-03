package project

import "fmt"

type fieldError struct {
	path  string
	field string
	err   error
}

func (err fieldError) Error() string {
	if err.field == "" {
		return fmt.Sprintf("%s: %v", err.path, err.err)
	}
	return fmt.Sprintf("%s: %s: %v", err.path, err.field, err.err)
}

func (err fieldError) Unwrap() error {
	return err.err
}

func wrapField(path, field string, cause error) error {
	return fieldError{path: path, field: field, err: cause}
}
