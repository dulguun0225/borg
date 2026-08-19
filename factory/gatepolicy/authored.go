package gatepolicy

// Authored is one parameter as an owner left it on the record its scope names:
// the number, and whether they authored one at all. The two are one value
// because the distinction is the whole of how authoring works — authoring is an
// override rather than a requirement, so where the field is empty the value in
// force is what the score supplies at that moment, and a factory with nothing
// authored in it runs.
//
// Present and a number of zero are different answers and both are real: a
// threshold authored at zero auto-passes nothing and puts a human at every
// firing, which is a decision an owner may make and not the absence of one.
// Every record that holds an authored parameter stores it as a column that is
// null when absent, and reads it back into this.
type Authored struct {
	Number  float64
	Present bool
}

// Or is the number an owner authored, or supplied where they authored none. It
// is the first two of the three reads an effective value is — the third, the
// clamp a pin applies, is package policy's, which is where a pin's subjects are
// known.
func (a Authored) Or(supplied float64) float64 {
	if a.Present {
		return a.Number
	}
	return supplied
}
