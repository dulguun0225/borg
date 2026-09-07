package screens

// DeclareDutyArgs holds one of the owner's twelve duties on a per-person
// key.
type DeclareDutyArgs struct {
	HumanKey string
	Duty     int64
}

// WithdrawDutyArgs withdraws a duty from a per-person key.
type WithdrawDutyArgs struct {
	HumanKey string
	Duty     int64
}

// DeclareObligationArgs names an obligation outside the twelve: hosting,
// installing the drift detector, or composing the fleet.
type DeclareObligationArgs struct {
	HumanKey   string
	Obligation string
}

// WithdrawObligationArgs withdraws an obligation from a per-person key.
type WithdrawObligationArgs struct {
	HumanKey   string
	Obligation string
}

// LendCredentialArgs lends a credential to a per-person key: whether the
// account is a person's own or an organisation's.
type LendCredentialArgs struct {
	HumanKey   string
	Credential string
	Kind       string // person or organisation
}

// TakeBackCredentialArgs takes a lent credential back; nothing here lifts
// itself until an owner attaches another.
type TakeBackCredentialArgs struct {
	HumanKey   string
	Credential string
}

// AuthorCeilingArgs authors a spend ceiling on a lent credential: an amount
// in a currency over a period a length and a start date define, in the zone
// the date was authored in.
type AuthorCeilingArgs struct {
	Credential string
	Amount     float64
	Currency   string
	PeriodUnit string // e.g. "monthly"
	Length     int64
	StartDate  string
	Zone       string
}

// AuthorRateArgs authors a rate a provider's units convert at, per kind,
// model version, and effort.
type AuthorRateArgs struct {
	Credential   string
	Kind         string
	ModelVersion string
	Effort       string
	Amount       float64
	Currency     string
}

// WriteMappingArgs writes a per-person key's name, kept outside the chain so
// an erasure can delete it alone.
type WriteMappingArgs struct {
	HumanKey string
	Name     string
}

// DeleteMappingArgs erases a per-person key's name; the key stands on every
// record it was written to.
type DeleteMappingArgs struct {
	HumanKey string
}
