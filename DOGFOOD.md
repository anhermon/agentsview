# Dogfood Notes for AgentsView Fork (anhermon/agentsview)

This fork includes Grok Bot / Sand desktop agent support and related fixes.

## Fixed: Sync Count Display

**Status**: Fixed in this PR.

Previously, sync completion reported "15 sessions synced" but the footer showed only "1 session, 153 messages" because the footer stats excluded subagents and forks. Now the footer includes all synced sessions to match the sync report.

**Verification**: After sync completes, the footer session count will match the "sessions synced" count (including subagents/forks). Use `session list --include-children` to see the full tree.

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
