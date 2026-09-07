// The client is not part of the Go module beside it. A go.mod here makes this
// directory a module of its own, which is what keeps `go build ./...`,
// `go test ./...` and cmd/depscheck from reading node_modules, where an npm
// package may ship Go source. Nothing is built from this file.
module github.com/dulguun0225/borg/factory/client

go 1.26.5
