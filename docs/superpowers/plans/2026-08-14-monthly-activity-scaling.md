# Scalable Activity Reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> to implement this plan directly in the current agent, task by task. Never use
> subagent-driven development. Keep the old aggregator available until the
> differential gate passes.

**Goal:** Make large Activity reports complete with bounded transport and
message-independent retained aggregation memory, expose honest progress, and
page session drill-down identically in the UI and CLI.

**Approved spec/design:**
`docs/superpowers/specs/2026-08-14-monthly-activity-scaling-design.md`

**Architecture:** Each store streams mechanically paired, start-ordered interval
candidates and usage rows into one shared aggregator. The aggregator folds
intervals directly into buckets and compact per-session artifacts. A signed
self-describing report token and bounded cache accelerate server-side session
paging without making cache residency part of correctness. The existing report
route negotiates SSE progress or plain JSON.

**Tech Stack:** Go, `database/sql`, SQLite, PostgreSQL, DuckDB, Huma SSE, Svelte
5, TypeScript, Paraglide JS, Vitest, Playwright, Testify.

## Global Constraints

- Preserve v5 observable calculations except the removed interval payload and
  the newly totalized session tie-break.
- Keep pairing SQL mechanical; all Activity semantics remain in
  `internal/activity`.
- Normalize calculation timestamps to microseconds in the shared aggregator.
- Preserve the whole-second membership behavior in `activeSessions.ts`.
- Never materialize all candidate intervals, messages, activity events, or usage
  rows in the completed implementation.
- Check `context.Context` in every long scan and aggregation loop.
- Keep stdout machine-readable in CLI JSON mode; progress goes to stderr.
- Make every new sort total by ending with session ID ascending.

______________________________________________________________________

### Task 1: Freeze v5 Semantics With a Differential Harness

**Files:**

- Modify: `internal/activity/activity.go`
- Test: `internal/activity/activity_test.go`
- Test: `internal/activity/parity_pgtest_test.go`

**Interfaces:**

- Preserve the current implementation as an unexported legacy test seam.

- Define a candidate value and slice-backed candidate iterator for the future
  streaming implementation.

- [ ] **Step 1: Add fixed differential fixtures**

    Cover range clipping, zero/negative gaps, gap capping, closing-message model
    attribution, carried concurrency at bucket boundaries, sub-second spans,
    usage-only sessions, duplicate usage rows, cost allocation, and sessions
    tied on agent minutes. Include adjacent pairs that straddle both
    `[range_start - GapCapSeconds, calculation_end)` scan bounds.

- [ ] **Step 2: Add randomized differential fixtures**

    Generate deterministic microsecond-precision sessions, ordered messages, usage
    rows, and bucket specifications. Compare totals, buckets, breakdowns, and
    session rows. Exclude only serialized raw intervals. Include a separate
    finer-than-microsecond fixture with an explicit duration tolerance.

- [ ] **Step 3: Run the focused suite and verify it passes against the legacy
  seam**

    ```bash
    go test ./internal/activity -run 'TestAggregate(Differential|Randomized)' -count=1
    ```

    Expected: PASS, establishing an executable baseline before the rewrite.

______________________________________________________________________

### Task 2: Implement the Streaming Aggregator

**Files:**

- Modify: `internal/activity/activity.go`
- Modify: `internal/activity/query.go`
- Test: `internal/activity/activity_test.go`

**Interfaces:**

- Consumes context-aware candidate and usage iterators plus resolved `Params`.

- Produces the v6 bounded summary and compact session artifacts, including
  membership bitmaps.

- [ ] **Step 1: Write failing sweep and membership tests**

    Assert direct bucket folding over all overlaps, concurrency carried across a
    bucket boundary, context cancellation, microsecond calculation precision,
    and the complete half-open whole-second membership rules copied from
    `activeSessions.ts`.

- [ ] **Step 2: Write the current bucket/gap invariant test**

    Assert that built-in bucket widths are currently at least the default gap cap,
    while the behavior test uses a wider configurable gap to prove the fold
    loops over more than two buckets.

- [ ] **Step 3: Run focused tests and verify RED**

    ```bash
    go test ./internal/activity \
      -run 'TestStreamingAggregate|TestActivityBucketGapInvariant' -count=1
    ```

    Expected: FAIL because the streaming aggregator and membership artifacts do
    not exist.

- [ ] **Step 4: Implement single-pass candidate aggregation**

    Normalize candidate timestamps to microseconds, apply shared semantics, fold
    duration into overlapped buckets, maintain the active-end heap, and update
    per-session rows and bitmaps. Do not append to `Report.Intervals`.

- [ ] **Step 5: Consolidate usage survivor selection**

    Run the existing two-tier dedup rules once, then feed the survivors to totals
    and session cost allocation. Preserve exact Usage-dashboard equivalence.

- [ ] **Step 6: Run focused and differential tests and verify GREEN**

    ```bash
    go test ./internal/activity -count=1
    ```

    Expected: PASS for both targeted streaming behavior and old/new differential
    comparisons.

______________________________________________________________________

### Task 3: Stream Candidate and Usage Rows From All Stores

**Files:**

- Modify: `internal/db/store.go`
- Modify: `internal/db/activityreport.go`
- Modify: `internal/postgres/activityreport.go`
- Modify: `internal/duckdb/activityreport.go`
- Test: `internal/db/activityreport_test.go`
- Test: `internal/postgres/activityreport_pgtest_test.go`
- Test: `internal/duckdb/activityreport_test.go`
- Test: `internal/activity/parity_pgtest_test.go`

**Interfaces:**

- Extend `Store.GetActivityReport` with a progress callback.

- Add backend candidate/usage iterator implementations with explicit close and
  error handling.

- [ ] **Step 1: Add candidate-level parity assertions**

    Seed ordinal-vs-timestamp disagreement, clipped starts, microsecond
    timestamps, duplicate usage rows, multiple agents, a predecessor before the
    left pruning bound, and a successor beyond the right bound. Compare each
    normalized backend stream with the Go slice-backed adapter's pairing of the
    complete fixture, then compare SQLite, PostgreSQL, and DuckDB with each
    other before final report comparison.

- [ ] **Step 2: Add row-scan cancellation tests**

    Cancel during candidate and usage scans and assert prompt context errors and
    closed rows for each practical backend seam.

- [ ] **Step 3: Run focused tests and verify RED**

    ```bash
    go test ./internal/db ./internal/duckdb -run ActivityReport -count=1
    go test -tags=pgtest ./internal/postgres ./internal/activity \
      -run ActivityReport -count=1
    ```

    Expected: FAIL until stores provide ordered candidate streams and progress.

- [ ] **Step 4: Implement mechanical pairing queries**

    Bound candidate-start rows to
    `[range_start - GapCapSeconds, calculation_end)`. For each start, resolve
    the true next timestamped message by ordinal through an indexed successor
    lookup or an equivalent query shape. Do not apply the timestamp predicate to
    the same row set used by a window function to establish adjacency, and do
    not right-bound the successor lookup. Emit candidates ordered by start and
    leave clipping, caps, rejection, and model semantics to `internal/activity`.

- [ ] **Step 5: Connect progress and context checks**

    Report processed candidate, usage, and finalized-session counters. Check
    context between rows and during finalization.

- [ ] **Step 6: Run backend and parity tests and verify GREEN**

    Run the commands from Step 3. Expected: PASS, with candidate-level parity
    failures localizing backend SQL differences.

______________________________________________________________________

### Task 4: Preserve Reporting Export and Remove the Legacy Aggregator

**Files:**

- Modify: `internal/db/reporting_export.go`

- Modify: `internal/activity/activity.go`

- Test: `internal/db/reporting_export_test.go`

- Test: `internal/activity/activity_test.go`

- [ ] **Step 1: Add reporting-export equivalence coverage**

    Assert hourly export totals, buckets, and breakdowns are unchanged when the
    source input is adapted from slices.

- [ ] **Step 2: Add the slice-backed adapter**

    Feed the reporting export's in-memory slices through the streaming iterator
    without asking the export path to understand cache or HTTP concepts.

- [ ] **Step 3: Run the differential and export gates**

    ```bash
    go test ./internal/activity ./internal/db \
      -run 'Aggregate|ReportingExport' -count=1
    ```

    Expected: PASS.

- [ ] **Step 4: Delete interval materialization and duplicate usage passes**

    Remove the legacy aggregator only after Step 3 passes. Keep a compact test
    oracle if needed, but no production path may allocate raw intervals.

- [ ] **Step 5: Re-run the gate after deletion**

    Run the command from Step 3. Expected: PASS.

______________________________________________________________________

### Task 5: Build Report Tokens, Artifact Digests, and the Bounded Cache

**Files:**

- Create: `internal/activity/reportartifacts.go`
- Create: `internal/activity/reportartifacts_test.go`
- Create: `internal/server/activity_report_cache.go`
- Create: `internal/server/activity_report_cache_test.go`
- Modify: `internal/db/store.go`
- Modify: `internal/db/activityreport.go`
- Modify: `internal/postgres/activityreport.go`
- Modify: `internal/duckdb/activityreport.go`

**Interfaces:**

- Add `ActivitySourceProbe` and a canonical resolved-query payload.

- Add a signed report-token codec using the existing store cursor secret.

- Add server cache lookup/build operations with singleflight.

- [ ] **Step 1: Write failing token and restart tests**

    Assert canonical round-trip, signature rejection, algorithm/schema version
    rejection, and successful decoding by a new store instance configured with
    the same persisted cursor secret. Cover the largest supported filter token
    and reject an encoded token that exceeds the documented practical URL limit
    without truncating metadata or creating a stateful stub.

- [ ] **Step 2: Write failing probe and artifact-digest tests**

    Mutate sessions, messages, usage, pricing, and project identity and assert
    either the coarse probe or canonical artifact digest changes. Assert
    identical cross-backend artifacts digest identically.

- [ ] **Step 3: Write failing cache-bound tests**

    Use a controllable clock to prove successful access slides the 15-minute idle
    deadline while an abandoned entry expires 15 minutes after its final access.
    Also cover three-entry eviction, 750,000-row eviction, 256 MiB accounting,
    lazy-sort accounting, one-report overflow, and no metadata stub after
    eviction.

- [ ] **Step 4: Write failing singleflight tests**

    Two identical callers must share one build. One caller canceling must not
    cancel a build still needed by another; a fully abandoned build must stop
    and must not cache an error or partial artifact.

- [ ] **Step 5: Implement token, digest, and probe support**

    Sign the self-describing token with the existing cursor secret. Hash only
    canonical finalized artifacts, not raw source rows. Keep the cheap probe an
    optimization rather than the sole correctness boundary.

- [ ] **Step 6: Implement cache accounting and singleflight**

    Keep finalized artifacts and deterministic paging in `internal/activity`.
    Store interned rows, actual-width bitmaps, bounded summary, and accounted
    lazy sort permutations in the server cache. Converge expiry, overflow,
    eviction, and restart on stateless recomputation.

- [ ] **Step 7: Run focused tests and verify GREEN**

    ```bash
    go test ./internal/activity ./internal/db ./internal/duckdb \
      -run 'Report(Cache|Token|Probe|Digest)|ActivityReport' -count=1
    ```

    Expected: PASS.

______________________________________________________________________

### Task 6: Add Deterministic Server-Side Session Paging

**Files:**

- Modify: `internal/activity/reportartifacts.go`

- Modify: `internal/server/activity_report_cache.go`

- Modify: `internal/db/store.go`

- Modify: `internal/service/service.go`

- Create: `internal/server/huma_routes_activity_sessions.go`

- Modify: `internal/server/huma_routes_activity.go`

- Test: `internal/activity/reportartifacts_test.go`

- Test: `internal/server/activity_report_cache_test.go`

- Test: `internal/server/activity_report_test.go`

- [ ] **Step 1: Write failing pagination contract tests**

    Assert clamped limits, default order, every alternate sort's session-ID
    tie-break, bucket filtering, empty usage-only bitmaps, cursor binding, and
    byte-identical embedded/endpoint first pages.

- [ ] **Step 2: Write failing cache-miss generation tests**

    A matching artifact digest must continue at the requested cursor after
    stateless recomputation. A mismatch must discard the old cursor and return a
    new report plus first page atomically.

- [ ] **Step 3: Run focused tests and verify RED**

    ```bash
    go test ./internal/activity ./internal/server \
      -run 'Activity(Session|Report).*Pag|ActivityReportGeneration' -count=1
    ```

- [ ] **Step 4: Implement the shared page function and endpoint**

    Generate embedded and later pages from one function. Validate report token,
    cursor, sort, direction, and bucket. Use the stateless miss path when no
    cache artifact exists.

- [ ] **Step 5: Run focused tests and verify GREEN**

    Run the command from Step 3. Expected: PASS.

______________________________________________________________________

### Task 7: Negotiate SSE Progress and Plain JSON on the Report Route

**Files:**

- Modify: `internal/server/huma_routes_activity.go`

- Modify: `internal/server/huma_routes.go`

- Test: `internal/server/huma_routes_activity_internal_test.go`

- Test: `internal/server/huma_routes_activity_cancellation_internal_test.go`

- Test: `internal/server/activity_report_test.go`

- [ ] **Step 1: Write failing negotiation tests**

    Assert SSE clients receive ordered phase/counter events and one terminal
    report, while ordinary clients receive only the equivalent JSON body from
    the same URL. Prove both forms bypass the Huma operation timeout.

- [ ] **Step 2: Extend cancellation coverage**

    Cancel an SSE request during a row scan and assert the callback and store
    observe cancellation. Assert no completed cache entry remains.

- [ ] **Step 3: Run focused tests and verify RED**

    ```bash
    go test ./internal/server \
      -run 'ActivityReport(Stream|Negotiation|Cancellation|Timeout)' -count=1
    ```

- [ ] **Step 4: Implement the negotiated streaming route**

    Reuse the existing `stream()` helper, sync progress shape, and 200 ms
    throttling sender. Map errors consistently for SSE and JSON.

- [ ] **Step 5: Run focused tests and verify GREEN**

    Run the command from Step 3. Expected: PASS.

______________________________________________________________________

### Task 8: Upgrade the CLI Contract for Progress and Paging

**Files:**

- Modify: `cmd/agentsview/archive_query_backend.go`

- Modify: `cmd/agentsview/activity.go`

- Test: `cmd/agentsview/archive_query_backend_test.go`

- Test: `cmd/agentsview/activity_test.go`

- [ ] **Step 1: Write failing daemon/direct-backend lockstep tests**

    Assert both implementations return the same v6 report, embedded page,
    subsequent page, bucket filter, sort order, and cursor behavior.

- [ ] **Step 2: Write failing output-channel tests**

    Assert progress phases go to stderr and `--json` stdout remains one valid
    bounded JSON document. Assert old human output still prints the first five
    default rows in the pinned order.

- [ ] **Step 3: Write failing paging-flag tests**

    Cover `--sessions-limit` (default 200, maximum 500), `--sessions-cursor`,
    `--sessions-sort`, `--sessions-direction`, and `--sessions-bucket` in both
    daemon and direct-database modes.

- [ ] **Step 4: Run focused tests and verify RED**

    ```bash
    go test ./cmd/agentsview -run 'Activity|ArchiveQueryBackend' -count=1
    ```

- [ ] **Step 5: Implement progress and paginated backend methods**

    Use SSE in daemon mode, callbacks in direct mode, and one renderer/output
    envelope for both. Do not add an unbounded `--all` materialization path.

- [ ] **Step 6: Run focused tests and verify GREEN**

    Run the command from Step 4. Expected: PASS.

______________________________________________________________________

### Task 9: Move the Activity UI to Streamed Reports and Async Pages

**Files:**

- Modify: `frontend/messages/en.json`

- Modify: other locale files under `frontend/messages/`

- Modify: `frontend/src/lib/api/types/activity.ts`

- Modify: `frontend/src/lib/stores/activity.svelte.ts`

- Modify: `frontend/src/lib/components/activity/ActivityPage.svelte`

- Modify: `frontend/src/lib/components/activity/SessionsTable.svelte`

- Remove after migration:
  `frontend/src/lib/components/activity/activeSessions.ts`

- Test: `frontend/src/lib/stores/activity.test.ts`

- Test: `frontend/src/lib/components/activity/ActivityPage.test.ts`

- Test: `frontend/src/lib/components/activity/SessionsTable.test.ts`

- Test: `frontend/src/lib/components/activity/activeSessions.test.ts`

- [ ] **Step 1: Add localized progress and paging messages**

    Add phase, loading, refresh, empty, and paging strings with Paraglide. Keep
    locale coverage and generated message usage consistent with the frontend
    guide.

- [ ] **Step 2: Write failing store tests**

    Simulate SSE phase updates, terminal report, structured failure, request
    cancellation, and stale sync state that does not auto-reload.

- [ ] **Step 3: Write failing component tests**

    Cover loading without clearing current rows, cancellation on filter/sort/
    bucket changes, async bucket membership, network sorting, pagination, and an
    atomic summary/table replacement on report-generation change.

- [ ] **Step 4: Run focused frontend tests and verify RED**

    ```bash
    pnpm --dir frontend test -- --run \
      src/lib/stores/activity.test.ts \
      src/lib/components/activity/ActivityPage.test.ts \
      src/lib/components/activity/SessionsTable.test.ts
    ```

- [ ] **Step 5: Implement the streamed report store**

    Parse progress and completion events, retain explicit stale state, and own
    abort controllers for the report and page requests.

- [ ] **Step 6: Implement async table interactions**

    Start with the embedded first page. Route bucket, sort, and pagination through
    the session endpoint. Clear page-local slot state when the report ID changes
    and atomically adopt refreshed reports.

- [ ] **Step 7: Remove client interval filtering**

    Delete `activeSessions.ts` only after the shared Go membership tests and UI
    endpoint tests cover its behavioral rules. Remove raw interval API types.

- [ ] **Step 8: Run frontend checks and verify GREEN**

    ```bash
    pnpm --dir frontend test -- --run
    pnpm --dir frontend check
    ```

    Expected: PASS.

______________________________________________________________________

### Task 10: Version, Document, Benchmark, and Verify the Whole Change

**Files:**

- Modify: `internal/export/types.go`

- Modify: generated frontend API models through the repository generator

- Modify: `docs/activity.md`

- Add or modify: Activity benchmark/test files in `internal/activity/`

- [ ] **Step 1: Bump Activity schema version from 5 to 6**

    Remove raw intervals from the documented/generated report contract and add
    report identity, first-page, cursor, and progress-event types.

- [ ] **Step 2: Update Activity documentation**

    Describe the bounded v6 JSON body, Accept negotiation, progress phases,
    session paging, cache-miss refresh semantics, direct/daemon CLI parity, and
    old-client compatibility boundary.

- [ ] **Step 3: Add a large synthetic benchmark**

    Generate many messages per session and record allocations, retained artifacts,
    phase time, and encoded response size. Keep wall-clock numbers
    informational; assert structural bounds and absence of raw intervals.

- [ ] **Step 4: Run focused package suites**

    ```bash
    go test ./internal/activity ./internal/db ./internal/duckdb ./internal/server ./cmd/agentsview -count=1
    go test -tags=pgtest ./internal/postgres ./internal/activity -count=1
    pnpm --dir frontend test -- --run
    pnpm --dir frontend check
    ```

- [ ] **Step 5: Run repository-required Go verification**

    ```bash
    go fmt ./...
    go vet ./...
    ```

    Re-run any package tests changed by formatting or generated-code updates.

- [ ] **Step 6: Review the final diff and benchmark evidence**

    Confirm aggregation-retained memory scales with sessions plus buckets,
    response size is bounded by summary plus requested page, all three backends
    pass parity, and no unrelated files or private data entered the diff.
