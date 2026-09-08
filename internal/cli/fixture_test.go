package cli

import (
	"context"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

type fixtureAdapter struct {
	observation evidence.Observation
	calls       int
	requests    []evidence.Request
}

func (a *fixtureAdapter) Observe(_ context.Context, request evidence.Request) (evidence.Observation, error) {
	a.calls++
	a.requests = append(a.requests, request)
	return a.observation, nil
}

var _ Adapter = (*fixtureAdapter)(nil)
