package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func newGrokBotProviderFactory(def AgentDef) ProviderFactory {
	return NewSingleFileProviderFactory(
		def,
		grokbotProviderCapabilities(),
		func(cfg ProviderConfig) singleFileSourceSet {
			return NewSingleFileSourceSet(
				AgentGrokBot,
				cfg.Roots,
				WithStreamingFileDiscovery(grokbotDiscoverEach),
				WithFileWatchRoots(grokbotWatchRoots),
				WithFileChangedPathClassifier(grokbotClassifyPath),
				WithFileLookup(grokbotFindFile),
				WithFileFingerprint(grokbotFingerprintSource),
				WithFileParse(grokbotParseFile),
			)
		},
	)
}

func grokbotDiscoverEach(
	ctx context.Context, root string, yield func(singleFileMatch) error,
) error {
	return streamDirectoryEntries(ctx, root, func(session os.DirEntry) error {
		if !IsValidSessionID(session.Name()) {
			return nil
		}
		isSessionDir, err := streamingDirCandidateOrIncomplete(
			AgentGrokBot, "GrokBot session directory", session, root,
		)
		if err != nil {
			return err
		}
		if !isSessionDir {
			return nil
		}
		sessionDir := filepath.Join(root, session.Name())
		jsonlPath := filepath.Join(sessionDir, session.Name()+".jsonl")
		if match, ok := grokbotStrictMatch(root, jsonlPath); ok {
			return yield(match)
		}
		return nil
	})
}

func grokbotWatchRoots(roots []string) []WatchRoot {
	out := make([]WatchRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, WatchRoot{
			Path:         root,
			Recursive:    true,
			IncludeGlobs: []string{"*.jsonl"},
			DebounceKey:  string(AgentGrokBot) + ":transcripts:" + root,
		})
	}
	return out
}

func grokbotClassifyPath(
	root, path string, allowMissing bool,
) (singleFileMatch, bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return singleFileMatch{}, false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || !IsValidSessionID(parts[0]) {
		return singleFileMatch{}, false
	}
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") {
		return singleFileMatch{}, false
	}
	jsonlPath := filepath.Join(root, parts[0], parts[0]+".jsonl")
	if allowMissing {
		return singleFileMatch{Path: jsonlPath}, true
	}
	return grokbotStrictMatch(root, jsonlPath)
}

func grokbotFindFile(root, rawID string) (singleFileMatch, bool) {
	if !IsValidSessionID(rawID) {
		return singleFileMatch{}, false
	}
	jsonlPath := filepath.Join(root, rawID, rawID+".jsonl")
	return grokbotStrictMatch(root, jsonlPath)
}

func grokbotStrictMatch(root, path string) (singleFileMatch, bool) {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return singleFileMatch{}, false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return singleFileMatch{}, false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || !IsValidSessionID(parts[0]) ||
		parts[1] != parts[0]+".jsonl" {
		return singleFileMatch{}, false
	}
	return singleFileMatch{Path: path}, true
}

func grokbotFingerprintSource(src singleFileSource) (SourceFingerprint, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return SourceFingerprint{}, fmt.Errorf("stat %s: %w", src.Path, err)
	}
	if info.IsDir() {
		return SourceFingerprint{}, fmt.Errorf("stat %s: source is a directory", src.Path)
	}
	return SourceFingerprint{
		Size:    info.Size(),
		MTimeNS: info.ModTime().UnixNano(),
	}, nil
}

func grokbotParseFile(
	src singleFileSource, req ParseRequest,
) ([]ParseResult, []string, error) {
	result, err := ParseGrokBotSession(src.Path, req.Machine)
	if err != nil {
		return nil, nil, err
	}
	if req.Fingerprint.Size > 0 {
		result.Session.File.Size = req.Fingerprint.Size
	}
	if req.Fingerprint.MTimeNS > 0 {
		result.Session.File.Mtime = req.Fingerprint.MTimeNS
	}
	if req.Fingerprint.Hash != "" {
		result.Session.File.Hash = req.Fingerprint.Hash
	}
	return []ParseResult{result}, nil, nil
}

func grokbotProviderCapabilities() Capabilities {
	return Capabilities{
		Source: jsonlFileProviderSourceCapabilities(),
		Content: ContentCapabilities{
			FirstMessage:         CapabilitySupported,
			ToolCalls:            CapabilitySupported,
			ToolResults:          CapabilitySupported,
			TerminationStatus:    CapabilityNotApplicable,
			MalformedLineCount:   CapabilityNotApplicable,
			AggregateUsageEvents: CapabilityNotApplicable,
		},
	}
}
