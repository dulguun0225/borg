package artifact

import (
	"errors"

	"github.com/dulguun0225/borg/factory/record"
)

// Kind is what an artifact is a version of. The first three belong to an
// item, one per stage; the other three are the fleet's, and belong to a role,
// a subject, or the factory as a whole rather than to an item — [ItemKinds]
// and [FleetKinds] split them, and the DDL's chain_key_matches_kind CHECK is
// the same split enforced on every write.
type Kind string

const (
	// KindSpec is a spec version; its content is the spec text.
	KindSpec Kind = "spec"
	// KindImplementationPlan is an implementation plan version; its content
	// is how the item will be built, as the planner wrote it.
	KindImplementationPlan Kind = "implementation_plan"
	// KindTasks is a tasks version; its content is the approved plan divided
	// into the work an agent picks up, one task per line.
	KindTasks Kind = "tasks"
	// KindImplementation is an implementation version; its content is the
	// commit hash the stage produced.
	KindImplementation Kind = "implementation"
	// KindConsumerContract is a consumer contract version, authored at the
	// implementation stage from the same build: its content is the words a human
	// reads the consumer contract by, and what it introduces is the predicates. A
	// kind of its own rather than a field of the implementation version, because
	// the two are derived from that build separately and either can be authored
	// again while the other stands.
	KindConsumerContract Kind = "consumer_contract"
	// KindRolePrompt is what an agent in a role works from. It belongs to the
	// role and not to an item, and its chain is named by role rather than by
	// item_id.
	KindRolePrompt Kind = "role_prompt"
	// KindSkill is a procedure an agent is given where the item it was
	// dispatched onto matches the skill's subject. It belongs to the subject
	// — an area, a service, or a project — and not to an item.
	KindSkill Kind = "skill"
	// KindSelectionRule is what context assembly selects by. There is one,
	// for the whole factory, and it belongs to none of item, role or
	// subject.
	KindSelectionRule Kind = "selection_rule"
)

// Kinds is every kind an artifact may be a version of. The CHECK constraint in
// [DDL] lists the same eight, and TestDDLListsEveryKind fails if the two lists
// stop agreeing.
var Kinds = []Kind{
	KindSpec, KindImplementationPlan, KindTasks, KindImplementation, KindConsumerContract,
	KindRolePrompt, KindSkill, KindSelectionRule,
}

// ItemKinds is every kind that belongs to an item: the four authored at a
// stage of the path, and the consumer contract derived at the last of them.
var ItemKinds = []Kind{
	KindSpec, KindImplementationPlan, KindTasks, KindImplementation, KindConsumerContract,
}

// FleetKinds is every kind that belongs to the fleet rather than to an item:
// a role prompt, a skill, or the one selection rule.
var FleetKinds = []Kind{KindRolePrompt, KindSkill, KindSelectionRule}

// Authorship is which of the store's three callers authored the version. It
// is an attribute of the version and not of the item, because authorship is
// per stage.
type Authorship string

const (
	// AuthorshipAgent is the agent in the stage's role.
	AuthorshipAgent Authorship = "agent"
	// AuthorshipHuman is a human backstopping the stage.
	AuthorshipHuman Authorship = "human"
	// AuthorshipGate is the gate component, where a human takes Edit in
	// place.
	AuthorshipGate Authorship = "gate"
)

// Authorships is every authorship a version an agent, a human, or the gate
// wrote may have. The CHECK constraint in [DDL] admits these three plus the
// empty string, which [By.Empty] names and [Authorships] deliberately
// excludes: it is not a fourth authorship, it is the absence of one.
var Authorships = []Authorship{AuthorshipAgent, AuthorshipHuman, AuthorshipGate}

// EnteredBy is which event entered a version nobody wrote. There are two, and
// they are not one: the install's entries enter in force ungated, a factory
// with nothing decided in it having to run, and an upgrade's first start
// enters a version awaiting the gate every version fires.
type EnteredBy string

const (
	// EnteredByInstall is the install's entry of the words that shipped with
	// the product. [InForce] reads such a version as in force with no
	// approval naming it.
	EnteredByInstall EnteredBy = "install"
	// EnteredByUpgradeFirstStart is the factory's first start on a new
	// version, entering shipped words that changed. It is in force only once
	// the gate every version fires has approved it, so where it extends a
	// chain the words the install ran on stand until then, and where it
	// starts one nothing is in force until then.
	EnteredByUpgradeFirstStart EnteredBy = "upgrade_first_start"
)

// EnteredBys is every event that enters a version nobody wrote. The CHECK
// constraint in [DDL] lists the same two and the empty value every authored
// version carries.
var EnteredBys = []EnteredBy{EnteredByInstall, EnteredByUpgradeFirstStart}

var (
	// ErrAuthorshipUnknown is returned for an authorship that is none of
	// [Authorships]. The store refuses it again through the CHECK constraint,
	// so a row inserted around the methods is refused too.
	ErrAuthorshipUnknown = errors.New("artifact: authorship is neither agent, human, nor gate")
	// ErrItemIDEmpty is returned by the item-kind submissions for a version
	// naming no item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("artifact: the item id is empty")
	// ErrAuthorEmpty is returned for an authored version naming no author. A
	// version whose author is not on the record is one no per-author prior
	// can be computed from, which is what the field is for.
	ErrAuthorEmpty = errors.New("artifact: the author is empty")
	// ErrRoleEmpty is returned by [Store.SubmitFleet] and [Store.EnterShipped]
	// for a role prompt naming no role.
	ErrRoleEmpty = errors.New("artifact: the role is empty")
	// ErrSubjectEmpty is returned by [Store.SubmitFleet] and
	// [Store.EnterShipped] for a skill naming no subject.
	ErrSubjectEmpty = errors.New("artifact: the subject is empty")
	// ErrFleetKindUnknown is returned by [Store.SubmitFleet] and
	// [Store.EnterShipped] for a kind outside [FleetKinds].
	ErrFleetKindUnknown = errors.New("artifact: the kind is none of role_prompt, skill, or selection_rule")
	// ErrShippedBundleIdentityEmpty is returned by [Store.EnterShipped] for a
	// call naming no release of the product.
	ErrShippedBundleIdentityEmpty = errors.New("artifact: the shipped bundle identity is empty")
	// ErrEnteredByUnknown is returned by [Store.EnterShipped] for an event
	// outside [EnteredBys]. Which of the two entered a row is what tells the
	// ungated entry from the one awaiting its gate.
	ErrEnteredByUnknown = errors.New("artifact: the entry is neither an install's nor an upgrade's first start")
)

// Artifact is one version of an artifact as it is stored.
type Artifact struct {
	ID         string
	Actor      record.Actor
	At         string
	ItemID     string
	Role       string
	Subject    string
	Kind       Kind
	Version    int
	Supersedes string
	Authorship Authorship
	// Author is the identity a prior is kept on: the model version for a
	// version an agent wrote, the person's name for one a human wrote, and
	// empty on the one entry nobody wrote.
	Author  string
	Content string
	// ContentDigest is the sha256 of Content in hexadecimal, computed at the
	// write.
	ContentDigest string
	// ShippedBundleIdentity is the release of the product that entered this
	// version, present exactly on the entry the factory wrote itself and
	// empty on every authored one.
	ShippedBundleIdentity string
	// EnteredBy is which of the two events entered a version nobody wrote,
	// and is empty on every authored one.
	EnteredBy EnteredBy
	// InputManifestID names the input manifest the version was authored
	// from, supplied by the caller that dispatched the run. It is empty where
	// that caller wrote no manifest, and on every shipped version, an entry
	// authoring nothing having read no manifest.
	InputManifestID string
}

// By is who authored a version: which of the store's three callers it came
// through, and the identity a per-author prior is kept on. The two are
// separate facts and both are stored — the authorship says whether an agent, a
// human at the stage, or a human at a gate wrote it, and the author says which
// one, so a prior can be computed from that author's own work.
//
// It is a struct and not two arguments so that a caller cannot pass the
// authorship where the author belongs, both being strings.
type By struct {
	Authorship Authorship
	// Author is the model version for a version an agent wrote, and the
	// person's name for one a human wrote. It is kept per model version and
	// not per family: a new version accumulates a prior of its own, and keeping
	// the old name for a new version would read the old version's outcomes as
	// the new one's.
	Author string
}

// Empty reports whether by names neither an authorship nor an author — the
// one pair [Store.EnterShipped] writes and every other submission refuses as
// a partial one instead.
func (b By) Empty() bool { return b.Authorship == "" && b.Author == "" }
