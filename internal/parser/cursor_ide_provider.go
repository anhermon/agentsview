package parser

import (
	"context"
	"os"
	"path/filepath"
)

// Cursor IDE stores every chat session in one shared SQLite database
// (state.vscdb). It is a multi-session container provider: discovery
// surfaces the database as a single source and Parse fans it out into one
// session per composer, addressed by "<db>#<composerID>" virtual paths. All
// behavior is wired into the shared multi-session-container base via
// options, mirroring the Zed provider.
func newCursorIDEProviderFactory(def AgentDef) ProviderFactory {
	return NewMultiSessionProviderFactory(
		def,
		cursorIDEProviderCapabilities(),
		func(cfg ProviderConfig) multiSessionContainerSourceSet {
			return NewMultiSessionContainerSourceSet(
				AgentCursorIDE,
				cfg.Roots,
				WithContainerDiscovery(cursorIDEDiscoverContainers),
				WithWatchRoots(cursorIDEWatchRoots),
				WithChangedPathClassifier(cursorIDEClassifyPath),
				WithMemberLookup(cursorIDEFindMember),
				WithContextFingerprint(cursorIDEFingerprintSource),
				WithContextContainerParse(cursorIDEParseContainer),
				WithContextMemberParse(cursorIDEParseMember),
				WithMemberPresence(cursorIDEMemberPresent),
			)
		},
	)
}

func cursorIDEDiscoverContainers(root string) []string {
	if root == "" {
		return nil
	}
	path := filepath.Join(root, CursorIDEDBRelPath)
	if !IsRegularFile(path) {
		return nil
	}
	return []string{path}
}

// cursorIDEWatchRoots watches the provider root directly: state.vscdb sits
// straight inside it, with no subdirectory like Zed's threads/threads.db.
func cursorIDEWatchRoots(roots []string) []WatchRoot {
	out := make([]WatchRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, WatchRoot{
			Path:         root,
			Recursive:    false,
			IncludeGlobs: []string{CursorIDEDBRelPath, CursorIDEDBRelPath + "-*"},
			DebounceKey:  string(AgentCursorIDE) + ":state:" + root,
		})
	}
	return out
}

// cursorIDEClassifyPath maps a stored or changed path to its database
// container and composer. allowMissing relaxes the regular-file check so a
// database delete (or its WAL/SHM sibling) still classifies for tombstones.
func cursorIDEClassifyPath(
	root, path string, allowMissing bool,
) (multiSessionMatch, bool) {
	return classifySQLiteContainerPath(
		root, path, CursorIDEDBRelPath, allowMissing, false,
		parseCursorIDEVirtualPath,
	)
}

func cursorIDEFindMember(root, rawID string) (multiSessionMatch, bool) {
	if root == "" || !IsValidSessionID(rawID) {
		return multiSessionMatch{}, false
	}
	dbPath := filepath.Join(root, CursorIDEDBRelPath)
	if !CursorIDEComposerExists(dbPath, rawID) {
		return multiSessionMatch{}, false
	}
	return multiSessionMatch{
		Path:      VirtualSourcePath(dbPath, rawID),
		Container: dbPath,
		MemberID:  rawID,
	}, true
}

// cursorIDEFingerprintSource returns the composite whole-database mtime for a
// container source, and a cheap per-composer digest (lastUpdatedAt + header
// count) for a member source. Neither reads or hashes the database's full
// contents: state.vscdb is 86MB+ locally and 500MB+ has been reported in the
// wild, and Cursor keeps the file open and mutating, so a full-file hash on
// every fingerprint pass is both slow and would defeat mtime-based freshness
// by always looking "changed".
func cursorIDEFingerprintSource(
	ctx context.Context, src multiSessionSource,
) (SourceFingerprint, error) {
	info, err := os.Stat(src.Container)
	if err != nil {
		if os.IsNotExist(err) {
			return SourceFingerprint{}, nil
		}
		return SourceFingerprint{}, err
	}
	if src.MemberID == "" {
		mtime := info.ModTime().UnixNano()
		if composite, err := sqliteDBCompositeMtime(
			src.Container, cursorIDEDBMtimeSuffixes,
		); err == nil {
			mtime = composite
		}
		return SourceFingerprint{Size: info.Size(), MTimeNS: mtime}, nil
	}

	conn, err := openCursorIDEDB(src.Container)
	if err != nil {
		return SourceFingerprint{}, err
	}
	defer conn.Close()
	meta, ok, err := loadCursorIDEComposerMeta(ctx, conn, src.MemberID)
	if err != nil {
		return SourceFingerprint{}, err
	}
	if !ok {
		// Composer row is gone but the DB file remains: a keyed-empty
		// fingerprint without error so the engine proceeds to Parse, which
		// force-replaces the deleted composer out of the archive.
		return SourceFingerprint{}, nil
	}
	return SourceFingerprint{
		Size:    info.Size(),
		MTimeNS: cursorIDETime(meta.lastUpdatedAt).UnixNano(),
		Hash:    meta.fingerprint(),
	}, nil
}

// cursorIDEDBMtimeSuffixes omits "-shm": Cursor IDE keeps its own read/write
// connection open against state.vscdb, which continually touches the
// shared-memory file, so including it would make every scan trigger the next
// one (the same reasoning as omnigentDBMtimeSuffixes).
var cursorIDEDBMtimeSuffixes = []string{"", "-wal"}

func cursorIDEMemberPresent(src multiSessionSource) bool {
	if src.MemberID == "" {
		return IsRegularFile(src.Container)
	}
	return CursorIDEComposerExists(src.Container, src.MemberID)
}

func cursorIDEParseMember(
	ctx context.Context, src multiSessionSource, req ParseRequest,
) (*ParseResult, error) {
	dbInfo, err := os.Stat(src.Container)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !IsValidSessionID(src.MemberID) {
		return nil, nil
	}
	conn, err := openCursorIDEDB(src.Container)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return parseCursorIDEComposer(
		ctx, conn, src.Container, src.MemberID, req.Machine, dbInfo,
	)
}

func cursorIDEParseContainer(
	ctx context.Context, src multiSessionSource, req ParseRequest,
) ([]ParseResult, error) {
	dbInfo, err := os.Stat(src.Container)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	conn, err := openCursorIDEDB(src.Container)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	ids, err := listCursorIDEComposerIDs(ctx, conn)
	if err != nil {
		return nil, err
	}
	results := make([]ParseResult, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := parseCursorIDEComposer(
			ctx, conn, src.Container, id, req.Machine, dbInfo,
		)
		if err != nil {
			return nil, err
		}
		if result == nil {
			continue
		}
		results = append(results, *result)
	}
	return results, nil
}

// parseCursorIDEVirtualPath splits a Cursor IDE virtual source path into its
// physical state.vscdb path and raw composer ID.
func parseCursorIDEVirtualPath(path string) (string, string, bool) {
	dbPath, composerID, ok := ParseVirtualSourcePathForBase(path, CursorIDEDBRelPath)
	if !ok || !IsValidSessionID(composerID) {
		return "", "", false
	}
	return dbPath, composerID, true
}

func cursorIDEProviderCapabilities() Capabilities {
	source := multiSessionContainerSourceCapabilities(
		CapabilitySupported,
		CapabilityUnsupported,
	)
	source.PersistentArchive = CapabilitySupported
	return Capabilities{
		Source: source,
		Content: ContentCapabilities{
			FirstMessage: CapabilitySupported,
			SessionName:  CapabilitySupported,
			Cwd:          CapabilitySupported,
			GitBranch:    CapabilitySupported,
			ToolCalls:    CapabilitySupported,
			ToolResults:  CapabilitySupported,
		},
		Sync: ProviderSyncSemantics{
			UnchangedResults: UnchangedResultMTime,
		},
	}
}
