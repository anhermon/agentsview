# Dogfood Notes for AgentsView Fork (anhermon/agentsview)

This fork includes Grok Bot / Sand desktop agent support and related fixes.

## Known Behavior: Sync Count Display

When sync completes, the reported "synced" count may differ from the footer session count:

- **Sync completion**: Reports total sessions written, including fork sessions
- **Footer count**: Shows only root sessions (excludes forks and subagents)

**Example**: Sync reports "15 sessions synced" but footer increases by 12. This happens when 3 fork sessions were included in the sync.

**Why**: Fork sessions are internal relationships (branches within a single session file) that users don't interact with directly. The footer shows the user-visible count.

**Verification**: Check the session list - you'll see the root sessions, not the forks.

## Daemon Restart

After configuration changes or installing a new build:

```bash
agentsview daemon restart
```

**Verification**:

```bash
# Check daemon is running
agentsview daemon status

# Verify sync is working (watch for new sessions)
tail -f ~/.agentsview/logs/agentsview.log  # or check UI footer
```

## Testing Changes

After code changes:

1. Rebuild: `make build`
2. Stop daemon: `agentsview daemon stop`
3. Start daemon: `agentsview daemon start` (or use installed binary)
4. Verify: Check UI at http://127.0.0.1:8080

## Frontend Asset Rebuild

If frontend changes were made:

```bash
cd frontend
npm run build
# Built assets are in frontend/dist
# Server serves them when built
```

The `make build` command includes the frontend build step automatically.
