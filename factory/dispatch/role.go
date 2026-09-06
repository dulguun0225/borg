package dispatch

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dulguun0225/borg/factory/item"
)

// Role is what an agent is put on: one stage of the path and the artifact that
// stage writes about the item, or one of the two roles put on an intent, which
// run before there is an item to match against. The set is closed — an owner
// composing a fleet entry chooses among these and never invents one — and it is
// closed at six here: the four authoring stages and the two on an intent. The
// grouper, which runs before an intent exists, and the role that argues a fleet
// proposal are not built.
type Role string

const (
	// RoleSpecAuthor authors the spec version and the criteria it introduces.
	RoleSpecAuthor Role = "spec_author"
	// RoleImplementationPlanner authors how the item will be built.
	RoleImplementationPlanner Role = "implementation_planner"
	// RoleTaskAuthor divides the approved plan into the work an agent picks
	// up.
	RoleTaskAuthor Role = "task_author"
	// RoleImplementer authors the code, the encodings, and the emission.
	RoleImplementer Role = "implementer"
	// RoleInterviewer asks the interview's rounds and states the reading the
	// requester confirms. It is put on an intent, the interview happening
	// before there is an item to match against.
	RoleInterviewer Role = "interviewer"
	// RoleDecomposer cuts one intent into the items that answer it. It is put
	// on an intent for the reason [RoleInterviewer] is.
	RoleDecomposer Role = "decomposer"
)

// Roles is every role, in the order the path reaches them: the two put on an
// intent, and then the four whose stages an item passes through.
var Roles = []Role{RoleInterviewer, RoleDecomposer,
	RoleSpecAuthor, RoleImplementationPlanner, RoleTaskAuthor, RoleImplementer}

// ErrRoleUnknown is returned for a role outside [Roles].
var ErrRoleUnknown = fmt.Errorf("dispatch: not a role")

// ErrRoleNamesNoStage is returned by [Role.Stage] for a role put on an intent.
// Neither names a stage: an intent has none until decomposition writes the
// items, which is why the two are matched on the intent's project alone.
var ErrRoleNamesNoStage = fmt.Errorf("dispatch: this role is put on an intent and names no stage")

// Stage is the stage the role names. A dispatch on an item is the match of the
// item's stage against this, and a role put on an intent is
// [ErrRoleNamesNoStage].
func (r Role) Stage() (item.Stage, error) {
	switch r {
	case RoleSpecAuthor:
		return item.StageSpec, nil
	case RoleImplementationPlanner:
		return item.StageImplementationPlan, nil
	case RoleTaskAuthor:
		return item.StageTasks, nil
	case RoleImplementer:
		return item.StageImplementation, nil
	case RoleInterviewer, RoleDecomposer:
		return "", fmt.Errorf("%w: %q", ErrRoleNamesNoStage, r)
	default:
		return "", fmt.Errorf("%w: %q", ErrRoleUnknown, r)
	}
}

// OnAnIntent reports whether the role is one of the two put on an intent. It is
// what a dispatch reads before it looks for a stage, and what a fleet answers
// an entry for with no item to match on.
func (r Role) OnAnIntent() bool { return r == RoleInterviewer || r == RoleDecomposer }

// RoleAt is the role whose stage is this one, and false for a stage no role is
// put on — queued and merged, where nothing authors, the three values that end
// an item, and the two roles put on an intent, which name no stage at all.
func RoleAt(stage item.Stage) (Role, bool) {
	for _, role := range Roles {
		at, err := role.Stage()
		if err == nil && at == stage {
			return role, true
		}
	}
	return "", false
}

// The operations an agent may perform, which is what a role carries beside its
// stage. The factory defines the list per role and an owner never invents one;
// an owner may narrow the list on a fleet entry and never widen it.
//
// Nothing enforces the list. It is declared here because the seam that would
// enforce it — seam 5 of ../../end-goal/deferred.md — is not built: there is no
// sandbox, no egress rule, and no credential issued per candidate, so an agent
// performing an operation off its list is stopped by nothing.
const (
	// OperationReadTheRepository is reading the checkout the stage was handed.
	OperationReadTheRepository = "read the repository"
	// OperationWriteTheRepository is writing files into the candidate's own
	// branch, which only the implementation stage does.
	OperationWriteTheRepository = "write the repository"
	// OperationSubmitAVersion is calling the artifact store with what the role
	// authored.
	OperationSubmitAVersion = "submit an artifact version"
	// OperationRunTheBuild is running the build's own tooling, which only the
	// implementation stage does.
	OperationRunTheBuild = "run the build"
)

// Operations is what a role may do. A role's list is the factory's, so this is
// a function of the role and of nothing an owner writes.
//
// The two roles put on an intent read and never write: neither authors an
// artifact version — the interview's questions and the items decomposition
// writes are records with writers of their own — so neither carries
// [OperationSubmitAVersion].
func (r Role) Operations() ([]string, error) {
	switch r {
	case RoleInterviewer, RoleDecomposer:
		return []string{OperationReadTheRepository}, nil
	case RoleSpecAuthor, RoleImplementationPlanner, RoleTaskAuthor:
		return []string{OperationReadTheRepository, OperationSubmitAVersion}, nil
	case RoleImplementer:
		return []string{
			OperationReadTheRepository, OperationWriteTheRepository,
			OperationSubmitAVersion, OperationRunTheBuild,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrRoleUnknown, r)
	}
}

// Narrow is a fleet entry's own list checked against the role's: an owner may
// leave an operation out and may never add one. It returns the list the entry
// runs under, which is the role's where the entry names none.
func (r Role) Narrow(entry []string) ([]string, error) {
	full, err := r.Operations()
	if err != nil {
		return nil, err
	}
	if len(entry) == 0 {
		return full, nil
	}
	for _, one := range entry {
		if !slices.Contains(full, one) {
			return nil, fmt.Errorf("%w: %s may not %q, and an entry narrows a role's list and never widens it",
				ErrOperationWidened, r, one)
		}
	}
	return entry, nil
}

// ErrOperationWidened is returned by [Role.Narrow] for an entry naming an
// operation the role does not carry.
var ErrOperationWidened = fmt.Errorf("dispatch: a fleet entry widened a role's operations")

// Scope is where an entry may be put: the same lines a safeguard is drawn on —
// a project, or an area inside one — plus the service, which is what an item
// names beside its area. An empty field matches anything, so the empty scope is
// the whole factory.
type Scope struct {
	ProjectID string
	ServiceID string
	AreaID    string
}

// Covers reports whether the scope admits an item with these subjects. A field
// the scope names has to be the item's; a field it leaves empty matches
// whatever the item has, the empty value included.
//
// Both halves of a scope are honoured, the item's area chain and its service's
// project: a scope drawn on any area in the chain reaches the item, so
// declaring a finer area never takes the item out of an entry drawn on a
// coarser one. [On.Areas] is what the area is matched against.
//
// What a scope costs is that it binds nothing yet: this honours it and no
// mechanism stops an agent reaching past it. Every call the agent makes carries
// it, in the principal seam 5 puts on a call, so what stops it later has the
// call to read.
func (s Scope) Covers(on On) bool {
	if s.ProjectID != "" && s.ProjectID != on.ProjectID {
		return false
	}
	if s.ServiceID != "" && s.ServiceID != on.ServiceID {
		return false
	}
	if s.AreaID != "" && !slices.Contains(on.Areas(), s.AreaID) {
		return false
	}
	return true
}

// Areas is what a scope's area is matched against and what a hold row names:
// the item's area chain where the caller supplied one, and the item's own area
// alone where it did not. An item with no area at all is matched against
// nothing, which only the empty scope covers.
func (o On) Areas() []string {
	if len(o.AreaChain) > 0 {
		return o.AreaChain
	}
	if o.AreaID == "" {
		return nil
	}
	return []string{o.AreaID}
}

// String is the scope as the principal carries it, so a call made under it
// says where the agent was put. The empty scope reads as the whole factory.
func (s Scope) String() string {
	named := make([]string, 0, 3)
	for _, part := range []struct{ what, id string }{
		{"project", s.ProjectID}, {"service", s.ServiceID}, {"area", s.AreaID},
	} {
		if part.id != "" {
			named = append(named, part.what+" "+part.id)
		}
	}
	if len(named) == 0 {
		return "the whole factory"
	}
	return strings.Join(named, ", ")
}
