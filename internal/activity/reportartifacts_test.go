package activity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.kenn.io/agentsview/internal/money"
)

func TestPageSessionsDefaultOrderMembershipAndCursorPosition(t *testing.T) {
	five, ten := 5.0, 10.0
	rows := []SessionRow{
		{SessionID: "untimed"},
		{SessionID: "b", AgentMinutes: &ten},
		{SessionID: "a", AgentMinutes: &ten},
		{SessionID: "c", AgentMinutes: &five},
	}
	membership := map[string]BucketMembership{
		"a": {1}, "b": {1}, "c": {2},
	}
	bucket := 0
	page, err := PageSessions(rows, membership, SessionPageOptions{
		Limit: 1, Bucket: &bucket,
	})
	require.NoError(t, err)
	require.Equal(t, 2, page.Total)
	require.Len(t, page.Sessions, 1)
	assert.Equal(t, "a", page.Sessions[0].SessionID)
	assert.True(t, page.HasNext)
	assert.Equal(t, 1, page.Next)

	page, err = PageSessions(rows, membership, SessionPageOptions{
		Limit: 10, Offset: page.Next, Bucket: &bucket,
	})
	require.NoError(t, err)
	require.Len(t, page.Sessions, 1)
	assert.Equal(t, "b", page.Sessions[0].SessionID)
}

func TestPageSessionsAlternateSortHasSessionIDTieBreak(t *testing.T) {
	rows := []SessionRow{
		{SessionID: "b", Project: "same", Cost: money.Money{Microdollars: 2}},
		{SessionID: "a", Project: "same", Cost: money.Money{Microdollars: 2}},
	}
	for _, sortKey := range []SessionSort{SessionSortProject, SessionSortCost} {
		page, err := PageSessions(rows, nil, SessionPageOptions{
			Sort: sortKey, Direction: "desc",
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, []string{
			page.Sessions[0].SessionID, page.Sessions[1].SessionID,
		})
	}
}

func TestArtifactDigestIsIndependentOfMembershipMapOrder(t *testing.T) {
	left := CandidateArtifacts{
		Report:     Report{Timezone: "UTC"},
		Sessions:   []SessionRow{{SessionID: "a"}, {SessionID: "b"}},
		Membership: map[string]BucketMembership{"a": {1}, "b": {2}},
	}
	right := CandidateArtifacts{
		Report: left.Report, Sessions: left.Sessions,
		Membership: map[string]BucketMembership{"b": {2}, "a": {1}},
	}
	leftDigest, err := ArtifactDigest(left)
	require.NoError(t, err)
	rightDigest, err := ArtifactDigest(right)
	require.NoError(t, err)
	assert.Equal(t, leftDigest, rightDigest)
	right.Sessions[0].Agent = "changed"
	changed, err := ArtifactDigest(right)
	require.NoError(t, err)
	assert.NotEqual(t, leftDigest, changed)
}
