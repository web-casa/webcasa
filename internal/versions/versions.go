// Package versions centralises pinned dependency versions used across the project.
//
// The single source of truth is the VERSIONS file in the repository root.
// Both install.sh (bash) and this Go package read from it.
//
// After editing VERSIONS, run:
//
//	go generate ./internal/versions/
//
//go:generate go run gen.go
package versions
