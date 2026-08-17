package secretref

import (
	"errors"
	"fmt"
)

// ErrName is returned for a name that is empty or has a character outside
// A-Z, a-z, 0-9, and the three punctuation marks '.', '_', and '-'. The
// character set is narrow so that a name is unambiguous in the resolver's file
// format, where the first '=' on a line separates the name from the value.
var ErrName = errors.New("secretref: a name is one or more of A-Z a-z 0-9 . _ -")

// ErrUnset is returned by [Resolver.Resolve] for the zero [Ref], which names
// nothing.
var ErrUnset = errors.New("secretref: the reference names no secret")

// Ref is the name of a secret. It is what every record, spec, and artifact
// contains where a value would otherwise be, and it holds no value at any
// point in its life: the struct has one unexported string field, and every
// method that renders it renders that name.
//
// The zero Ref names nothing. [Ref.IsZero] reports that, and a caller that
// requires a secret checks it rather than letting the resolver refuse later.
type Ref struct {
	name string
}

// New returns the reference to the secret called name, or [ErrName].
func New(name string) (Ref, error) {
	if name == "" {
		return Ref{}, fmt.Errorf("%w: the name is empty", ErrName)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-':
		default:
			return Ref{}, fmt.Errorf("%w: character %d of %q is not one of them", ErrName, i+1, name)
		}
	}
	return Ref{name: name}, nil
}

// MustNew is [New] for a name written in the source, where a bad name is a
// defect rather than input. It panics on [ErrName].
func MustNew(name string) Ref {
	r, err := New(name)
	if err != nil {
		panic(err.Error())
	}
	return r
}

// Name is the secret's name.
func (r Ref) Name() string { return r.name }

// IsZero reports whether the reference names nothing.
func (r Ref) IsZero() bool { return r.name == "" }

// String is the name, because that is all a Ref has. The zero Ref renders as
// the four characters "none" rather than as nothing, so a log line that
// contains one says so.
func (r Ref) String() string {
	if r.name == "" {
		return "none"
	}
	return r.name
}
