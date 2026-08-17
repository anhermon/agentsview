<script lang="ts">
  import { onMount } from "svelte";
  import { Button, Chip, EmptyState } from "@kenn-io/kit-ui";
  import { ChevronLeftIcon, ClockIcon, LinkIcon } from "../../icons.js";
  import { fetchTask, fetchTaskSessionPreview } from "../../api/tasks.js";
  import { watchEvents } from "../../api/client.js";
  import type { TaskDetail, TaskEvent, TaskGateStatus, TaskSessionPreviewMessage } from "../../api/types/tasks.js";
  import { m } from "../../i18n/index.js";
  import { router } from "../../stores/router.svelte.js";
  import { formatDuration } from "../../utils/duration.js";
  import { formatTimestamp } from "../../utils/format.js";

  interface Props { taskId: string }
  let { taskId }: Props = $props();
  let task: TaskDetail | null = $state(null);
  let loading = $state(true);
  let error: string | null = $state(null);
  let sessionMessages: TaskSessionPreviewMessage[] = $state([]);
  let sessionError = $state(false);

  async function load(showLoading = true): Promise<void> {
    if (showLoading) loading = true;
    error = null;
    try {
      task = await fetchTask(taskId);
      await loadSessionPreview();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : m.tasks_detail_load_failed();
    } finally {
      loading = false;
    }
  }

  async function loadSessionPreview(): Promise<void> {
    const link = task?.agent_session?.links.find((item) => item.active)
      ?? task?.agent_session?.links[0];
    sessionError = false;
    sessionMessages = [];
    if (!link?.recent_messages_api_url) return;
    try {
      sessionMessages = await fetchTaskSessionPreview(link.recent_messages_api_url);
    } catch {
      sessionError = true;
    }
  }

  onMount(() => {
    void load();
    const events = watchEvents((event) => {
      if (event.scope === "tasks" || event.scope === "messages" || event.scope === "sessions") void load(false);
    });
    return () => events.close();
  });

  function countPhaseReworks(events: TaskEvent[], fromPhase: string, toPhase: string): number {
    let count = 0;
    for (const event of events) {
      if (event.type !== "task.updated") continue;
      const before = event.payload?.before as { phase?: string } | undefined;
      const after = event.payload?.after as { phase?: string } | undefined;
      if (before?.phase === fromPhase && after?.phase === toPhase) count++;
    }
    return count;
  }

  function gateTone(status: TaskGateStatus): "success" | "danger" | "warning" | "neutral" {
    if (status === "passed") return "success";
    if (status === "failed") return "danger";
    if (status === "pending") return "warning";
    return "neutral";
  }

  function payloadSummary(event: TaskEvent): string {
    if (!event.payload) return m.tasks_event_no_details();
    for (const key of ["summary", "message", "status", "phase", "session_id", "gate_id"]) {
      const value = event.payload[key];
      if (typeof value === "string" && value) return value;
    }
    const entries = Object.entries(event.payload).slice(0, 3);
    return entries.length
      ? entries.map(([key, value]) => `${key}: ${String(value)}`).join(" · ")
      : m.tasks_event_no_details();
  }

  function evidenceSummary(value: unknown): string {
    if (typeof value === "string") return value;
    if (value && typeof value === "object") {
      const record = value as Record<string, unknown>;
      return payloadSummary({ id: 0, task_id: taskId, type: "evidence", source: "", payload: record, created_at: "" });
    }
    return m.tasks_evidence_recorded();
  }

  function openSession(event: MouseEvent, sessionId: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    router.navigateToSession(sessionId);
  }

  function messageSummary(message: TaskSessionPreviewMessage): string {
    const value = message.content.replace(/\s+/g, " ").trim();
    return value.length > 320 ? `${value.slice(0, 317)}…` : value;
  }
</script>

<div class="detail-page">
  <nav class="detail-nav" aria-label={m.tasks_detail_navigation()}>
    <Button label={m.tasks_back_to_board()} size="sm" onclick={() => router.navigate("tasks")}>
      <ChevronLeftIcon size="14" aria-hidden="true" />
    </Button>
    <a href={router.buildTaskMetricsHref()} onclick={(event) => {
      if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) {
        event.preventDefault();
        router.navigateToTaskMetrics();
      }
    }}>{m.tasks_view_metrics()}</a>
  </nav>

  {#if loading}
    <div class="detail-loading" aria-label={m.tasks_loading()}>
      <span></span><span></span><span></span>
    </div>
  {:else if error || !task}
    <EmptyState title={m.tasks_detail_load_failed()} description={error ?? m.tasks_detail_not_found()}>
      <Button label={m.tasks_retry()} tone="info" surface="solid" onclick={() => load()} />
    </EmptyState>
  {:else}
    <header class="detail-header">
      <div>
        <span class="task-id">{task.id}</span>
        <h1>{task.title}</h1>
        <p>{task.description || m.tasks_no_description()}</p>
      </div>
      <div class="header-chips" aria-label={m.tasks_detail_state()}>
        <Chip size="sm" tone="info" uppercase={false}>{task.status}</Chip>
        <Chip size="sm" uppercase={false}>{task.phase}</Chip>
        <Chip size="sm" uppercase={false}>{task.type || m.tasks_type_unknown()}</Chip>
      </div>
    </header>

    <div class="detail-grid">
      <main>
        <section class="panel agent-session-panel" aria-labelledby="task-agent-session-heading">
          <header>
            <h2 id="task-agent-session-heading">{m.tasks_agent_session()}</h2>
            {#if task.agent_session}
              <Chip size="xs" tone={task.agent_session.active ? "success" : "neutral"} uppercase={false}>
                {task.agent_session.run_state || (task.agent_session.active ? m.tasks_active() : m.tasks_disconnected())}
              </Chip>
            {/if}
          </header>
          {#if !task.agent_session}
            <div class="session-empty"><strong>{m.tasks_agent_session_empty()}</strong><span>{m.tasks_agent_session_empty_description()}</span></div>
          {:else}
            {@const sessionLink = task.agent_session.links[0]}
            <div class="session-overview">
              <dl>
                <div><dt>{m.tasks_assignee()}</dt><dd>{task.agent_session.agent_name || task.assignee_name || m.tasks_unassigned()}</dd></div>
                <div><dt>{m.tasks_agent_harness()}</dt><dd>{task.agent_session.harness || task.harness || "—"}</dd></div>
                <div><dt>{m.tasks_phase()}</dt><dd>{task.agent_session.phase || task.phase}</dd></div>
                <div><dt>{m.tasks_last_activity()}</dt><dd>{task.agent_session.last_activity_at ? formatTimestamp(task.agent_session.last_activity_at) : "—"}</dd></div>
              </dl>
              {#if sessionLink}
                <a class="open-session" href={sessionLink.full_session_url || router.buildSessionHref(sessionLink.session_id)} onclick={(event) => openSession(event, sessionLink.session_id)}>
                  {m.tasks_open_full_session()} <span aria-hidden="true">→</span>
                </a>
              {/if}
            </div>
            <div class="session-stream">
              <h3>{m.tasks_agent_current_work()}</h3>
              {#if sessionError}
                <p class="empty-copy">{m.tasks_agent_session_disconnected()}</p>
              {:else if sessionMessages.length}
                <ol class="message-preview">
                  {#each sessionMessages.slice(0, 6) as message (message.id)}
                    <li><div><strong>{message.role}</strong><span>{formatTimestamp(message.timestamp)}</span></div><p>{messageSummary(message)}</p></li>
                  {/each}
                </ol>
              {:else if task.agent_session.recent_activity.length}
                <ol class="message-preview">
                  {#each [...task.agent_session.recent_activity].reverse().slice(0, 6) as event (event.id)}
                    <li><div><strong>{event.type}</strong><span>{formatTimestamp(event.created_at)}</span></div><p>{payloadSummary(event)}</p></li>
                  {/each}
                </ol>
              {:else}
                <p class="empty-copy">{m.tasks_agent_session_no_activity()}</p>
              {/if}
              <small>{m.tasks_agent_session_bounded()}</small>
            </div>
          {/if}
        </section>

        <section class="panel activity-panel" aria-labelledby="task-activity-heading">
          <header><h2 id="task-activity-heading">{m.tasks_activity()}</h2><span>{task.events?.length ?? 0}</span></header>
          {#if !task.events?.length}
            <p class="empty-copy">{m.tasks_activity_empty()}</p>
          {:else}
            <ol class="timeline">
              {#each [...task.events].reverse() as event (event.id)}
                <li>
                  <span class="event-dot" aria-hidden="true"></span>
                  <div class="event-content">
                    <div><strong>{event.type}</strong><span>{formatTimestamp(event.created_at)}</span></div>
                    <p>{payloadSummary(event)}</p>
                    <small>{event.source}</small>
                  </div>
                </li>
              {/each}
            </ol>
          {/if}
        </section>
      </main>

      <aside>
        <section class="panel facts" aria-labelledby="task-facts-heading">
          <header><h2 id="task-facts-heading">{m.tasks_details()}</h2></header>
          <dl>
            <div><dt>{m.tasks_field_project()}</dt><dd>{task.project}</dd></div>
            <div><dt>{m.tasks_assignee()}</dt><dd>{task.assignee_name || m.tasks_unassigned()}</dd></div>
            <div><dt>{m.tasks_agent_harness()}</dt><dd>{task.harness || "—"}</dd></div>
            <div><dt>{m.tasks_created()}</dt><dd>{formatTimestamp(task.created_at)}</dd></div>
            <div><dt>{m.tasks_updated()}</dt><dd>{formatTimestamp(task.updated_at)}</dd></div>
          </dl>
        </section>

        <section class="panel timing" aria-labelledby="task-timing-heading">
          <header><h2 id="task-timing-heading">{m.tasks_timing()}</h2><ClockIcon size="14" aria-hidden="true" /></header>
          <dl class="timing-summary">
            <div><dt>{m.tasks_lead_time()}</dt><dd>{task.timing?.lead_time_ms == null ? "—" : formatDuration(task.timing.lead_time_ms)}</dd></div>
            <div><dt>{m.tasks_cycle_time()}</dt><dd>{task.timing?.cycle_time_ms == null ? "—" : formatDuration(task.timing.cycle_time_ms)}</dd></div>
          </dl>
          {#if task.timing?.phase_durations?.length}
            <ul class="phase-times">
              {#each task.timing.phase_durations as phase}
                <li><span>{phase.phase}</span><strong>{formatDuration(phase.total_ms)}</strong></li>
              {/each}
            </ul>
          {/if}
        </section>

        <section class="panel gates-panel" aria-labelledby="task-gates-heading">
          <header>
            <h2 id="task-gates-heading">{m.tasks_gates()}</h2>
            <Chip size="xs" tone={task.gate_summary?.completion_ready ? "success" : "warning"} uppercase={false}>
              {task.gate_summary?.completion_ready ? m.tasks_completion_ready() : m.tasks_completion_blocked()}
            </Chip>
          </header>
          {#if !task.gates?.length}
            <p class="empty-copy">{m.tasks_no_gates()}</p>
          {:else}
            <ul class="gate-list">
              {#each task.gates as gate (gate.id)}
                <li>
                  <div class="gate-row">
                    <div><strong>{gate.name}</strong><small>{gate.kind}{gate.required ? ` · ${m.tasks_required()}` : ""}</small></div>
                    <Chip size="xs" tone={gateTone(gate.status)} uppercase={false}>{gate.status}</Chip>
                  </div>
                  {#if gate.rule}<p class="gate-rule">{m.tasks_gate_rule()}: <code>{gate.rule}</code></p>{/if}
                  {#if gate.status !== "pending"}
                    <p class="gate-evidence">
                      {m.tasks_gate_evidence()}: {evidenceSummary(gate.evidence)}
                      {#if gate.evaluated_at}<span> · {formatTimestamp(gate.evaluated_at)}</span>{/if}
                    </p>
                  {/if}
                </li>
              {/each}
            </ul>
            {#if countPhaseReworks(task.events, "Verify", "Execute") > 0}
              {@const verifyReworkCount = countPhaseReworks(task.events, "Verify", "Execute")}
              <p class="verify-rework">{m.tasks_verify_rework({ count: verifyReworkCount, countLabel: String(verifyReworkCount) })}</p>
            {/if}
          {/if}
        </section>

        <section class="panel" aria-labelledby="task-evidence-heading">
          <header><h2 id="task-evidence-heading">{m.tasks_evidence()}</h2><span>{task.evidence?.length ?? 0}</span></header>
          {#if !task.evidence?.length}
            <p class="empty-copy">{m.tasks_evidence_empty()}</p>
          {:else}
            <ul class="evidence-list">
              {#each task.evidence as evidence}<li>{evidenceSummary(evidence)}</li>{/each}
            </ul>
          {/if}
        </section>

        <section class="panel" aria-labelledby="task-sessions-heading">
          <header><h2 id="task-sessions-heading">{m.tasks_linked_session_heading()}</h2><LinkIcon size="14" aria-hidden="true" /></header>
          {#if !task.session_links?.length}
            <p class="empty-copy">{m.tasks_linked_session_empty()}</p>
          {:else}
            <ul class="session-list">
              {#each task.session_links as link (link.id)}
                <li>
                  <a href={router.buildSessionHref(link.session_id)} onclick={(event) => openSession(event, link.session_id)}>{link.session_id}</a>
                  <span>{link.harness || link.method} · {Math.round(link.confidence * 100)}%</span>
                  {#if link.active}<Chip size="xs" tone="success" uppercase={false}>{m.tasks_active()}</Chip>{/if}
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </aside>
    </div>
  {/if}
</div>

<style>
  .detail-page { display: flex; min-height: 100%; flex-direction: column; background: var(--bg-primary); }
  .detail-nav { display: flex; min-height: 44px; align-items: center; justify-content: space-between; padding: 6px 20px; background: var(--bg-secondary); border-bottom: 1px solid var(--border-default); }
  .detail-nav a { color: var(--accent-blue); font-size: 11px; text-decoration: none; }
  .detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; padding: 20px; border-bottom: 1px solid var(--border-default); }
  .detail-header > div:first-child { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
  .task-id { color: var(--text-muted); font-family: var(--font-mono); font-size: 10px; }
  h1 { margin: 0; color: var(--text-primary); font-size: 20px; line-height: 1.3; }
  .detail-header p { max-width: 72ch; margin: 0; color: var(--text-secondary); font-size: 12px; }
  .header-chips { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
  .detail-grid { display: grid; flex: 1; min-height: 0; grid-template-columns: minmax(0, 1fr) minmax(280px, 360px); gap: 12px; padding: 12px; }
  main, aside { min-width: 0; }
  aside { display: flex; flex-direction: column; gap: 12px; }
  .panel { background: var(--bg-surface); border: 1px solid var(--border-default); border-radius: 12px; }
  .panel > header { display: flex; min-height: 38px; align-items: center; justify-content: space-between; gap: 8px; padding: 0 12px; border-bottom: 1px solid var(--border-subtle); }
  .panel h2 { margin: 0; color: var(--text-primary); font-size: 12px; }
  .panel header > span { color: var(--text-muted); font-size: 10px; }
  main { display: flex; flex-direction: column; gap: 12px; }
  .activity-panel { flex: 1; min-height: 320px; }
  .session-empty { display: flex; padding: 18px 12px; flex-direction: column; gap: 4px; }
  .session-empty strong { color: var(--text-secondary); font-size: 11px; }
  .session-empty span { color: var(--text-muted); font-size: 10px; }
  .session-overview { display: flex; align-items: center; gap: 12px; padding: 8px 12px; border-bottom: 1px solid var(--border-subtle); }
  .session-overview dl { display: grid; flex: 1; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; padding: 0; }
  .session-overview dl div { display: flex; min-width: 0; flex-direction: column; gap: 2px; padding: 0; }
  .session-overview dt { font-size: 9px; text-transform: uppercase; }
  .session-overview dd { overflow: hidden; font-size: 11px; text-align: left; text-overflow: ellipsis; white-space: nowrap; }
  .open-session { flex: none; color: var(--accent-blue); font-size: 10px; text-decoration: none; }
  .session-stream { padding: 10px 12px 12px; }
  .session-stream h3 { margin: 0 0 6px; color: var(--text-secondary); font-size: 10px; text-transform: uppercase; }
  .session-stream > small { display: block; margin-top: 7px; color: var(--text-muted); font-size: 9px; }
  .message-preview { display: flex; max-height: 260px; margin: 0; overflow: auto; padding: 0; flex-direction: column; gap: 1px; list-style: none; }
  .message-preview li { padding: 7px 8px; background: var(--bg-secondary); border-radius: 6px; }
  .message-preview li > div { display: flex; justify-content: space-between; gap: 8px; }
  .message-preview strong { color: var(--text-primary); font-size: 10px; text-transform: capitalize; }
  .message-preview span { color: var(--text-muted); font-size: 9px; }
  .message-preview p { margin: 2px 0 0; color: var(--text-secondary); font-family: var(--font-mono); font-size: 10px; line-height: 1.45; overflow-wrap: anywhere; }
  .timeline { margin: 0; padding: 8px 16px 20px; list-style: none; }
  .timeline li { position: relative; display: grid; grid-template-columns: 12px minmax(0, 1fr); gap: 8px; padding: 8px 0; }
  .timeline li:not(:last-child)::after { position: absolute; top: 24px; bottom: -8px; left: 5px; width: 1px; background: var(--border-default); content: ""; }
  .event-dot { z-index: 1; width: 9px; height: 9px; margin-top: 4px; background: var(--accent-blue); border: 2px solid var(--bg-surface); border-radius: 50%; }
  .event-content { min-width: 0; }
  .event-content > div { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
  .event-content strong { color: var(--text-primary); font-size: 11px; }
  .event-content span, .event-content small { color: var(--text-muted); font-size: 10px; }
  .event-content p { margin: 2px 0; color: var(--text-secondary); font-size: 11px; overflow-wrap: anywhere; }
  dl { margin: 0; padding: 4px 12px 10px; }
  dl div { display: flex; justify-content: space-between; gap: 12px; padding: 5px 0; }
  dt { color: var(--text-muted); font-size: 10px; }
  dd { margin: 0; color: var(--text-secondary); font-size: 11px; text-align: right; overflow-wrap: anywhere; }
  .timing-summary { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; padding-top: 10px; }
  .timing-summary div { flex-direction: column; gap: 2px; padding: 0; }
  .timing-summary dd { color: var(--text-primary); font-size: 15px; font-weight: 650; text-align: left; }
  .phase-times, .gate-list, .evidence-list, .session-list { margin: 0; padding: 4px 12px 10px; list-style: none; }
  .phase-times li { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 5px 0; border-top: 1px solid var(--border-subtle); }
  .gate-list li { padding: 7px 0; border-top: 1px solid var(--border-subtle); }
  .gate-row { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .gate-rule, .gate-evidence { margin: 4px 0 0; color: var(--text-muted); font-size: 10px; overflow-wrap: anywhere; }
  .gate-rule code { color: var(--text-secondary); font-family: var(--font-mono); }
  .verify-rework { margin: 0; padding: 8px 12px 10px; color: var(--accent-amber); font-size: 10px; border-top: 1px solid var(--border-subtle); }
  .phase-times span, .phase-times strong { font-size: 10px; }
  .gate-list div { display: flex; min-width: 0; flex-direction: column; }
  .gate-list strong { color: var(--text-secondary); font-size: 11px; }
  .gate-list small { color: var(--text-muted); font-size: 9px; }
  .evidence-list li { padding: 5px 0; color: var(--text-secondary); font-size: 10px; border-top: 1px solid var(--border-subtle); overflow-wrap: anywhere; }
  .session-list li { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 2px 8px; padding: 6px 0; border-top: 1px solid var(--border-subtle); }
  .session-list a { overflow: hidden; color: var(--accent-blue); font-family: var(--font-mono); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
  .session-list > li > span { color: var(--text-muted); font-size: 9px; }
  .empty-copy { margin: 0; padding: 16px 12px; color: var(--text-muted); font-size: 10px; }
  .detail-loading { display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 12px; padding: 12px; }
  .detail-loading span { min-height: 180px; background: var(--bg-secondary); border-radius: 12px; animation: loading-pulse 1.4s ease-in-out infinite alternate; }
  .detail-loading span:first-child { grid-row: span 2; min-height: 480px; }
  @keyframes loading-pulse { to { opacity: 0.55; } }
  @media (prefers-reduced-motion: reduce) { .detail-loading span { animation: none; } }
  @media (max-width: 900px) { .detail-grid { grid-template-columns: minmax(0, 1fr); } .activity-panel { min-height: 360px; } }
  @media (max-width: 640px) { .detail-nav { padding-inline: 12px; } .detail-header { flex-direction: column; padding: 16px 12px; } .header-chips { justify-content: flex-start; } .detail-grid { padding: 8px; } .session-overview { align-items: flex-start; flex-direction: column; } .session-overview dl { width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
