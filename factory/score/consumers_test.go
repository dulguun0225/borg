package score

import (
	"context"
	"testing"
)

// TestConsumersIsUnavailableWhereTheFiringNamesNoService: on the row that
// decides a version of what an agent is told, which belongs to no item and no
// service, every factor that would have read them resolves the way an
// unavailable factor always does, [Score.Assess]'s own doc.go says. A change
// naming no service is one of them, and a level of nothing is a value the
// score computed, not the absence [ErrChangeIncomplete] already refuses above
// this one set.
func TestConsumersIsUnavailableWhereTheFiringNamesNoService(t *testing.T) {
	s := &Score{}
	ctx := context.Background()

	unnamed, err := s.consumers(ctx, Version{}, Change{FactorSet: SetRolePromptOrSkill})
	if err != nil {
		t.Fatalf("consumers: %v", err)
	}
	if unnamed.unavailable == "" {
		t.Errorf("a change naming no service read as %v, and it is unavailable", unnamed.level)
	}
}
