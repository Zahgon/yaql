// Package repository lets `go build ./...` and `go test ./...` run from the repository root reach the yaql
// module, which lives in go/ and is a module of its own. The port itself is entirely under go/.
package repository

import (
	_ "yaql"
	_ "yaql/language"
	_ "yaql/stdlib"
)
