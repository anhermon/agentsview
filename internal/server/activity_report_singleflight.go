package server

import (
	"context"
	"sync"

	"go.kenn.io/agentsview/internal/activity"
)

type activityReportFlight struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	waiters   int
	artifacts activity.CandidateArtifacts
	err       error
}

type activityReportBuildGroup struct {
	mu      sync.Mutex
	flights map[string]*activityReportFlight
}

func newActivityReportBuildGroup() *activityReportBuildGroup {
	return &activityReportBuildGroup{flights: make(map[string]*activityReportFlight)}
}

func (group *activityReportBuildGroup) do(
	ctx context.Context,
	key string,
	build func(context.Context) (activity.CandidateArtifacts, error),
) (activity.CandidateArtifacts, error) {
	group.mu.Lock()
	flight := group.flights[key]
	if flight == nil {
		buildCtx, cancel := context.WithCancel(context.Background())
		flight = &activityReportFlight{
			ctx: buildCtx, cancel: cancel, done: make(chan struct{}), waiters: 1,
		}
		group.flights[key] = flight
		go func() {
			flight.artifacts, flight.err = build(flight.ctx)
			group.mu.Lock()
			delete(group.flights, key)
			close(flight.done)
			group.mu.Unlock()
		}()
	} else {
		flight.waiters++
	}
	group.mu.Unlock()

	select {
	case <-flight.done:
		group.release(flight, false)
		return flight.artifacts, flight.err
	case <-ctx.Done():
		group.release(flight, true)
		return activity.CandidateArtifacts{}, ctx.Err()
	}
}

func (group *activityReportBuildGroup) release(
	flight *activityReportFlight, canceled bool,
) {
	group.mu.Lock()
	defer group.mu.Unlock()
	flight.waiters--
	if canceled && flight.waiters == 0 {
		flight.cancel()
	}
}
