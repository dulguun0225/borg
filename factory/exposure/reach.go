package exposure

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// OutboundPackages is what a Go build reaches the world through: the packages
// whose use is a call leaving the process. A new import of one of them, or a new
// call into one, is an outbound call added.
//
// It is a list and not a rule about paths because the list is what a human
// argues with. A package outside it is one this extractor does not read as
// outbound, and adding one is a line here.
var OutboundPackages = []string{
	"net/http", "net", "net/smtp", "os/exec", "database/sql", "google.golang.org/grpc",
}

// CredentialWords are what a name has to carry for a read of it to be a
// credential read. They are matched against the upper-cased name, so a lowercase
// spelling matches the same word.
var CredentialWords = []string{"KEY", "TOKEN", "SECRET", "PASSWORD"}

// AuthorizationWords are what a function's name has to carry for a removed call
// to it to be an authorization check removed. A check named outside them is one
// this extractor does not read, which is the cost of a list a human can argue
// with.
var AuthorizationWords = []string{"Authorize", "Authenticate", "Permit", "CheckAccess"}

// httpClientMethods are the methods of an http client, a call to one of which is
// an outbound call however the client was obtained. The receiver has to read as
// an http client — its name carrying "client" or "http" — because these are
// method names ordinary code uses for its own types, and an extractor that read
// every Get as an outbound call would put a human at every gate. What it costs
// is a client named neither, whose calls this does not read.
var httpClientMethods = []string{"Do", "Get", "Head", "Post", "PostForm"}

// secretName is the shape a secret's name takes in this factory: lowercase
// segments joined by dots, as "deploy.local" and "model.openrouter" are written.
// A string literal that is entirely one of those is a credential named, which is
// what a build hard-coding a reference looks like. The whole literal has to
// match, so a sentence carrying a dotted word is not one.
var secretName = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)+$`)

// literal is a Go string literal on one line, matched for its content alone.
var literal = regexp.MustCompile(`"([^"\\]*)"`)

// call is an identifier or a selector followed by an open bracket, which is what
// a call reads as on one line.
var call = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\(|\b([A-Z][A-Za-z0-9_]*)\(`)

// getenv is a read of one environment variable by a literal name.
var getenv = regexp.MustCompile(`Getenv\("([^"]*)"\)`)

// outboundCall is the entry an added line makes as an outbound call added, and
// false where it makes none.
func outboundCall(c change) (string, bool) {
	if c.Removed {
		return "", false
	}
	for _, pkg := range OutboundPackages {
		if strings.Contains(c.Text, `"`+pkg+`"`) {
			return entry(c, "a new import of "+pkg), true
		}
	}
	for _, match := range call.FindAllStringSubmatch(c.Text, -1) {
		receiver, method := match[1], match[2]
		if receiver == "" {
			continue
		}
		for _, pkg := range OutboundPackages {
			if receiver == path.Base(pkg) {
				return entry(c, fmt.Sprintf("a new call to %s.%s", receiver, method)), true
			}
		}
		lowered := strings.ToLower(receiver)
		if !strings.Contains(lowered, "client") && !strings.Contains(lowered, "http") {
			continue
		}
		for _, name := range httpClientMethods {
			if method == name {
				return entry(c, fmt.Sprintf("a new call to the http client method %s.%s", receiver, method)), true
			}
		}
	}
	return "", false
}

// credential is the entry an added line makes as a credential named or read.
func credential(c change) (string, bool) {
	if c.Removed {
		return "", false
	}
	if strings.Contains(c.Text, "secretref.Resolve(") {
		return entry(c, "a new call to secretref.Resolve"), true
	}
	for _, match := range getenv.FindAllStringSubmatch(c.Text, -1) {
		if word, carried := credentialWord(match[1]); carried {
			return entry(c, fmt.Sprintf("a new read of the environment variable %s, whose name carries %s", match[1], word)), true
		}
	}
	for _, match := range literal.FindAllStringSubmatch(c.Text, -1) {
		if secretName.MatchString(match[1]) {
			return entry(c, "a new string literal shaped like a secret's name: "+match[1]), true
		}
	}
	return "", false
}

// credentialWord is the word a name carries that makes reading it a credential
// read, and false for a name carrying none.
func credentialWord(name string) (string, bool) {
	upper := strings.ToUpper(name)
	for _, word := range CredentialWords {
		if strings.Contains(upper, word) {
			return word, true
		}
	}
	return "", false
}

// authorizationCheck is the entry a removed line makes as an authorization check
// removed or weakened: a removed call to a function whose name carries one of
// [AuthorizationWords], and a removed if guard around one, which is the same
// line read as the weakening rather than the removal.
func authorizationCheck(c change) (string, bool) {
	if !c.Removed {
		return "", false
	}
	guard := strings.HasPrefix(strings.TrimSpace(c.Text), "if ")
	for _, match := range call.FindAllStringSubmatch(c.Text, -1) {
		name := match[2]
		if name == "" {
			name = match[3]
		}
		for _, word := range AuthorizationWords {
			if !strings.Contains(name, word) {
				continue
			}
			if guard {
				return entry(c, "a removed if guard on "+name), true
			}
			return entry(c, "a removed call to "+name), true
		}
	}
	return "", false
}

// dependencyChange is the entry a changed line of go.mod or go.sum makes: the
// package and the version it names, with the licence the build's resolved set
// gives that package where the set names one. The key it answers beside the
// entry is the package and its version, which is what tells one change from
// another: go.sum names one package on two lines and go.mod on a third, so a
// package added is one dependency change and not three. A line naming no version is not a
// dependency change — the module line and the go directive are the file's own
// and not a package.
//
// An unpinned range that resolves differently with nothing in the manifest
// changed is read as the change it is, because go.sum is one of the two files
// read and a re-resolution changes it.
func dependencyChange(c change, resolved []Package) (key, found string, ok bool) {
	name := path.Base(c.File)
	if name != "go.mod" && name != "go.sum" {
		return "", "", false
	}
	pkg, version, named := packageVersion(c.Text)
	if !named {
		return "", "", false
	}
	licence := "a licence the build's resolved set does not name"
	for _, r := range resolved {
		if r.Package == pkg && r.Licence != "" {
			licence = "licence " + r.Licence
			break
		}
	}
	return pkg + " " + version, entry(c, fmt.Sprintf("%s %s, %s", pkg, version, licence)), true
}

// packageVersion is the package and the version one line of go.mod or go.sum
// names, and false for a line naming neither. A version is a field beginning
// with "v" and a digit, and the package is the field before it.
func packageVersion(text string) (pkg, version string, ok bool) {
	fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(text), "// indirect"))
	for i, field := range fields {
		if i == 0 || len(field) < 2 || field[0] != 'v' || field[1] < '0' || field[1] > '9' {
			continue
		}
		return fields[i-1], strings.TrimSuffix(field, "/go.mod"), true
	}
	return "", "", false
}

// entry is one line of the evidence list: where it is, and what was found there.
// The file and the line are first because the list is read beside the diff.
func entry(c change, what string) string {
	return fmt.Sprintf("%s:%d — %s", c.File, c.Line, what)
}
