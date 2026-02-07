package main

import "strings"

const pgDuplicateError = "duplicate key value violates unique constraint"

func isPGDuplicateError(err error) bool {
	return strings.Contains(err.Error(), pgDuplicateError)
}
