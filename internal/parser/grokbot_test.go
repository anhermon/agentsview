package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGrokBotSession(t *testing.T) {
	fixtureDir := t.TempDir()
	sessionID := "test-session-123"
	sessionDir := filepath.Join(fixtureDir, sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	jsonlPath := filepath.Join(sessionDir, sessionID+".jsonl")
	fixture := `{"role":"user","message":{"content":[{"type":"text","text":"Hello, can you help me?"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"Of course! I'd be happy to help."}]}}
{"role":"assistant","message":{"content":[{"type":"tool_use","tool_use_id":"tool_001","name":"read_file","input":{"path":"test.go"}}]}}
{"role":"tool","message":{"content":[{"type":"tool_result","tool_use_id":"tool_001","tool_result":"package main\n"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"I can see the file."}]}}
`
	require.NoError(t, os.WriteFile(jsonlPath, []byte(fixture), 0644))

	result, err := ParseGrokBotSession(jsonlPath, "test-machine")
	require.NoError(t, err)

	assert.Equal(t, "grokbot:"+sessionID, result.Session.ID)
	assert.Equal(t, AgentGrokBot, result.Session.Agent)
	assert.Equal(t, "test-machine", result.Session.Machine)
	assert.Equal(t, 5, result.Session.MessageCount)
	assert.Equal(t, 1, result.Session.UserMessageCount)
	assert.Contains(t, result.Session.FirstMessage, "Hello, can you help")

	require.Len(t, result.Messages, 5)

	assert.Equal(t, RoleUser, result.Messages[0].Role)
	assert.Contains(t, result.Messages[0].Content, "Hello, can you help")

	assert.Equal(t, RoleAssistant, result.Messages[1].Role)
	assert.Contains(t, result.Messages[1].Content, "happy to help")

	assert.Equal(t, RoleAssistant, result.Messages[2].Role)
	assert.True(t, result.Messages[2].HasToolUse)
	require.Len(t, result.Messages[2].ToolCalls, 1)
	assert.Equal(t, "tool_001", result.Messages[2].ToolCalls[0].ToolUseID)
	assert.Equal(t, "read_file", result.Messages[2].ToolCalls[0].ToolName)

	assert.Equal(t, RoleUser, result.Messages[3].Role)
	require.Len(t, result.Messages[3].ToolResults, 1)
	assert.Equal(t, "tool_001", result.Messages[3].ToolResults[0].ToolUseID)
	assert.Contains(t, result.Messages[3].ToolResults[0].ContentRaw, "package main")

	assert.Equal(t, RoleAssistant, result.Messages[4].Role)
	assert.Contains(t, result.Messages[4].Content, "I can see the file")
}

func TestParseGrokBotSessionEmpty(t *testing.T) {
	fixtureDir := t.TempDir()
	sessionID := "empty-session"
	sessionDir := filepath.Join(fixtureDir, sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	jsonlPath := filepath.Join(sessionDir, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(jsonlPath, []byte(""), 0644))

	_, err := ParseGrokBotSession(jsonlPath, "test-machine")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "has no messages")
}

func TestParseGrokBotSessionLargeLines(t *testing.T) {
	fixtureDir := t.TempDir()
	sessionID := "large-line-session"
	sessionDir := filepath.Join(fixtureDir, sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0755))

	jsonlPath := filepath.Join(sessionDir, sessionID+".jsonl")

	// Create a tool result with a large content block (~100KB)
	largeContent := make([]byte, 100*1024)
	for i := range largeContent {
		largeContent[i] = byte('a' + (i % 26))
	}

	fixture := `{"role":"user","message":{"content":[{"type":"text","text":"Read this large file"}]}}
{"role":"assistant","message":{"content":[{"type":"tool_use","tool_use_id":"large_001","name":"read_file","input":{"path":"large.txt"}}]}}
{"role":"tool","message":{"content":[{"type":"tool_result","tool_use_id":"large_001","tool_result":"` + string(largeContent) + `"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"Done reading."}]}}
`
	require.NoError(t, os.WriteFile(jsonlPath, []byte(fixture), 0644))

	result, err := ParseGrokBotSession(jsonlPath, "test-machine")
	require.NoError(t, err, "should handle large lines without scanner buffer overflow")

	assert.Equal(t, "grokbot:"+sessionID, result.Session.ID)
	assert.Equal(t, 4, result.Session.MessageCount)

	require.Len(t, result.Messages, 4)
	assert.Equal(t, RoleUser, result.Messages[2].Role)
	require.Len(t, result.Messages[2].ToolResults, 1)
	assert.Equal(t, "large_001", result.Messages[2].ToolResults[0].ToolUseID)
	assert.GreaterOrEqual(t, result.Messages[2].ToolResults[0].ContentLength, 100*1024,
		"should successfully parse tool result with large content without scanner overflow")
}

func TestGrokBotProviderDiscovery(t *testing.T) {
	fixtureDir := t.TempDir()

	session1ID := "session-aaa"
	session1Dir := filepath.Join(fixtureDir, session1ID)
	require.NoError(t, os.MkdirAll(session1Dir, 0755))
	jsonl1 := filepath.Join(session1Dir, session1ID+".jsonl")
	require.NoError(t, os.WriteFile(jsonl1, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"test"}]}}`+"\n"), 0644))

	session2ID := "session-bbb"
	session2Dir := filepath.Join(fixtureDir, session2ID)
	require.NoError(t, os.MkdirAll(session2Dir, 0755))
	jsonl2 := filepath.Join(session2Dir, session2ID+".jsonl")
	require.NoError(t, os.WriteFile(jsonl2, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"another"}]}}`+"\n"), 0644))

	def := AgentDef{
		Type:        AgentGrokBot,
		DisplayName: "Grok Bot",
		IDPrefix:    "grokbot:",
		FileBased:   true,
	}

	factory := newGrokBotProviderFactory(def)
	cfg := ProviderConfig{
		Roots:   []string{fixtureDir},
		Machine: "test-machine",
	}
	provider := factory.NewProvider(cfg)

	sources, err := provider.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, sources, 2)

	paths := make([]string, len(sources))
	for i, src := range sources {
		paths[i] = providerSourcePath(src)
	}
	assert.Contains(t, paths, jsonl1)
	assert.Contains(t, paths, jsonl2)
}

func TestGrokBotClassifyPath(t *testing.T) {
	root := "/home/user/sand-data/agent-transcripts"

	tests := []struct {
		name  string
		path  string
		valid bool
	}{
		{
			name:  "valid session jsonl",
			path:  "/home/user/sand-data/agent-transcripts/abc-123/abc-123.jsonl",
			valid: true,
		},
		{
			name:  "wrong file name normalizes to canonical",
			path:  "/home/user/sand-data/agent-transcripts/abc-123/other.jsonl",
			valid: true,
		},
		{
			name:  "not in session dir",
			path:  "/home/user/sand-data/agent-transcripts/abc-123.jsonl",
			valid: false,
		},
		{
			name:  "invalid session id",
			path:  "/home/user/sand-data/agent-transcripts/../../abc.jsonl",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := grokbotClassifyPath(root, tt.path, true)
			assert.Equal(t, tt.valid, ok)
		})
	}
}
