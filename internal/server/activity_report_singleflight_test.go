package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/activity"
)

func TestActivityReportBuildGroupSharesBuildAfterOneWaiterCancels(t *testing.T) {
	group := newActivityReportBuildGroup()
	started := make(chan struct{})
	release := make(chan struct{})
	var builds atomic.Int32
	build := func(ctx context.Context) (activity.CandidateArtifacts, error) {
		if builds.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return activity.CandidateArtifacts{Sessions: []activity.SessionRow{{SessionID: "ok"}}}, nil
		case <-ctx.Done():
			return activity.CandidateArtifacts{}, ctx.Err()
		}
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := group.do(firstCtx, "same", build)
		firstDone <- err
	}()
	<-started
	type result struct {
		artifacts activity.CandidateArtifacts
		err       error
	}
	secondDone := make(chan result, 1)
	go func() {
		artifacts, err := group.do(context.Background(), "same", build)
		secondDone <- result{artifacts: artifacts, err: err}
	}()
	require.Eventually(t, func() bool {
		group.mu.Lock()
		defer group.mu.Unlock()
		return group.flights["same"] != nil && group.flights["same"].waiters == 2
	}, time.Second, time.Millisecond)
	cancelFirst()
	require.ErrorIs(t, <-firstDone, context.Canceled)
	close(release)
	second := <-secondDone
	require.NoError(t, second.err)
	require.Equal(t, "ok", second.artifacts.Sessions[0].SessionID)
	require.Equal(t, int32(1), builds.Load())
}

func TestActivityReportBuildGroupCancelsAbandonedBuild(t *testing.T) {
	group := newActivityReportBuildGroup()
	buildCanceled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := group.do(ctx, "abandoned", func(ctx context.Context) (
			activity.CandidateArtifacts, error,
		) {
			<-ctx.Done()
			close(buildCanceled)
			return activity.CandidateArtifacts{}, ctx.Err()
		})
		done <- err
	}()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	select {
	case <-buildCanceled:
	case <-time.After(time.Second):
		require.FailNow(t, "abandoned build was not canceled")
	}
}
