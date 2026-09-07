// fakeViews is every read screens.Views may make: a func field per method,
// so a test supplies only the ones it needs and reads what it was handed off
// the closure it wrote. A method left nil answers the zero view with no
// error, which is what a test that does not care about that address gets.
package screens_test

import (
	"context"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

type fakeViews struct {
	home       func(context.Context, principal.Principal) (screens.Home, error)
	work       func(context.Context, principal.Principal, screens.Filter) (screens.Work, error)
	item       func(context.Context, principal.Principal, string) (screens.Item, error)
	decision   func(context.Context, principal.Principal, string) (screens.Decision, error)
	ops        func(context.Context, principal.Principal) (screens.Ops, error)
	serviceOn  func(context.Context, principal.Principal, string, string) (screens.Service, error)
	factory    func(context.Context, principal.Principal) (screens.Factory, error)
	constraint func(context.Context, principal.Principal, string) (screens.Constraint, error)
	people     func(context.Context, principal.Principal) (screens.People, error)
}

func (f *fakeViews) Home(ctx context.Context, p principal.Principal) (screens.Home, error) {
	if f.home == nil {
		return screens.Home{}, nil
	}
	return f.home(ctx, p)
}

func (f *fakeViews) Work(ctx context.Context, p principal.Principal, filter screens.Filter) (screens.Work, error) {
	if f.work == nil {
		return screens.Work{}, nil
	}
	return f.work(ctx, p, filter)
}

func (f *fakeViews) Item(ctx context.Context, p principal.Principal, id string) (screens.Item, error) {
	if f.item == nil {
		return screens.Item{}, nil
	}
	return f.item(ctx, p, id)
}

func (f *fakeViews) Decision(ctx context.Context, p principal.Principal, id string) (screens.Decision, error) {
	if f.decision == nil {
		return screens.Decision{}, nil
	}
	return f.decision(ctx, p, id)
}

func (f *fakeViews) Ops(ctx context.Context, p principal.Principal) (screens.Ops, error) {
	if f.ops == nil {
		return screens.Ops{}, nil
	}
	return f.ops(ctx, p)
}

func (f *fakeViews) ServiceOn(ctx context.Context, p principal.Principal, serviceID, environmentID string) (screens.Service, error) {
	if f.serviceOn == nil {
		return screens.Service{}, nil
	}
	return f.serviceOn(ctx, p, serviceID, environmentID)
}

func (f *fakeViews) Factory(ctx context.Context, p principal.Principal) (screens.Factory, error) {
	if f.factory == nil {
		return screens.Factory{}, nil
	}
	return f.factory(ctx, p)
}

func (f *fakeViews) Constraint(ctx context.Context, p principal.Principal, id string) (screens.Constraint, error) {
	if f.constraint == nil {
		return screens.Constraint{}, nil
	}
	return f.constraint(ctx, p, id)
}

func (f *fakeViews) People(ctx context.Context, p principal.Principal) (screens.People, error) {
	if f.people == nil {
		return screens.People{}, nil
	}
	return f.people(ctx, p)
}
