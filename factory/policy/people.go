package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// AppendPeopleVersion appends the policy version a write to the People
// declaration takes, with People as the caller. It is what package people
// calls before it writes the declaration: the version first and the
// declaration second, the order every owner write takes, so a stop between
// them leaves a version naming what nothing yet reads.
//
// declaration is the whole declaration as it will stand after the write, by
// per-person key and never by name, so the mapping stays outside the chain and
// an erasure still deletes it alone. This package neither reads nor writes the
// declaration: the direction between the two is People to here.
//
// Nothing calls it yet — package people writes no version — and what remains
// for that caller is the call itself.
func (f *Factory) AppendPeopleVersion(ctx context.Context, actor record.Actor,
	declaration DeclarationSnapshot) (Version, error) {
	body, err := json.Marshal(declaration)
	if err != nil {
		return Version{}, fmt.Errorf("policy: keying the People declaration: %w", err)
	}
	digest := sha256.Sum256(body)
	return f.append(ctx, write{
		caller: CallerPeople, actor: actor, action: ActionDeclarationWritten,
		scope: Scope{Kind: "people", ID: "declaration"}, declaration: &declaration,
		keyExtra: hex.EncodeToString(digest[:]),
	})
}
