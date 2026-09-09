package deploy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/targetseam"
)

// What the deployer hands the service and what the record says about it: the
// resolved value set the build runs under, the token minted for the way in
// among its values, and the digest over the set that goes on the record.

// DigestConfiguration is the digest over a resolved value set: each name and
// each value in the order the caller assembled them, separated so that two sets
// differing only in where one value ends do not digest the same. It is what goes
// on the deploy record beside the build's digest, and what a rollback restores
// the configuration version by.
//
// It is taken over the set the caller resolved and nothing else: the way-in
// token is not among it, the token's own digest already having its own field
// on the record per [WayInTokenName]'s doc. Callers take this digest before
// [Performance.mintingTheWayInToken] appends the token to the set that is
// actually handed to the target, so a configuration that has not changed
// digests the same at every deploy of it — the token minted fresh each time
// never moves this digest.
func DigestConfiguration(values targetseam.ValueSet) string {
	if len(values.Names) == 0 {
		return ""
	}
	sum := sha256.New()
	for n, name := range values.Names {
		sum.Write([]byte(name))
		sum.Write([]byte{0})
		if n < len(values.Values) {
			sum.Write([]byte(values.Values[n]))
		}
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// WayInTokenName is the name the way-in token is handed to the service under.
// The deployer hands the token to the service in its configuration beside the
// service's own credentials, so the name is one of the value set's and the way
// in inside the service's build reads it by that name.
//
// It is package wayin's name spelled again rather than imported: this package
// resolves no configuration value and reads none of them, and what the shipped
// way in reads is that package's own text.
// TestTheWayInTokenIsHandedInTheConfiguration fails if the two spellings part.
const WayInTokenName = "BORG_WAY_IN"

// mintingTheWayInToken is the performance with the token the deployer mints for
// the way in at this deploy among its configuration values, and the digest of
// the token itself, which the record carries in its own field. The token is
// stored nowhere else: the digest is what the report store finds the deploy
// record by, through [ByWayInTokenDigest], and [Performance.WayInAddress] is
// where the way in presents the token.
//
// It goes into the value set rather than beside it, so the token is still
// handed to the service in its configuration — the seam carries no field for
// it — but the caller takes [DigestConfiguration] of the set before calling
// this, so the configuration digest the record carries is over the resolved
// set alone and never moves because a token minted fresh does.
func (p Performance) mintingTheWayInToken() (Performance, string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return p, "", fmt.Errorf("deploy: minting the way-in token: %w", err)
	}
	token := hex.EncodeToString(bytes[:])
	sum := sha256.Sum256([]byte(token))

	p.Configuration = targetseam.ValueSet{
		Names:  append(slices.Clone(p.Configuration.Names), WayInTokenName),
		Values: append(slices.Clone(p.Configuration.Values), token),
	}
	return p, hex.EncodeToString(sum[:]), nil
}

// addingTheDeployID is the configuration handed to the target with the deploy
// record's own identity appended, under [targetseam.DeployIDName], beside the
// way-in token: the instance is told its deploy at placement, so the record
// carries it and no reader joins it by time. It is appended once the record
// exists, after [DigestConfiguration] has already been taken and after the
// way-in token has already been minted and appended, so neither digest ever
// moves because a deploy id known only once the record is written is
// appended here.
func addingTheDeployID(configuration targetseam.ValueSet, deployID string) targetseam.ValueSet {
	return targetseam.ValueSet{
		Names:  append(slices.Clone(configuration.Names), targetseam.DeployIDName),
		Values: append(slices.Clone(configuration.Values), deployID),
	}
}
