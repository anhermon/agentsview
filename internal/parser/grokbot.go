package parser

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GrokBot (Cursor Sand / desktop agent) stores transcripts at:
// ~/sand-data/agent-transcripts/<id>/<id>.jsonl
// or ~/agent-data/agent-transcripts/<id>/<id>.jsonl

type grokbotMessage struct {
	Role    string                 `json:"role"`
	Message grokbotMessageContents `json:"message"`
}

type grokbotMessageContents struct {
	Content []grokbotContentBlock `json:"content"`
}

type grokbotContentBlock struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	ToolUseID  string `json:"tool_use_id"`
	Name       string `json:"name"`
	Input      any    `json:"input"`
	ToolResult any    `json:"tool_result"`
}

func ParseGrokBotSession(
	path, machine string,
) (ParseResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ParseResult{}, fmt.Errorf("stat %s: %w", path, err)
	}

	rawID := filepath.Base(filepath.Dir(path))
	if !IsValidSessionID(rawID) {
		return ParseResult{}, fmt.Errorf("invalid grokbot session id for %s", path)
	}

	messages, hasAutomation, err := parseGrokBotJSONL(path)
	if err != nil {
		return ParseResult{}, err
	}

	if len(messages) == 0 {
		return ParseResult{}, fmt.Errorf("grokbot session %s has no messages", rawID)
	}

	firstPrompt := ""
	userMessageCount := 0
	for _, msg := range messages {
		if msg.Role == RoleUser && strings.TrimSpace(msg.Content) != "" {
			userMessageCount++
			if firstPrompt == "" {
				if strings.TrimSpace(msg.Content) != "" {
					firstPrompt = truncate(
						strings.ReplaceAll(msg.Content, "\n", " "), 300,
					)
				}
			}
		}
	}

	startedAt := time.Time{}
	endedAt := info.ModTime().UTC()
	if len(messages) > 0 {
		if !messages[0].Timestamp.IsZero() {
			startedAt = messages[0].Timestamp
		}
		for _, msg := range messages {
			if !msg.Timestamp.IsZero() {
				if startedAt.IsZero() || msg.Timestamp.Before(startedAt) {
					startedAt = msg.Timestamp
				}
				if msg.Timestamp.After(endedAt) {
					endedAt = msg.Timestamp
				}
			}
		}
	}
	if startedAt.IsZero() {
		startedAt = endedAt
	}

	sessionKind := ""
	if hasAutomation {
		sessionKind = SessionKindNonInteractive
	}

	parentSessionID, relationshipType := grokbotDetectParent(path, rawID)

	session := ParsedSession{
		ID:               "grokbot:" + rawID,
		Agent:            AgentGrokBot,
		Machine:          machine,
		Project:          "",
		FirstMessage:     firstPrompt,
		StartedAt:        startedAt,
		EndedAt:          endedAt,
		MessageCount:     len(messages),
		UserMessageCount: userMessageCount,
		SessionKind:      sessionKind,
		ParentSessionID:  parentSessionID,
		RelationshipType: relationshipType,
		File: FileInfo{
			Path:  path,
			Size:  info.Size(),
			Mtime: info.ModTime().UnixNano(),
		},
	}

	return ParseResult{Session: session, Messages: messages}, nil
}

func grokbotStripAutomationMarkers(text string) string {
	cleaned := strings.ReplaceAll(text, "[SAND_HIDDEN_PROMPT]", "")
	cleaned = strings.ReplaceAll(cleaned, "[SAND_TRUSTED_AUTOMATION_PROMPT]", "")
	return strings.TrimSpace(cleaned)
}

func grokbotHasAutomationMarkers(text string) bool {
	return strings.Contains(text, "[SAND_HIDDEN_PROMPT]") ||
		strings.Contains(text, "[SAND_TRUSTED_AUTOMATION_PROMPT]")
}

func grokbotDetectParent(sessionPath, childID string) (string, RelationshipType) {
	if !strings.HasPrefix(childID, "sand-subagent-") {
		return "", RelNone
	}

	sessionDir := filepath.Dir(sessionPath)
	transcriptsRoot := filepath.Dir(sessionDir)

	entries, err := os.ReadDir(transcriptsRoot)
	if err != nil {
		return "", RelNone
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == childID {
			continue
		}
		if !IsValidSessionID(entry.Name()) {
			continue
		}
		if strings.HasPrefix(entry.Name(), "sand-subagent-") {
			continue
		}
		return "grokbot:" + entry.Name(), RelSubagent
	}

	return "", RelNone
}

func parseGrokBotJSONL(path string) ([]ParsedMessage, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var messages []ParsedMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	ordinal := 0
	hasAutomation := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg grokbotMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		parsed, msgHasAutomation := grokbotMessageFromLine(msg, ordinal)
		if parsed != nil {
			messages = append(messages, *parsed)
			if msgHasAutomation {
				hasAutomation = true
			}
			ordinal++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan %s: %w", path, err)
	}

	return messages, hasAutomation, nil
}

func grokbotMessageFromLine(msg grokbotMessage, ordinal int) (*ParsedMessage, bool) {
	role := strings.ToLower(strings.TrimSpace(msg.Role))
	if role == "" {
		return nil, false
	}

	parsed := ParsedMessage{
		Ordinal: ordinal,
	}

	switch role {
	case "user":
		parsed.Role = RoleUser
	case "assistant":
		parsed.Role = RoleAssistant
	case "tool":
		parsed.Role = RoleUser
	default:
		return nil, false
	}

	var textParts []string
	var toolCalls []ParsedToolCall
	var toolResults []ParsedToolResult
	hasAutomationMarkers := false

	for _, block := range msg.Message.Content {
		switch block.Type {
		case "text":
			if text := strings.TrimSpace(block.Text); text != "" {
				if !hasAutomationMarkers && grokbotHasAutomationMarkers(text) {
					hasAutomationMarkers = true
				}
				cleaned := grokbotStripAutomationMarkers(text)
				if cleaned != "" {
					textParts = append(textParts, cleaned)
				}
			}
		case "tool_use":
			inputJSON := "{}"
			if block.Input != nil {
				if inputBytes, err := json.Marshal(block.Input); err == nil {
					inputJSON = string(inputBytes)
				}
			}
			toolCalls = append(toolCalls, ParsedToolCall{
				ToolUseID: block.ToolUseID,
				ToolName:  block.Name,
				Category:  NormalizeToolCategory(block.Name),
				InputJSON: inputJSON,
			})
		case "tool_result":
			resultText := ""
			if block.ToolResult != nil {
				if resultStr, ok := block.ToolResult.(string); ok {
					resultText = resultStr
				} else if resultBytes, err := json.Marshal(block.ToolResult); err == nil {
					resultText = string(resultBytes)
				}
			}
			toolResults = append(toolResults, ParsedToolResult{
				ToolUseID:     block.ToolUseID,
				ContentLength: len(resultText),
				ContentRaw:    resultText,
			})
		}
	}

	content := strings.Join(textParts, "\n")
	parsed.Content = content
	parsed.ContentLength = len(content)

	if len(toolCalls) > 0 {
		parsed.HasToolUse = true
		parsed.ToolCalls = toolCalls
	}
	if len(toolResults) > 0 {
		parsed.ToolResults = toolResults
	}

	if parsed.ContentLength == 0 && len(toolCalls) == 0 && len(toolResults) == 0 {
		return nil, false
	}

	return &parsed, hasAutomationMarkers
}
