package parser

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Cursor IDE (the GUI editor) is a completely different session store from
// Cursor Agent (the CLI, see cursor.go): it is a VS Code-style global-state
// SQLite database, state.vscdb, whose cursorDiskKV table holds one JSON blob
// per key. A "composerData:<uuid>" key is one chat session; its
// fullConversationHeadersOnly array lists that session's turns in order, each
// pointing at a sibling "bubbleId:<composerId>:<bubbleUuid>" row holding the
// turn's text. conversationMap, the alternative inline-message field, was
// observed empty on every real (including large) conversation, so it is not a
// usable source in this schema version (_v: 16 composerData / _v: 3 bubbles).
const (
	cursorIDEIDPrefix = "cursor-ide:"
	// CursorIDEDBRelPath is the container's path relative to the provider
	// root: Cursor IDE's default dirs point straight at globalStorage, and
	// state.vscdb lives directly inside it (no subdirectory).
	CursorIDEDBRelPath         = "state.vscdb"
	cursorIDEComposerKeyPrefix = "composerData:"
	cursorIDEBubbleKeyPrefix   = "bubbleId:"

	// cursorIDEBubbleTypeUser and cursorIDEBubbleTypeAssistant are the
	// cursorDiskKV bubble "type" values.
	cursorIDEBubbleTypeUser      = 1
	cursorIDEBubbleTypeAssistant = 2
)

// cursorIDEDefaultDirs returns platform-specific default directories holding
// state.vscdb.
func cursorIDEDefaultDirs() []string {
	return []string{
		// macOS
		"Library/Application Support/Cursor/User/globalStorage",
		// Linux
		".config/Cursor/User/globalStorage",
		// Windows
		"AppData/Roaming/Cursor/User/globalStorage",
	}
}

func openCursorIDEDB(dbPath string) (*sql.DB, error) {
	dsn := "file:" + sqliteURIPath(dbPath) + "?mode=ro&_busy_timeout=3000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening cursor IDE db %s: %w", dbPath, err)
	}
	return db, nil
}

// CursorIDEComposerExists reports whether a composerData row with the given
// composer ID exists in state.vscdb.
func CursorIDEComposerExists(dbPath, composerID string) bool {
	if dbPath == "" || composerID == "" || !IsValidSessionID(composerID) {
		return false
	}
	conn, err := openCursorIDEDB(dbPath)
	if err != nil {
		return false
	}
	defer conn.Close()
	var one int
	err = conn.QueryRow(
		`SELECT 1 FROM cursorDiskKV WHERE key = ? LIMIT 1`,
		cursorIDEComposerKeyPrefix+composerID,
	).Scan(&one)
	return err == nil
}

// cursorIDEComposerHeader is one entry of fullConversationHeadersOnly: the
// bubble to look up and its rendered role, in transcript order.
type cursorIDEComposerHeader struct {
	BubbleID string `json:"bubbleId"`
	Type     int    `json:"type"`
}

type cursorIDEGitBranch struct {
	BranchName        string `json:"branchName"`
	LastInteractionAt int64  `json:"lastInteractionAt"`
}

type cursorIDEGitRepo struct {
	RepoPath string               `json:"repoPath"`
	Branches []cursorIDEGitBranch `json:"branches"`
}

type cursorIDEWorkspaceIdentifier struct {
	URI struct {
		FSPath string `json:"fsPath"`
	} `json:"uri"`
}

// cursorIDEComposerDoc is the composerData:<uuid> document. Only the fields
// the parser needs are modeled; the rest of the blob (rich editor state,
// capability flags, encryption keys for Cursor's own cloud sync, ...) is
// ignored.
type cursorIDEComposerDoc struct {
	Headers             []cursorIDEComposerHeader    `json:"fullConversationHeadersOnly"`
	Name                string                       `json:"name"`
	CreatedAt           int64                        `json:"createdAt"`
	LastUpdatedAt       int64                        `json:"lastUpdatedAt"`
	WorkspaceIdentifier cursorIDEWorkspaceIdentifier `json:"workspaceIdentifier"`
	TrackedGitRepos     []cursorIDEGitRepo           `json:"trackedGitRepos"`
}

// cursorIDEComposerMeta is a lightweight per-composer descriptor for the
// engine's freshness check, decoded without loading the composer's bubbles.
type cursorIDEComposerMeta struct {
	rawID         string
	lastUpdatedAt int64
	headerCount   int
}

// fingerprint returns a cheap classification digest over lastUpdatedAt and
// header count, mirroring omnigentMeta.fingerprint(): it catches ordinary
// edits without reading every bubble, at the cost of missing a same-second
// in-place bubble edit that leaves both fields unchanged. The composite
// container fingerprint (whole-DB mtime) is the periodic backstop for that.
func (m cursorIDEComposerMeta) fingerprint() string {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d|%d", m.lastUpdatedAt, m.headerCount)
	return strconv.FormatUint(h.Sum64(), 16)
}

func loadCursorIDEComposerMeta(
	ctx context.Context, conn *sql.DB, composerID string,
) (cursorIDEComposerMeta, bool, error) {
	var raw []byte
	err := conn.QueryRowContext(ctx,
		`SELECT value FROM cursorDiskKV WHERE key = ?`,
		cursorIDEComposerKeyPrefix+composerID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return cursorIDEComposerMeta{}, false, nil
	}
	if err != nil {
		return cursorIDEComposerMeta{}, false, fmt.Errorf(
			"loading cursor IDE composer meta %s: %w", composerID, err)
	}
	var doc cursorIDEComposerDoc
	if json.Unmarshal(raw, &doc) != nil {
		return cursorIDEComposerMeta{}, false, nil
	}
	return cursorIDEComposerMeta{
		rawID:         composerID,
		lastUpdatedAt: doc.LastUpdatedAt,
		headerCount:   len(doc.Headers),
	}, true, nil
}

// listCursorIDEComposerIDs returns every composer ID in state.vscdb.
func listCursorIDEComposerIDs(ctx context.Context, conn *sql.DB) ([]string, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT key FROM cursorDiskKV WHERE key LIKE ? ORDER BY key`,
		cursorIDEComposerKeyPrefix+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("listing cursor IDE composers: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning cursor IDE composer key: %w", err)
		}
		id := strings.TrimPrefix(key, cursorIDEComposerKeyPrefix)
		if IsValidSessionID(id) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// cursorIDEToolFormerData is a bubble's embedded tool call: unlike Claude's
// separate call/result blocks, Cursor stores one tool invocation (input,
// status, and output) inline on the assistant bubble that issued it.
type cursorIDEToolFormerData struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	RawArgs    string `json:"rawArgs"`
	Result     string `json:"result"`
}

type cursorIDEBubble struct {
	Type           int                      `json:"type"`
	Text           string                   `json:"text"`
	CreatedAt      string                   `json:"createdAt"`
	ToolFormerData *cursorIDEToolFormerData `json:"toolFormerData"`
}

func loadCursorIDEBubble(
	ctx context.Context, conn *sql.DB, composerID, bubbleID string,
) (*cursorIDEBubble, error) {
	var raw []byte
	err := conn.QueryRowContext(ctx,
		`SELECT value FROM cursorDiskKV WHERE key = ?`,
		cursorIDEBubbleKeyPrefix+composerID+":"+bubbleID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		// Cursor version updates have been observed to shrink or wipe rows
		// out of cursorDiskKV; a missing bubble is a gap in the transcript,
		// not a fatal error.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"loading cursor IDE bubble %s:%s: %w", composerID, bubbleID, err)
	}
	var bubble cursorIDEBubble
	if json.Unmarshal(raw, &bubble) != nil {
		return nil, nil
	}
	return &bubble, nil
}

// cursorIDEMessageFromBubble converts one decoded bubble into a
// ParsedMessage. Returns ok=false for a bubble with no visible content (pure
// bookkeeping bubbles such as in-flight generation placeholders).
func cursorIDEMessageFromBubble(ordinal int, bubble cursorIDEBubble) (ParsedMessage, bool) {
	content := strings.TrimSpace(bubble.Text)
	msg := ParsedMessage{
		Ordinal:       ordinal,
		ContentLength: len(content),
		Timestamp:     parseTimestamp(bubble.CreatedAt),
	}

	switch bubble.Type {
	case cursorIDEBubbleTypeUser:
		if content == "" {
			return ParsedMessage{}, false
		}
		msg.Role = RoleUser
		msg.Content = content
		return msg, true

	case cursorIDEBubbleTypeAssistant:
		msg.Role = RoleAssistant
		msg.Content = content
		if bubble.ToolFormerData == nil {
			if content == "" {
				return ParsedMessage{}, false
			}
			return msg, true
		}
		tfd := bubble.ToolFormerData
		msg.HasToolUse = true
		msg.ToolCalls = []ParsedToolCall{{
			ToolUseID: tfd.ToolCallID,
			ToolName:  tfd.Name,
			Category:  NormalizeToolCategory(tfd.Name),
			InputJSON: tfd.RawArgs,
		}}
		if tfd.Result != "" {
			quoted, err := json.Marshal(tfd.Result)
			if err == nil {
				msg.ToolResults = []ParsedToolResult{{
					ToolUseID:     tfd.ToolCallID,
					ContentLength: len(tfd.Result),
					ContentRaw:    string(quoted),
				}}
			}
		}
		return msg, true

	default:
		return ParsedMessage{}, false
	}
}

// cursorIDELatestBranch picks the branch with the most recent
// lastInteractionAt across every tracked git repo, matching the workspace
// picker Cursor itself shows.
func cursorIDELatestBranch(repos []cursorIDEGitRepo) string {
	var branch string
	var latest int64
	for _, repo := range repos {
		for _, b := range repo.Branches {
			if b.BranchName == "" {
				continue
			}
			if branch == "" || b.LastInteractionAt > latest {
				branch = b.BranchName
				latest = b.LastInteractionAt
			}
		}
	}
	return branch
}

func cursorIDECwd(doc cursorIDEComposerDoc) string {
	if fsPath := doc.WorkspaceIdentifier.URI.FSPath; fsPath != "" {
		return fsPath
	}
	if len(doc.TrackedGitRepos) > 0 {
		return doc.TrackedGitRepos[0].RepoPath
	}
	return ""
}

// parseCursorIDEComposer decodes one composer into a ParseResult using an
// already-open connection. Returns (nil, nil) for a composer with zero
// renderable messages (an empty draft, or one whose bubbles were all wiped by
// a Cursor version update).
func parseCursorIDEComposer(
	ctx context.Context, conn *sql.DB, dbPath, composerID, machine string,
	dbInfo os.FileInfo,
) (*ParseResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var raw []byte
	err := conn.QueryRowContext(ctx,
		`SELECT value FROM cursorDiskKV WHERE key = ?`,
		cursorIDEComposerKeyPrefix+composerID,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"loading cursor IDE composer %s: %w", composerID, err)
	}
	var doc cursorIDEComposerDoc
	if json.Unmarshal(raw, &doc) != nil {
		return nil, nil
	}
	if len(doc.Headers) == 0 {
		return nil, nil
	}

	var messages []ParsedMessage
	for _, header := range doc.Headers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		bubble, err := loadCursorIDEBubble(ctx, conn, composerID, header.BubbleID)
		if err != nil {
			return nil, err
		}
		if bubble == nil {
			continue
		}
		msg, ok := cursorIDEMessageFromBubble(len(messages), *bubble)
		if !ok {
			continue
		}
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		return nil, nil
	}

	var firstMessage string
	userCount := 0
	for _, m := range messages {
		if m.Role == RoleUser {
			userCount++
			if firstMessage == "" && m.Content != "" {
				firstMessage = truncate(
					strings.ReplaceAll(m.Content, "\n", " "), 300,
				)
			}
		}
	}

	startedAt := cursorIDETime(doc.CreatedAt)
	if startedAt.IsZero() && len(messages) > 0 {
		startedAt = messages[0].Timestamp
	}
	endedAt := cursorIDETime(doc.LastUpdatedAt)
	if endedAt.IsZero() {
		if last := messages[len(messages)-1].Timestamp; !last.IsZero() {
			endedAt = last
		} else {
			endedAt = startedAt
		}
	}

	cwd := cursorIDECwd(doc)
	sess := ParsedSession{
		ID:               cursorIDEIDPrefix + composerID,
		Agent:            AgentCursorIDE,
		Machine:          machine,
		Project:          ExtractProjectFromCwd(cwd),
		Cwd:              cwd,
		GitBranch:        cursorIDELatestBranch(doc.TrackedGitRepos),
		SessionName:      doc.Name,
		FirstMessage:     firstMessage,
		StartedAt:        startedAt,
		EndedAt:          endedAt,
		MessageCount:     len(messages),
		UserMessageCount: userCount,
		File: FileInfo{
			Path:  VirtualSourcePath(dbPath, composerID),
			Size:  dbInfo.Size(),
			Mtime: endedAt.UnixNano(),
			Hash: cursorIDEComposerMeta{
				lastUpdatedAt: doc.LastUpdatedAt,
				headerCount:   len(doc.Headers),
			}.fingerprint(),
		},
	}

	return &ParseResult{Session: sess, Messages: messages}, nil
}

// cursorIDETime converts an epoch-milliseconds stamp (composerData
// createdAt/lastUpdatedAt) to UTC. Zero stays zero. Bubble timestamps use a
// different encoding (ISO-8601 strings) and go through parseTimestamp
// instead.
func cursorIDETime(epochMS int64) time.Time {
	if epochMS == 0 {
		return time.Time{}
	}
	return time.UnixMilli(epochMS).UTC()
}
