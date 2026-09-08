package screens

// People is [Views.People]'s view: every row of the People declaration.
type People struct {
	Rows []PersonRow
}

// PersonRow is one human: the duties they hold, the obligation they hold
// outside the twelve, the credentials they lent, whether the row is the
// owner's, and whether the row acts anywhere at all.
type PersonRow struct {
	Key         string
	Name        string
	Duties      []int64
	Obligations []string
	Credentials []LentCredential
	// Owner is true for the one row that is the owner this install was made
	// as. The owner is the person the design gives no record, so the row
	// holds nothing on a fresh install and acts everywhere regardless: every
	// unheld row widens to them, and theirs is the one key an acting call
	// exempts before it reads the declaration. It is a field of the view
	// because the key is what a call carries and this is the one place a
	// human at a screen can read it.
	Owner bool
	// ActsAnywhere is false for a row added only so a human can read the
	// four screens without gating, approving, or otherwise acting anywhere.
	ActsAnywhere bool
}

// LentCredential is one credential a person or an organisation lent: its
// kind, the ceiling authored on it, and the rates a provider's units convert
// at.
type LentCredential struct {
	Name string
	Kind string // person or organisation
	// Ceiling is nil where none is authored.
	Ceiling *Ceiling
	Rates   []Rate
}

// Ceiling is a spend ceiling: an amount in a currency over a period a length
// and a start date define, in the zone the date was authored in.
type Ceiling struct {
	Amount    float64
	Currency  string
	Period    string // the length, e.g. "monthly"
	StartDate string // calendar date
	Zone      string // the IANA zone StartDate was authored in
}

// Rate is what a provider's units convert to per kind, model version, and
// effort.
type Rate struct {
	Kind         string
	ModelVersion string
	Effort       string
	Amount       float64
	Currency     string
}
