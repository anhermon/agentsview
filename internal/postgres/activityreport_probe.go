package postgres

import (
	"context"
	"fmt"

	"go.kenn.io/agentsview/internal/activity"
)

func (s *Store) ActivityReportSourceProbe(
	ctx context.Context,
) (activity.SourceProbe, error) {
	var probe activity.SourceProbe
	err := s.pg.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM sessions),
		COALESCE((SELECT MAX(local_modified_at)::text FROM sessions), ''),
		COALESCE((SELECT MAX(data_version) FROM sessions), 0),
		COALESCE((SELECT MAX(id) FROM messages), 0),
		COALESCE((SELECT MAX(id) FROM usage_events), 0),
		COALESCE((SELECT MAX(updated_at)::text FROM model_pricing), '')`).Scan(
		&probe.SessionCount, &probe.MaxSessionModified, &probe.MaxDataVersion,
		&probe.MaxMessageID, &probe.MaxUsageID, &probe.MaxPricingUpdated,
	)
	if err != nil {
		return activity.SourceProbe{}, fmt.Errorf("probing pg activity report source: %w", err)
	}
	return probe, nil
}
