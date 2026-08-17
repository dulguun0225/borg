package secretref

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const value = "sk-the-value-nothing-else-may-see"

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	return path
}

// TestRefHoldsOneStringAndRendersIt is what the type actually promises: one
// field, and every rendering of it is that field. It does not promise the
// string is a name — doc.go says why that is a convention and not a property.
func TestRefHoldsOneStringAndRendersIt(t *testing.T) {
	ref := MustNew("model.anthropic")
	if got := ref.String(); got != "model.anthropic" {
		t.Fatalf("String() = %q, want the name", got)
	}
	// The type has one field and it is the name, so there is nowhere for a
	// value to be. This is the check that says so, and it fails if a field is
	// ever added.
	var _ struct{ name string } = struct{ name string }(ref)
}

func TestNewRefusesABadName(t *testing.T) {
	for _, name := range []string{"", "has space", "has=equals", "has\nnewline", "has/slash"} {
		if _, err := New(name); !errors.Is(err, ErrName) {
			t.Errorf("New(%q) = %v, want ErrName", name, err)
		}
	}
	for _, name := range []string{"a", "model.anthropic", "deploy_staging", "a-b.c_1"} {
		if _, err := New(name); err != nil {
			t.Errorf("New(%q) = %v, want it accepted", name, err)
		}
	}
}

func TestZeroRefNamesNothing(t *testing.T) {
	var ref Ref
	if !ref.IsZero() {
		t.Fatal("the zero Ref does not report itself as zero")
	}
	if got := ref.String(); got != "none" {
		t.Fatalf("String() = %q, want %q", got, "none")
	}
	resolver, err := Load(writeFile(t, "a=b\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := resolver.Resolve(ref); !errors.Is(err, ErrUnset) {
		t.Fatalf("Resolve(zero) = %v, want ErrUnset", err)
	}
}

func TestResolveReadsTheFile(t *testing.T) {
	path := writeFile(t, "# a comment\n\nmodel.anthropic="+value+"\ndeploy.staging=two words \n")
	resolver, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := resolver.Resolve(MustNew("model.anthropic"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != value {
		t.Fatalf("Resolve = %q, want %q", got, value)
	}

	// The value is taken as it is, to the end of the line, trailing space
	// included.
	spaced, err := resolver.Resolve(MustNew("deploy.staging"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if spaced != "two words " {
		t.Fatalf("Resolve = %q, want the value with its trailing space", spaced)
	}

	if _, err := resolver.Resolve(MustNew("absent")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Resolve(absent) = %v, want ErrUnknown", err)
	}
}

func TestLoadRefusesAMalformedFile(t *testing.T) {
	cases := map[string]string{
		"no equals":  "a=b\nthis line has no equals\n",
		"bad name":   "a b=c\n",
		"empty name": "=c\n",
		"repeated":   "a=one\na=two\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeFile(t, content)); !errors.Is(err, ErrFormat) {
				t.Fatalf("Load = %v, want ErrFormat", err)
			}
		})
	}
}

// TestErrorsCarryNoValue is the reason the parser reports a line number rather
// than the line. Every malformed line below holds a credential, and each takes
// a different branch of the parser, because the branch that quotes back "the
// name" is the one that leaks: the text before a malformed line's first '=' is
// not a name.
func TestErrorsCarryNoValue(t *testing.T) {
	// A credential as a provider hands one over: base64, so it ends in '='
	// padding and its own padding is the first '=' on the line.
	const pasted = "AKIAIOSFODNN7EXAMPLE/wJalrXUtnFEMI+K7MDENGbPxRfiCYEXAMPLEKEY=="

	lines := map[string]string{
		"no equals at all":          "a line holding " + value,
		"an equals inside a value":  pasted,
		"a name with a space":       "deploy staging=" + value,
		"no name before the equals": "=" + value,
	}
	for name, line := range lines {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeFile(t, "good="+value+"\n"+line+"\n"))
			if !errors.Is(err, ErrFormat) {
				t.Fatalf("Load = %v, want ErrFormat", err)
			}
			for what, secret := range map[string]string{"the value": value, "the pasted credential": pasted} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the parse error contains %s: %v", what, err)
				}
			}
			// A prefix long enough to identify the credential is a leak too,
			// which is what quoting the rejected text back would produce.
			if strings.Contains(err.Error(), pasted[:12]) {
				t.Errorf("the parse error contains a prefix of the credential: %v", err)
			}
		})
	}

	resolver, err := Load(writeFile(t, "good="+value+"\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = resolver.Resolve(MustNew("absent"))
	if strings.Contains(err.Error(), value) {
		t.Fatalf("the resolve error contains a value: %v", err)
	}
}
