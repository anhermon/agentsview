# Scalable Activity Reports

## Goal

Make large Activity reports finish without request timeouts, raw interval
transport, or a retained object per adjacent message pair, while showing useful
progress in the web UI and CLI and preserving the existing report semantics
across SQLite, PostgreSQL, and DuckDB. The exact usage-dedup working-set limit
is called out separately below.

The concrete failure to remove is a month-sized report whose current request
times out after 30 seconds and can materialize hundreds of megabytes of raw
interval JSON. The design must continue to work for archives with roughly
500,000 sessions and millions of messages.

## Contract Summary

- Activity report schema version 6 removes the raw interval list from the
  response and returns a bounded first page of session rows.
- The report route supports both plain JSON and server-sent progress events by
  `Accept` negotiation on the existing URL.
- The server aggregates ordered interval candidates in one pass and never stores
  one object per adjacent message pair.
- Session drill-down is server-side, cursor-paginated, and backed by a bounded
  cache that is only a performance layer.
- A signed, self-describing `report_id` lets a cache miss, eviction, overflow,
  or daemon restart use the same stateless recomputation path.
- The UI never combines a summary from one report generation with session rows
  from another.
- Daemon and direct-database CLI backends expose the same report and paging
  operations.

## Computation Pipeline

### Ordered candidate streams

Each storage backend emits adjacent-message-pair candidates using mechanical
ordinal successor lookups. The query joins only the identifiers, timestamps,
closing role/model, and ordinal-derived prior-model context needed by the shared
aggregator. It does not apply Activity range, cap, duration, or closing-model
selection semantics.

The candidate stream is ordered by candidate start time. Clipping the start to
the report boundary preserves that order because `max(start, boundary)` is
monotone. The SQL layer must not apply the gap cap, discard non-positive gaps,
clip to the report range, or choose the closing-message model. Those rules
remain in `internal/activity` so all three stores use one behavioral
implementation.

Candidate timestamps are normalized to microsecond precision on ingest in the
shared aggregator. SQLite can retain finer timestamps than the PostgreSQL and
DuckDB mirrors; normalizing before any duration, sweep, or membership logic
prevents backend drift after raw intervals stop being serialized. Differential
fixtures use microsecond timestamps, and any deliberately finer fixture compares
durations with a sub-microsecond tolerance.

Every row scan and aggregation loop checks `context.Context`. The ordered SQL
query may require an external sort over the full candidate set, so cancellation
must reach both the database call and the row-processing loop.

### Bounded pairing scan

The backend bounds candidate-start rows to
`[range_start - GapCapSeconds, calculation_end)`, where `calculation_end` is
`range_end` for a complete report and `effective_end` for a partial report. A
start before the left bound cannot produce activity inside the report after the
shared gap cap is applied. The SQL query may use `GapCapSeconds` to derive this
safe pruning bound; this does not move gap-cap semantics out of the shared
aggregator.

The timestamp predicate must not be applied to the same row set on which a
window function establishes adjacency. Window functions run after `WHERE`, so
that shape would remove out-of-range messages before pairing and silently join
messages that are not adjacent by ordinal. Instead, each bounded candidate-start
row resolves its true next timestamped message by ordinal, using an indexed
successor lookup or an equivalent query shape that preserves full-session
adjacency.

The successor lookup has no right timestamp bound. A start just before
`calculation_end` must distinguish a real last message, which produces no
velocity interval, from a message whose successor lies beyond the boundary,
which produces an interval clipped at the boundary. The successor's role and
model are also retained so closing-message model attribution remains shared and
correct. Tests cover pairs straddling both pruning edges and ordinal sequences
whose timestamps are not monotone.

The prior-model context is also derived before candidate-start pruning by
walking valid timestamped predecessors in ordinal order. This preserves the v5
fallback even when the model-setting pair starts before the left scan bound or
timestamps are not monotone; the shared aggregator still decides whether the
closing assistant model or that context owns the interval.

SQLite uses a timestamp-first partial index for the coarse physical scan. A
14-hour text padding on each side covers every RFC3339 timezone offset and mixed
whole/fractional-second representation; the registered timestamp parser then
applies the exact instant bounds. PostgreSQL uses the equivalent timestamp-first
partial index directly. Successor lookups remain backed by the session/ordinal
index. A read-only legacy SQLite archive without the new index falls back to the
correct session-indexed query until it is next opened writable and the
idempotent index migration runs.

### Single-pass aggregation

The shared aggregator consumes candidates chronologically and keeps only:

- bounded bucket accumulators;
- a heap of active interval end times for concurrency;
- per-session aggregate rows;
- one membership bitmap per session, sized to the actual bucket count; and
- the existing bounded model, agent, and project breakdowns.

For each accepted interval it:

1. applies range clipping, the gap cap, non-positive-gap rejection, and
   closing-message model attribution;
1. updates the session and report totals;
1. folds minutes into every overlapped bucket; and
1. updates sweep-line concurrency and membership state.

The bucket fold is a loop over overlapped buckets. Today the minimum bucket
width is at least the default gap cap, so an interval normally touches at most
two buckets, but control flow must not hard-code that fact. A test asserts the
current invariant while the loop remains correct if `GapCapSeconds` becomes
configurable or smaller buckets are added.

At a bucket boundary, peak concurrency begins with intervals carried across the
boundary: expire ended intervals first, then measure the remaining heap, then
process starts inside the bucket. This preserves peaks for intervals that
started in an earlier bucket.

The usage stream performs survivor selection once and reuses the survivors for
both totals and per-session allocation. Its two-tier deduplication and cost
allocation remain identical to the Usage dashboard contract.

This stage has an explicit limit: exact first-seen and complete-snapshot dedup
retains the report-range candidate/survivor set, so its worst case remains
linear in mostly unique usage rows. The implementation bounds that set to the
selected sessions, padded report range, and required Claude snapshot peers and
eliminates duplicate selection/allocation passes, but it does not claim a strict
message-independent usage-memory bound. A future strict bound requires a
key-ordered external spill/reduction design; it is separate from the raw
interval and transport failure fixed here.

The reporting-export caller in `internal/db/reporting_export.go` receives a
slice-backed adapter for the streaming interface. It continues to consume only
totals, breakdowns, and buckets.

### Two timestamp precision levels

Calculation uses microsecond-normalized timestamps so totals, bucket minutes,
and peaks retain current precision. Bucket membership uses the existing
whole-second frontend behavior in
`frontend/src/lib/components/activity/activeSessions.ts`:

- positive spans overlap half-open slots;
- a sub-second span that serializes to one second collapses to a point; and
- that point belongs only to the half-open slot containing it.

This deliberately preserves an existing edge case: a span that straddles a
bucket boundary only at microsecond precision may contribute to the bucket's
minutes or peak while its second-precision drill-down membership omits it. It is
existing behavior, not a new claim that chart counts and membership use the same
precision.

Usage-only sessions have empty membership bitmaps. They appear in unfiltered
session pages, sorted after timed sessions, and are excluded from
bucket-filtered pages, matching the current client-side drill-down.

## Report and Session APIs

### Summary route and progress

`GET /api/v1/activity/report` remains the single report URL.

- `Accept: text/event-stream` receives throttled progress events followed by a
  completed report event.
- Other clients receive the same completed report as plain JSON.

The route uses the existing streaming route helper, which bypasses the Huma
per-operation timeout for both response forms. Plain JSON therefore also avoids
the current 30-second cap; it does not need a separate legacy route.

Progress reuses the `Phase` and counter shape from `internal/sync/progress.go`
and the 200 ms throttling pattern already used by push progress. Phases cover
candidate scan, usage scan, aggregation/finalization, and cache preparation.
Counters are honest processed-row or completed-session counts; an unknown total
is allowed rather than presenting a fabricated percentage.

`db.Store.GetActivityReport` accepts a progress callback, and SQLite,
PostgreSQL, and DuckDB implement the same callback contract. Cancellation of the
HTTP request stops the scans and aggregation.

Schema v6 contains the bounded summary, buckets, breakdowns, `report_id`, and a
first page of 200 rows generated by the same pagination function as the sessions
endpoint. The endpoint clamps page size to the existing session-list maximum of
500 rows. The first page remains in the JSON field `by_session` so older Go
clients decode it. The report does not contain raw intervals. The first page's
default order is pinned to the intended v5 presentation:

1. agent-minutes descending;
1. sessions without timed activity last; and
1. session ID ascending as the total tie-break.

The explicit tie-break makes the order deterministic. Old CLI binaries do not
validate `schema_version`; they can decode v6 and continue to print the first
five `by_session` rows with the same meaning.

### Session paging

`GET /api/v1/activity/reports/{report_id}/sessions` accepts a signed cursor, a
clamped limit, a total sort choice and direction, and an optional bucket index.
It returns session rows, `next_cursor`, and report-generation metadata. Cursors
bind:

- the report artifact digest;
- sort and direction;
- optional bucket index; and
- the position or total sort key needed to continue.

The default sort above and every alternate sort include session ID as a final
tie-break. Given the same artifact digest, recomputation must produce
byte-identical row content, membership, and ordering. This determinism
substitutes for cached immutability on the fallback path and makes cursors safe
across recomputation.

The embedded first page and endpoint pages call one pagination implementation.

## Self-Describing Report Identity

`report_id` is an opaque, versioned, HMAC-signed token. Its payload contains:

- the resolved filter, range, bucket specification, and timezone;
- Activity schema and aggregation-algorithm versions;
- a coarse source probe captured at build time; and
- a digest of the canonical compact report artifacts.

The existing persisted `cursor_secret` signs the token for all three stores, so
it remains valid across daemon restarts. The token is integrity-protected, not
encrypted; it contains no information that the corresponding report query did
not already send to the server.

Version 6 assumes the compact encoded token remains within practical HTTP
request-target limits for currently accepted filters. Token-length tests cover
the largest supported filter payload. If that assumption stops holding, the
sessions operation must move the token into a request body rather than truncate
the query metadata or add an undeclared stateful stub fallback.

The source probe is a small backend query over scalars such as matching-session
count, maximum relevant session sync marker or last activity, matching usage row
count and maximum usage marker, plus the effective pricing and project identity
revisions used by the report. It is a fast change detector, not a cryptographic
snapshot: count-and-maximum tuples can miss an in-place change.

The artifact digest closes that correctness gap without hashing millions of
source rows. It is computed incrementally over canonical finalized summary
fields, session rows, and membership bitmaps. Its work scales with sessions and
buckets, and its input is already required to match across backends.

On a cache miss:

1. decode and validate the token;
1. run the cheap source probe;
1. recompute from the embedded resolved query when the cache cannot serve the
   page; and
1. compare the recomputed artifact digest before applying the old cursor.

If the digest matches, the requested deterministic page is safe. If it differs,
the response carries the newly recomputed bounded summary and first page with a
new `report_id`; the UI atomically replaces the displayed generation instead of
combining old summary and new rows. A probe mismatch may select this refresh
path early, but correctness does not depend on the probe having no false
negatives.

Overflow, size eviction, expiry, and daemon restart therefore converge on one
miss path. The server keeps no metadata stubs for evicted reports.

Concurrent builds are deduplicated with singleflight keyed by the canonical
resolved query and current source probe. Two tabs share the same scan and cache
result instead of consuming two cache entries.

## Bounded Cache

The cache stores compact report artifacts only:

- finalized `SessionRow` values with interned repeated strings;
- membership bitmaps sized to the report's actual bucket count;
- the bounded summary and first page; and
- lazily computed sort permutations.

It never stores raw intervals, messages, activity events, or usage rows.

Eviction considers all of these limits:

- 15-minute idle expiry;
- at most three entries;
- at most 750,000 cached session rows; and
- at most 256 MiB of accounted artifact memory.

The accounting includes bitmap capacity, strings, row storage, maps, and lazy
sort permutations. The implementation documents its estimation method and tests
eviction at each bound.

Idle expiry is intentionally sliding: a successful report or page access
refreshes the entry's last-access time, while an abandoned report expires 15
minutes after its final access. Cache tests use idle-expiry terminology and a
controllable clock so activity, not build time, governs expiration.

If one report cannot fit, its summary and embedded first page still complete,
but the artifacts are not retained. Later pages use the stateless miss path. The
cache is only an accelerator, so cache pressure cannot turn a valid report into
a permanent error.

Sync completion continues to mark Activity data stale without automatically
rebuilding an open dashboard. Progress events are scoped to an explicit report
or page request and do not become a new sync-triggered refresh path.

## UI Behavior

The Activity page opens the report as an SSE request and shows the current
localized phase and counters. It retains a determinate percentage only when a
real total is known. Report completion replaces the progress view with the
summary and embedded first page.

Bucket selection, sorting, and pagination become asynchronous server requests.
The table shows a local loading state while preserving the current rows. Each
request owns an `AbortController`; a report-filter change, a new sort, a new
bucket, or component teardown cancels the obsolete request. `slotFilter` remains
page-local and is cleared when the report generation changes.

If a page response contains a refreshed report generation, the page swaps the
summary, chart, filters, and first page together. It does not silently retain a
chart built from older data.

Sync staleness behavior remains unchanged: the page can display that newer
archive data exists, but it does not reaggregate until the user explicitly
refreshes.

All new status, error, empty, and paging copy uses Paraglide messages and
locale-aware number formatting.

## CLI Behavior

The CLI's `archiveQueryBackend` gains both report and paginated-session
operations. The daemon implementation uses the HTTP routes; the direct SQLite
implementation invokes the same service/store operations with a slice or row
stream adapter. This keeps offline and daemon behavior in lockstep.

Human output retains the existing summary and first five default session rows.
Progress is written to stderr, never stdout. JSON stdout remains a valid single
JSON document and contains the bounded v6 report plus its embedded first page.

The paging flags are `--sessions-limit`, `--sessions-cursor`, `--sessions-sort`,
`--sessions-direction`, and `--sessions-bucket`. The limit defaults to 200 and
clamps at 500. They work in both daemon and direct-database modes. A script can
follow `next_cursor` without asking the CLI to materialize every session in one
process.

## Compatibility and Failure Semantics

- Existing plain-JSON clients keep using the report URL.
- Existing CLI binaries decode v6 because removed fields are optional during Go
  JSON decoding and the first page keeps the default presentation order.
- Invalid or tampered report IDs and cursors return the normal invalid-cursor
  error shape.
- Cancellation is not cached as a failed build; another caller may start or join
  a later build.
- A backend error terminates SSE with a structured error event and returns the
  corresponding structured JSON error for non-streaming clients.
- Reporting export remains source-compatible through its slice-backed adapter.

## Verification

Implementation keeps the old and new aggregators together temporarily. A
differential suite runs fixed and randomized fixtures through both and compares
all v5-observable output except the removed raw intervals. It specifically
covers bucket-boundary concurrency, sub-second membership, usage-only sessions,
gap clipping, closing-model attribution, and the existing usage dedup rules. The
old path is removed only after this suite passes.

Backend parity tests compare the ordered candidate streams themselves before
comparing final reports, session pages, membership bitmaps, and cursors. Each
backend's SQL stream is also compared with the Go slice-backed adapter's pairing
of the same complete fixture, making that adapter the single reference
implementation rather than merely proving that three SQL queries agree. The
fixture includes adjacent pairs straddling both scan bounds. A candidate-level
diff makes SQL pairing, pruning, or timestamp drift local and visible.

Additional behavioral tests cover:

- SSE phases, throttling, completion, errors, and client cancellation;
- plain JSON negotiation on the same timeout-free route;
- context cancellation during database row scans;
- first-page and endpoint pagination identity;
- deterministic sorts and cursors across stateless recomputation;
- artifact mismatch causing an atomic report refresh;
- token validation across a simulated daemon restart with the same persisted
  cursor secret;
- singleflight sharing and cancellation behavior;
- all cache size, row, entry, expiry, and overflow limits;
- frontend loading, request cancellation, bucket filtering, sorting, and
  generation replacement;
- daemon/direct-DB CLI lockstep and clean JSON stdout; and
- reporting-export equivalence.

The existing Activity route cancellation test remains and is adapted to the
streaming route. Documentation in `docs/activity.md` describes schema v6,
progress negotiation, bounded JSON, and session paging.

A large synthetic benchmark records peak retained memory, time through each
phase, and response size. It is evidence for the scaling change rather than a
timing assertion in normal CI. The acceptance property is structural: retained
aggregation memory scales with sessions plus buckets, and transport size scales
with the bounded summary plus requested page, not with message count.
