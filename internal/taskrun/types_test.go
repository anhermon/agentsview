package taskrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskEnvelopePromptIsCompactAndFetchesDetailsOnDemand(t *testing.T) {
	t.Parallel()

	envelope := TaskEnvelope{
		TaskID:  "TASK-7",
		Summary: "Fix the parser",
		Criteria: []Criterion{
			{ID: "unit", Summary: "unit tests pass"},
		},
		References: []Reference{
			{Label: "failing fixture", Kind: "file", URI: "repo://parser/testdata/broken.jsonl"},
		},
		DetailsRef: "agentsview://tasks/TASK-7/details",
	}
	require.NoError(t, envelope.Validate())

	prompt := envelope.Prompt()
	assert.Contains(t, prompt, "Task TASK-7: Fix the parser")
	assert.Contains(t, prompt, "[unit] unit tests pass")
	assert.Contains(t, prompt, "repo://parser/testdata/broken.jsonl")
	assert.Contains(t, prompt, "Fetch additional details on demand")
	assert.NotContains(t, prompt, "history")
}

func TestTaskEnvelopeValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope TaskEnvelope
		message  string
	}{
		{name: "missing task", envelope: TaskEnvelope{Summary: "work"}, message: "task_id"},
		{name: "missing summary", envelope: TaskEnvelope{TaskID: "T-1"}, message: "summary"},
		{name: "empty criterion", envelope: TaskEnvelope{TaskID: "T-1", Summary: "work", Criteria: []Criterion{{ID: "one"}}}, message: "criterion"},
		{name: "incomplete reference", envelope: TaskEnvelope{TaskID: "T-1", Summary: "work", References: []Reference{{Kind: "file"}}}, message: "reference"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, test.envelope.Validate(), test.message)
		})
	}
}
