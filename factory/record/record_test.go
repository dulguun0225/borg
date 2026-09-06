package record

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestActorValidate(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  error
	}{
		{"human claimed", Actor{Kind: KindHuman, Key: "p_abc123", Basis: BasisClaimed}, nil},
		{"human verified", Actor{Kind: KindHuman, Key: "p_abc123", Basis: BasisVerified}, nil},
		{"component claimed", Actor{Kind: KindComponent, Key: "gate.merge", Basis: BasisClaimed}, nil},
		{"agent claimed", Actor{Kind: KindAgent, Key: "anthropic/claude-opus-4.8", Basis: BasisClaimed}, nil},
		{"agent verified", Actor{Kind: KindAgent, Key: "anthropic/claude-opus-4.8", Basis: BasisVerified}, nil},
		{"empty actor", Actor{}, ErrKindUnknown},
		{"empty kind", Actor{Key: "owner"}, ErrKindUnknown},
		{"unknown kind", Actor{Kind: "robot", Key: "owner"}, ErrKindUnknown},
		{"empty key human", Actor{Kind: KindHuman, Basis: BasisClaimed}, ErrKeyEmpty},
		{"empty key component", Actor{Kind: KindComponent, Basis: BasisClaimed}, ErrKeyEmpty},
		{"empty key agent", Actor{Kind: KindAgent, Basis: BasisClaimed}, ErrKeyEmpty},
		{"human no basis", Actor{Kind: KindHuman, Key: "p_abc123"}, ErrBasisEmpty},
		{"component no basis", Actor{Kind: KindComponent, Key: "gate.merge"}, ErrBasisEmpty},
		{"agent no basis", Actor{Kind: KindAgent, Key: "anthropic/claude-opus-4.8"}, ErrBasisEmpty},
		{"human unknown basis", Actor{Kind: KindHuman, Key: "p_abc123", Basis: "guessed"}, ErrBasisUnknown},
		{"component unknown basis", Actor{Kind: KindComponent, Key: "gate.merge", Basis: "guessed"}, ErrBasisUnknown},
		{"agent unknown basis", Actor{Kind: KindAgent, Key: "anthropic/claude-opus-4.8", Basis: "guessed"}, ErrBasisUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.actor.Validate()
			if c.want == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("Validate() = %v, want %v", err, c.want)
			}
		})
	}
}

// TestConstraintsListEveryKind is what keeps the CHECK constraint and [Kinds]
// from disagreeing. The constraint is written as SQL text rather than built
// from the slice, so this is the check that they still name the same three.
func TestConstraintsListEveryKind(t *testing.T) {
	const open = "actor_kind in ("
	i := strings.Index(Constraints, open)
	if i < 0 {
		t.Fatalf("Constraints has no %q list", open)
	}
	rest := Constraints[i+len(open):]
	j := strings.Index(rest, ")")
	if j < 0 {
		t.Fatalf("the %q list is not closed", open)
	}
	listed := strings.Split(rest[:j], ",")
	if len(listed) != len(Kinds) {
		t.Fatalf("the constraint lists %d kinds, Kinds has %d", len(listed), len(Kinds))
	}
	for n, k := range Kinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where Kinds has %s", got, want)
		}
	}
}

// TestConstraintsListEveryBasis is what keeps the CHECK constraint and
// [Bases] from disagreeing, the way TestConstraintsListEveryKind does for
// [Kinds].
func TestConstraintsListEveryBasis(t *testing.T) {
	const open = "actor_key_basis in ("
	i := strings.Index(Constraints, open)
	if i < 0 {
		t.Fatalf("Constraints has no %q list", open)
	}
	rest := Constraints[i+len(open):]
	j := strings.Index(rest, ")")
	if j < 0 {
		t.Fatalf("the %q list is not closed", open)
	}
	listed := strings.Split(rest[:j], ",")
	if len(listed) != len(Bases) {
		t.Fatalf("the constraint lists %d bases, Bases has %d", len(listed), len(Bases))
	}
	for n, b := range Bases {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(b)+"'"; got != want {
			t.Errorf("the constraint lists %s where Bases has %s", got, want)
		}
	}
}

// TestConstraintsMatchTheTimeLayout is what holds the two spellings of the
// timestamp format together: [TimeLayout], which the writer formats with, and
// [TimePattern], which the store checks against. They are different notations
// for one format and nothing but this test says so.
func TestConstraintsMatchTheTimeLayout(t *testing.T) {
	if !strings.Contains(Constraints, TimePattern) {
		t.Fatalf("Constraints does not check against TimePattern")
	}
	pattern, err := regexp.Compile(TimePattern)
	if err != nil {
		t.Fatalf("TimePattern does not compile: %v", err)
	}

	for _, at := range []time.Time{
		time.Date(2026, 8, 17, 1, 30, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		time.Now(),
	} {
		if got := FormatTime(at); !pattern.MatchString(got) {
			t.Errorf("FormatTime(%v) = %q, which TimePattern does not match", at, got)
		}
	}

	// The forms a writer that did not use FormatTime would produce, and the
	// two ways an anchor could be too loose to catch them.
	for _, at := range []string{
		"",
		"2026-08-17T01:30:00Z",
		"2026-08-17T01:30:00.000Z",
		"2026-08-17T01:30:00.000000000+00:00",
		"2026-08-17 01:30:00.000000000Z",
		"2026-08-17T01:30:00.000000000Z ",
		"2026-08-17T01:30:00.000000000Z\n",
		"x\n2026-08-17T01:30:00.000000000Z",
	} {
		if pattern.MatchString(at) {
			t.Errorf("TimePattern matches %q, which FormatTime never produces", at)
		}
	}
}

func TestNewIDIsPrefixedAndUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		id := NewID("dl")
		if !strings.HasPrefix(id, "dl_") {
			t.Fatalf("NewID(%q) = %q, want the prefix and an underscore", "dl", id)
		}
		if len(id) != len("dl_")+2*idBytes {
			t.Fatalf("NewID(%q) = %q, want %d characters", "dl", id, len("dl_")+2*idBytes)
		}
		if seen[id] {
			t.Fatalf("NewID returned %q twice", id)
		}
		seen[id] = true
	}
}

func TestFormatTimeIsFixedWidthUTC(t *testing.T) {
	at := time.Date(2026, 8, 17, 9, 30, 0, 0, time.FixedZone("+08", 8*3600))
	got := FormatTime(at)
	if want := "2026-08-17T01:30:00.000000000Z"; got != want {
		t.Fatalf("FormatTime = %q, want %q", got, want)
	}
	back, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("the layout does not produce RFC 3339: %v", err)
	}
	if !back.Equal(at) {
		t.Fatalf("parsed back %v, want %v", back, at)
	}
	if n := len(FormatTime(time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC))); n != len(got) {
		t.Fatalf("width varies with the fraction: %d against %d", n, len(got))
	}
}
