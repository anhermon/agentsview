<script lang="ts">
  import { onMount } from "svelte";
  import { Button, Chip, EmptyState, Typeahead, type TypeaheadOption } from "@kenn-io/kit-ui";
  import { ChevronLeftIcon } from "../../icons.js";
  import { fetchTaskMetrics } from "../../api/tasks.js";
  import type { TaskMetricFilters, TaskMetrics } from "../../api/types/tasks.js";
  import { m } from "../../i18n/index.js";
  import { router } from "../../stores/router.svelte.js";
  import { formatDuration } from "../../utils/duration.js";
  import RangePicker from "../shared/RangePicker.svelte";
  import { resolveRange, type RangeSelection } from "../shared/rangeSelection.js";
  import { localDateStr } from "../shared/dateRangeSelector.js";

  let metrics = $state<TaskMetrics | null>(null);
  let loading = $state(true);
  let error: string | null = $state(null);
  let project = $state(router.params.project ?? "");
  let status = $state(router.params.status ?? "");
  let phase = $state(router.params.phase ?? "");
  let type = $state(router.params.type ?? "");
  let assignee = $state(router.params.assignee ?? "");
  let range: RangeSelection = $state(initialRange());
  const emptyCounts: TaskMetrics["counts"] = {
    by_project: {}, by_status: {}, by_phase: {}, by_type: {}, by_assignee: {},
  };
  const counts = $derived(metrics ? metrics.counts : emptyCounts);

  function initialRange(): RangeSelection {
    if (router.params.from && router.params.to) {
      return { mode: "custom", from: router.params.from, to: router.params.to };
    }
    return { mode: "relative", days: 30 };
  }

  function optionList(values: string[], allLabel: string): TypeaheadOption[] {
    return [
      { name: "", label: allLabel, displayLabel: allLabel },
      ...[...new Set(values.filter(Boolean))].sort().map((value) => ({ name: value, label: value, displayLabel: value })),
    ];
  }

  const projectOptions = $derived(optionList([...Object.keys(counts.by_project), project], m.tasks_metrics_all_projects()));
  const statusOptions = $derived(optionList([...Object.keys(counts.by_status), status, "Backlog", "Ready", "In Progress", "Blocked", "Review", "Done"], m.tasks_metrics_all_statuses()));
  const phaseOptions = $derived(optionList([...Object.keys(counts.by_phase), phase, "Understand", "Plan", "Execute", "Verify", "Deliver"], m.tasks_metrics_all_phases()));
  const typeOptions = $derived(optionList([...Object.keys(counts.by_type), type], m.tasks_metrics_all_types()));
  const assigneeOptions = $derived(optionList([...Object.keys(counts.by_assignee), assignee], m.tasks_metrics_all_assignees()));

  const distributions = $derived([
    { id: "status", label: m.tasks_metrics_by_status(), values: counts.by_status },
    { id: "phase", label: m.tasks_metrics_by_phase(), values: counts.by_phase },
    { id: "type", label: m.tasks_metrics_by_type(), values: counts.by_type },
    { id: "project", label: m.tasks_metrics_by_project(), values: counts.by_project },
    { id: "assignee", label: m.tasks_metrics_by_assignee(), values: counts.by_assignee },
  ]);

  function dateToISO(date: string, exclusive = false): string {
    const value = new Date(`${date}T00:00:00`);
    if (exclusive) value.setDate(value.getDate() + 1);
    return value.toISOString();
  }

  function filters(): TaskMetricFilters {
    const params: TaskMetricFilters = { project, status, phase, type, assignee };
    if (router.params.from) params.from = dateToISO(router.params.from);
    if (router.params.to) params.to = dateToISO(router.params.to, true);
    return params;
  }

  function syncURL(): void {
    const params: Record<string, string> = {};
    for (const [key, value] of Object.entries({ project, status, phase, type, assignee })) {
      if (value) params[key] = value;
    }
    const resolved = resolveRange(range);
    if (!(range.mode === "relative" && range.days === 0)) {
      params.from = resolved.from;
      params.to = resolved.to;
    }
    router.replaceParams(params);
  }

  async function load(): Promise<void> {
    loading = true;
    error = null;
    try {
      metrics = await fetchTaskMetrics(filters());
    } catch (cause) {
      error = cause instanceof Error ? cause.message : m.tasks_metrics_load_failed();
    } finally {
      loading = false;
    }
  }

  function applyFilter(key: "project" | "status" | "phase" | "type" | "assignee", value: string): void {
    if (key === "project") project = value;
    else if (key === "status") status = value;
    else if (key === "phase") phase = value;
    else if (key === "type") type = value;
    else assignee = value;
    syncURL();
    void load();
  }

  function applyRange(selection: RangeSelection): void {
    range = selection;
    syncURL();
    void load();
  }

  function clearFilters(): void {
    project = "";
    status = "";
    phase = "";
    type = "";
    assignee = "";
    range = { mode: "relative", days: 0 };
    router.replaceParams({});
    void load();
  }

  function hasFilters(): boolean {
    return Boolean(project || status || phase || type || assignee || !(range.mode === "relative" && range.days === 0));
  }

  function maximum(values: Record<string, number>): number {
    return Math.max(1, ...Object.values(values));
  }

  onMount(() => {
    syncURL();
    void load();
  });
</script>

<div class="metrics-page">
  <header class="metrics-header">
    <div>
      <Button label={m.tasks_back_to_board()} size="sm" onclick={() => router.navigate("tasks")}>
        <ChevronLeftIcon size="14" aria-hidden="true" />
      </Button>
      <span aria-hidden="true"></span>
      <h1>{m.tasks_metrics_title()}</h1>
      <p>{m.tasks_metrics_description()}</p>
    </div>
  </header>

  <section class="filters" aria-label={m.tasks_metrics_filters()}>
    <div class="filter-grid">
      <Typeahead options={projectOptions} value={project} fallbackLabel={project || m.tasks_metrics_all_projects()} placeholder={m.tasks_metrics_project()} title={m.tasks_metrics_project()} emptyLabel={m.tasks_no_projects()} onselect={(value) => applyFilter("project", value)} />
      <Typeahead options={statusOptions} value={status} fallbackLabel={status || m.tasks_metrics_all_statuses()} placeholder={m.tasks_metrics_status()} title={m.tasks_metrics_status()} emptyLabel={m.tasks_metrics_no_options()} onselect={(value) => applyFilter("status", value)} />
      <Typeahead options={phaseOptions} value={phase} fallbackLabel={phase || m.tasks_metrics_all_phases()} placeholder={m.tasks_metrics_phase()} title={m.tasks_metrics_phase()} emptyLabel={m.tasks_metrics_no_options()} onselect={(value) => applyFilter("phase", value)} />
      <Typeahead options={typeOptions} value={type} fallbackLabel={type || m.tasks_metrics_all_types()} placeholder={m.tasks_metrics_type()} title={m.tasks_metrics_type()} emptyLabel={m.tasks_metrics_no_options()} onselect={(value) => applyFilter("type", value)} />
      <Typeahead options={assigneeOptions} value={assignee} fallbackLabel={assignee || m.tasks_metrics_all_assignees()} placeholder={m.tasks_metrics_assignee()} title={m.tasks_metrics_assignee()} emptyLabel={m.tasks_no_agents()} onselect={(value) => applyFilter("assignee", value)} />
      <RangePicker selection={range} onSelect={applyRange} maxDate={localDateStr(new Date())} align="right" />
    </div>
    <Button label={m.tasks_metrics_clear()} size="sm" disabled={!hasFilters()} onclick={clearFilters} />
  </section>

  {#if loading}
    <div class="metrics-loading" aria-label={m.tasks_metrics_loading()}>
      <span></span><span></span><span></span><span></span>
    </div>
  {:else if error}
    <EmptyState title={m.tasks_metrics_load_failed()} description={error}>
      <Button label={m.tasks_retry()} tone="info" surface="solid" onclick={load} />
    </EmptyState>
  {:else if !metrics || metrics.total_tasks === 0}
    <EmptyState title={m.tasks_metrics_empty()} description={m.tasks_metrics_empty_description()}>
      {#if hasFilters()}<Button label={m.tasks_metrics_clear()} tone="info" surface="solid" onclick={clearFilters} />{/if}
    </EmptyState>
  {:else}
    <div class="metrics-content">
      <section class="summary-strip" aria-label={m.tasks_metrics_summary()}>
        <div><span>{m.tasks_metrics_tasks()}</span><strong>{metrics.total_tasks.toLocaleString()}</strong></div>
        <div><span>{m.tasks_lead_time()}</span><strong>{formatDuration(metrics.timing.lead_time.average_ms)}</strong><small>{m.tasks_metrics_samples({ count: metrics.timing.lead_time.samples, countLabel: String(metrics.timing.lead_time.samples) })}</small></div>
        <div><span>{m.tasks_cycle_time()}</span><strong>{formatDuration(metrics.timing.cycle_time.average_ms)}</strong><small>{m.tasks_metrics_samples({ count: metrics.timing.cycle_time.samples, countLabel: String(metrics.timing.cycle_time.samples) })}</small></div>
        <div><span>{m.tasks_completion_ready()}</span><strong>{metrics.gates.completion_ready_tasks.toLocaleString()}</strong><small>{m.tasks_metrics_of_total({ total: metrics.total_tasks })}</small></div>
      </section>

      <section class="phase-metrics panel" aria-labelledby="task-phase-metrics-heading">
        <header><h2 id="task-phase-metrics-heading">{m.tasks_metrics_phase_timing()}</h2><Chip size="xs" uppercase={false}>{m.tasks_metrics_closed_intervals()}</Chip></header>
        {#if metrics.timing.phase_time.length === 0}
          <p class="empty-copy">{m.tasks_metrics_no_timing()}</p>
        {:else}
          <div class="phase-table" role="table" aria-label={m.tasks_metrics_phase_timing()}>
            <div class="phase-row phase-head" role="row">
              <span role="columnheader">{m.tasks_phase()}</span><span role="columnheader">{m.tasks_metrics_average()}</span><span role="columnheader">{m.tasks_metrics_range()}</span><span role="columnheader">{m.tasks_metrics_sample_count()}</span>
            </div>
            {#each metrics.timing.phase_time as item (item.phase)}
              <div class="phase-row" role="row">
                <span role="cell"><strong>{item.phase}</strong></span><span role="cell">{formatDuration(item.average_ms)}</span><span role="cell">{formatDuration(item.min_ms)} – {formatDuration(item.max_ms)}</span><span role="cell">{item.samples}</span>
              </div>
            {/each}
          </div>
        {/if}
      </section>

      <div class="distribution-grid">
        {#each distributions as distribution (distribution.id)}
          <section class="panel distribution" aria-labelledby={`task-distribution-${distribution.id}`}>
            <header><h2 id={`task-distribution-${distribution.id}`}>{distribution.label}</h2></header>
            <ul>
              {#each Object.entries(distribution.values).sort((a, b) => b[1] - a[1]) as [label, count]}
                <li>
                  <div><span>{label || m.tasks_unassigned()}</span><strong>{count}</strong></div>
                  <span class="bar"><span style={`width:${Math.max(3, count / maximum(distribution.values) * 100)}%`}></span></span>
                </li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .metrics-page { display: flex; min-height: 100%; flex-direction: column; background: var(--bg-primary); }
  .metrics-header { padding: 14px 20px 12px; border-bottom: 1px solid var(--border-default); }
  .metrics-header > div { display: grid; grid-template-columns: auto 1px minmax(0, 1fr); align-items: center; gap: 12px; }
  .metrics-header > div > span { width: 1px; height: 24px; background: var(--border-default); }
  h1, h2, p { margin: 0; }
  h1 { font-size: 17px; }
  .metrics-header p { grid-column: 3; color: var(--text-muted); font-size: 10px; }
  .filters { display: flex; align-items: center; gap: 12px; padding: 8px 20px; background: var(--bg-secondary); border-bottom: 1px solid var(--border-default); }
  .filter-grid { display: grid; flex: 1; grid-template-columns: repeat(5, minmax(112px, 1fr)) minmax(150px, auto); gap: 6px; }
  .metrics-content { display: flex; flex-direction: column; gap: 12px; padding: 12px; }
  .summary-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 12px; }
  .summary-strip > div { display: grid; grid-template-columns: 1fr auto; align-items: baseline; gap: 2px 8px; padding: 12px; }
  .summary-strip > div:not(:first-child) { border-left: 1px solid var(--border-subtle); }
  .summary-strip span, .summary-strip small { color: var(--text-muted); font-size: 10px; }
  .summary-strip strong { color: var(--text-primary); font-size: 16px; }
  .summary-strip small { grid-column: 1 / -1; }
  .panel { background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 12px; }
  .panel > header { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; border-bottom: 1px solid var(--border-subtle); }
  .panel h2 { font-size: 12px; }
  .phase-table { padding: 4px 12px 10px; }
  .phase-row { display: grid; grid-template-columns: 1.2fr 1fr 1.5fr 0.7fr; gap: 12px; padding: 7px 0; border-top: 1px solid var(--border-subtle); }
  .phase-row:first-child { border-top: 0; }
  .phase-row span, .phase-row strong { color: var(--text-secondary); font-size: 10px; }
  .phase-head span { color: var(--text-muted); font-weight: 600; }
  .distribution-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 12px; }
  .distribution ul { display: flex; margin: 0; padding: 6px 12px 12px; flex-direction: column; gap: 8px; list-style: none; }
  .distribution li { display: flex; flex-direction: column; gap: 4px; }
  .distribution li > div { display: flex; justify-content: space-between; gap: 8px; }
  .distribution li span, .distribution li strong { color: var(--text-secondary); font-size: 10px; }
  .bar { display: block; height: 3px; overflow: hidden; background: var(--border-subtle); border-radius: 2px; }
  .bar span { display: block; height: 100%; background: var(--accent-blue); border-radius: inherit; }
  .empty-copy { padding: 16px 12px; color: var(--text-muted); font-size: 10px; }
  .metrics-loading { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; padding: 12px; }
  .metrics-loading span { min-height: 180px; background: var(--bg-secondary); border-radius: 12px; animation: loading-pulse 1.4s ease-in-out infinite alternate; }
  @keyframes loading-pulse { to { opacity: 0.55; } }
  @media (prefers-reduced-motion: reduce) { .metrics-loading span { animation: none; } }
  @media (max-width: 900px) { .filters { align-items: flex-end; } .filter-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } .summary-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); } .summary-strip > div:nth-child(3) { border-left: 0; border-top: 1px solid var(--border-subtle); } .summary-strip > div:nth-child(4) { border-top: 1px solid var(--border-subtle); } }
  @media (max-width: 640px) { .metrics-header { padding-inline: 12px; } .filters { align-items: stretch; padding-inline: 12px; flex-direction: column; } .filter-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .phase-row { grid-template-columns: 1fr 1fr; } .phase-row > :nth-child(3), .phase-row > :nth-child(4) { display: none; } }
</style>
