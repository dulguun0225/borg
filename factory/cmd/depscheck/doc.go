// Command depscheck fails the build on an import between two packages of this
// module that factory/deps.txt does not allow.
//
// It reads the allowed graph from deps.txt in the working directory and the
// actual graph from "go list -deps=false -json ./...", which reports each
// package's Imports, TestImports, and XTestImports. Only imports of this
// module are checked: the standard library and pgx are ignored. Run it from
// factory/.
//
// Three things are an error: an import a package's line does not allow, a
// package the file does not list, and a line naming a package that does not
// exist. The last one is what keeps the file from describing a tree that has
// moved on. An allowed edge that nothing uses is not an error, because a line
// stating what a package may import is a bound and not a requirement.
//
// A test import is allowed by the importing package's own line or by a line
// beginning with "test". The two are separate so that reading deps.txt says
// which edges the shipped code has and which exist only to test it.
//
// Who may write what: this command writes nothing. It opens no database, it
// reads deps.txt and never edits it — a wrong line is fixed by a person, not
// by the tool that found it — and it reports what it found on standard error
// and in its exit status. It runs "go list", which writes to the build cache
// and to nothing of the factory's.
//
// What defines it: "Machine-checked dependency direction" under Code in
// ../../../CLAUDE.md#code.
package main
