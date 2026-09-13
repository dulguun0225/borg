package score

func adoptionSource(c Change) (reading, bool) {
	if !c.Adoption || !c.AtSpec {
		return reading{}, false
	}
	return reading{
		resolved: "the repository was adopted and no gate has admitted its contents, so a human decides at Spec",
		cause:    CauseAdoptedRepository,
		words:    "the intent adopts an existing repository",
	}, true
}
