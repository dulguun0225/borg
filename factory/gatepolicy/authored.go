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
// clamp a safeguard applies, is package policy's, which is where a safeguard's
// subjects are known.
func (a Authored) Or(supplied float64) float64 {
	if a.Present {
		return a.Number
	}
	return supplied
}

// Unauthored is the value in force where an owner authored none: the value the
// design fixes for the parameter rather than one the score supplies, because no
// outcome teaches it. A parameter the score supplies has [Unauthored.Given]
// false and the score's own value stands there instead, which is what makes a
// fixed value a field here rather than a sentence in [Definition.Unit] a reader
// has to parse.
type Unauthored struct {
	// Given is whether the design fixes one at all.
	Given bool
	// Unbounded is the fixed value that places no bound: the decision log and
	// the report store kept for the life of the install, arrival at the way in
	// unbounded, a schema-change snapshot standing until an owner deletes it,
	// and every hour a paging hour. Number is nothing where it is set, there
	// being no number that means no bound.
	Unbounded bool
	Number    float64
	List      []string
}
