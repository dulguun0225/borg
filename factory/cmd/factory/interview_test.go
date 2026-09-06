// The interview asking its question again after a blank answer.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
)

// TestAnEmptyAnswerIsAskedAgain scripts a blank line at the interview. The
// answer is write-once and the interview is one round, so the blank line is
// asked again rather than sent — sending it would stamp the question answered
// with nothing in it.
func TestAnEmptyAnswerIsAskedAgain(t *testing.T) {
	ctx, d, out := newPath(t, "\n"+theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "type one") {
		t.Errorf("the blank line was not asked again:\n%s", out)
	}

	// Three questions: the interviewer's own, answered with the scripted line,
	// the confirming round's, which this command-line interface answers itself —
	// authorintent.go's own comment says why — and the acceptance round the last
	// item going live asked, which waits on the requester.
	questions, err := intent.Questions(ctx, d.pool, only(t, res).intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 3 || questions[0].Answer != theAnswer {
		t.Errorf("the questions are %+v, want the interviewer's answered %q first", questions, theAnswer)
	}
}
