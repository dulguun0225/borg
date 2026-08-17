package secretref

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrUnknown is returned by [Resolver.Resolve] for a name the file does not
// have.
var ErrUnknown = errors.New("secretref: no secret of that name")

// ErrFormat is returned by [Load] for a line the format does not allow. It
// names the file and the line number and no byte of the line itself, in any
// of the forms it takes. What makes that worth the worse message: the text
// before a malformed line's first '=' is not a name, whatever it looks like,
// and a credential pasted in whole splits on the '=' of its own base64
// padding — so the part that would be quoted back as "the name" is the
// credential, and it lands in a startup log or a bug report.
var ErrFormat = errors.New("secretref: the secrets file is malformed")

// Resolver answers a [Ref] with the secret's value. It is the one place a
// value is read, and it is held by the component about to use one.
//
// The file it reads is a file on disk today, which is what seam 3 allows. The
// format is one secret per line:
//
//	# A line that starts with a number sign is a comment.
//	model.anthropic=sk-example-value
//	deploy.staging=another value, spaces and all
//
// A line is the name, one '=', and the value. The value is every byte after
// that '=' to the end of the line, taken as it is: no quoting, no escaping, no
// trimming, so a value that ends in a space keeps it. What that costs is that
// a value cannot contain a newline and cannot contain a carriage return unless
// the value is meant to end with one — the file is separated by line feeds and
// nothing else. A name follows the rule [New] states. A blank line is skipped.
// A name that appears twice is [ErrFormat] rather than a last-one-wins rule
// nobody can see.
//
// The whole file is read once, by [Load], and the values stay in memory for as
// long as the Resolver does. Nothing here wipes a value, checks who can read
// the file, or separates one secret from another: seam 3 separates storage and
// not reach, and this is that.
type Resolver struct {
	path    string
	secrets map[string]string
}

// Load reads the file at path and returns a resolver over it. A later change
// to the file is not seen: a caller that wants the current contents calls Load
// again.
func Load(path string) (*Resolver, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secretref: reading %s: %w", path, err)
	}
	secrets := make(map[string]string)
	for n, line := range strings.Split(string(content), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("%w: %s line %d has no '='", ErrFormat, path, n+1)
		}
		if _, err := New(name); err != nil {
			// New's error renders the text it rejected, and that text is
			// whatever stood before the first '=' on a malformed line, which
			// is not a name — a pasted credential ending in base64 padding
			// splits on its own '=' and puts the credential here. So the
			// error is discarded and ErrName is wrapped in its place: the
			// rule, which is fixed text, and no byte of the line.
			return nil, fmt.Errorf("%w: %s line %d has nothing usable as a name before its first '=': %w", ErrFormat, path, n+1, ErrName)
		}
		if _, repeated := secrets[name]; repeated {
			return nil, fmt.Errorf("%w: %s line %d repeats the name %q", ErrFormat, path, n+1, name)
		}
		secrets[name] = value
	}
	return &Resolver{path: path, secrets: secrets}, nil
}

// Resolve is the secret the reference names. It returns [ErrUnset] for the
// zero [Ref] and [ErrUnknown] for a name the file does not have; neither error
// nor any other this package returns contains a value.
func (r *Resolver) Resolve(ref Ref) (string, error) {
	if ref.IsZero() {
		return "", ErrUnset
	}
	value, found := r.secrets[ref.Name()]
	if !found {
		return "", fmt.Errorf("%w: %q is not in %s", ErrUnknown, ref.Name(), r.path)
	}
	return value, nil
}
