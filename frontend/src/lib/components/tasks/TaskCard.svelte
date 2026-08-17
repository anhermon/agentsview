<script lang="ts">
  import {
    Chip,
    IconButton,
    Typeahead,
    type TypeaheadOption,
  } from "@kenn-io/kit-ui";
  import { ChevronLeftIcon, ChevronRightIcon } from "../../icons.js";
  import { m } from "../../i18n/index.js";
  import { formatRelativeTime } from "../../utils/format.js";
  import type { Task, TaskAgent, TaskGateStatus } from "../../api/types/tasks.js";

  interface Props {
    task: Task;
    agents: TaskAgent[];
    previousStatus?: string;
    nextStatus?: string;
    busy?: boolean;
    href: string;
    onopen: (event: MouseEvent) => void;
    onmove: (status: string) => void;
    onphase: (phase: string) => void;
    onassign: (agentId: string | null) => void;
    ondragstart: (event: DragEvent) => void;
  }

  let {
    task,
    agents,
    previousStatus,
    nextStatus,
    busy = false,
    href,
    onopen,
    onmove,
    onphase,
    onassign,
    ondragstart,
  }: Props = $props();

  const phases = ["Understand", "Plan", "Execute", "Verify", "Deliver"];
  const phaseIndex = $derived(Math.max(0, phases.findIndex((phase) => phase.toLowerCase() === task.phase.toLowerCase())));
  const passedGates = $derived(task.gates?.filter((gate) => gate.status === "passed").length ?? 0);
  const failedGates = $derived(task.gates?.filter((gate) => gate.status === "failed") ?? []);
  const evidence = $derived(task.evidence?.slice(0, 2) ?? []);
  const activeSessions = $derived(task.session_links?.filter((link) => link.active) ?? []);
  const agentOptions: TypeaheadOption[] = $derived([
    {
      name: "",
      label: m.tasks_unassigned(),
      displayLabel: m.tasks_unassigned(),
    },
    ...agents.map((agent) => ({
      name: agent.id,
      label: `${agent.name} · ${agent.harness}`,
      displayLabel: agent.name,
    })),
  ]);
  const phaseOptions: TypeaheadOption[] = $derived(
    phases.map((phase) => ({
      name: phase,
      label: phaseLabel(phase),
      displayLabel: phaseLabel(phase),
    })),
  );
  const assigneeLabel = $derived(
    task.assignee_name
      ? `${task.assignee_name}${task.harness ? ` · ${task.harness}` : ""}`
      : m.tasks_unassigned(),
  );

  function phaseLabel(phase: string): string {
    if (phase.toLowerCase() === "understand") return m.tasks_phase_understand();
    if (phase.toLowerCase() === "plan") return m.tasks_phase_plan();
    if (phase.toLowerCase() === "execute") return m.tasks_phase_execute();
    if (phase.toLowerCase() === "verify") return m.tasks_phase_verify();
    if (phase.toLowerCase() === "deliver") return m.tasks_phase_deliver();
    return phase;
  }

  function typeLabel(type: string): string {
    if (type === "feature") return m.tasks_type_feature();
    if (type === "bug") return m.tasks_type_bug();
    if (type === "research") return m.tasks_type_research();
    if (type === "maintenance") return m.tasks_type_maintenance();
    if (type === "review") return m.tasks_type_review();
    return type;
  }

  function gateTone(status: TaskGateStatus): "success" | "warning" | "danger" | "neutral" {
    if (status === "passed") return "success";
    if (status === "failed") return "danger";
    if (status === "pending") return "warning";
    return "neutral";
  }

  function gateLabel(status: TaskGateStatus): string {
    if (status === "passed") return m.tasks_gate_passed();
    if (status === "failed") return m.tasks_gate_failed();
    if (status === "pending") return m.tasks_gate_pending();
    return m.tasks_gate_waived();
  }

  function evidenceLabel(value: unknown): string {
    if (typeof value === "string") return value;
    if (value && typeof value === "object") {
      const record = value as Record<string, unknown>;
      for (const key of ["summary", "message", "path", "command"]) {
        if (typeof record[key] === "string") return record[key];
      }
    }
    return m.tasks_evidence_recorded();
  }
</script>

<!-- The card is draggable for pointer users; the labelled move controls below
     provide the same transition without drag and drop. -->
<article
  class="task-card"
  class:busy
  class:agent-active={activeSessions.length > 0}
  draggable={true}
  ondragstart={ondragstart}
  aria-label={task.title}
  data-task-id={task.id}
>
  <a class="card-link" {href} onclick={onopen} aria-label={m.tasks_open_task({ title: task.title })}>
    <span class="kit-sr-only">{task.title}</span>
  </a>
  <div class="card-topline">
    <Chip size="xs" uppercase={false}>{typeLabel(task.type)}</Chip>
    <span class="activity">
      {task.last_activity_at
        ? formatRelativeTime(task.last_activity_at)
        : m.tasks_no_activity()}
    </span>
  </div>

  <h3>{task.title}</h3>

  <div class="phase-block">
    <div class="phase-heading">
      <span>{m.tasks_phase()}</span>
      <strong>{phaseLabel(task.phase)}</strong>
    </div>
    <ol class="phase-track" aria-label={m.tasks_phase_progress()}>
      {#each phases as phase, index}
        <li
          class:complete={index < phaseIndex}
          class:current={index === phaseIndex}
          aria-current={index === phaseIndex ? "step" : undefined}
          title={phaseLabel(phase)}
        >
          <span class="phase-dot" aria-hidden="true"></span>
          <span class="kit-sr-only">{phaseLabel(phase)}</span>
        </li>
      {/each}
    </ol>
    <div class="phase-picker">
      <Typeahead
        options={phaseOptions}
        value={task.phase}
        fallbackLabel={phaseLabel(task.phase)}
        placeholder={m.tasks_phase_placeholder()}
        title={m.tasks_change_phase()}
        emptyLabel={m.tasks_no_phases()}
        disabled={busy}
        onselect={onphase}
      />
    </div>
  </div>

  <div class="assignee">
    <span>{m.tasks_assignee()}</span>
    <Typeahead
      options={agentOptions}
      value={task.assignee_id ?? ""}
      fallbackLabel={assigneeLabel}
      placeholder={m.tasks_assign_placeholder()}
      title={m.tasks_assign_agent()}
      emptyLabel={m.tasks_no_agents()}
      disabled={busy}
      onselect={(value) => onassign(value || null)}
    />
  </div>

  {#if task.session_summary || evidence.length > 0 || task.session_links?.length}
    <section class="session" aria-label={m.tasks_session_progress()}>
      {#if task.session_summary}<p>{task.session_summary}</p>{/if}
      {#if task.session_links?.length}
        <p>
          {activeSessions.length > 0
            ? m.tasks_active_sessions({ count: activeSessions.length, countLabel: String(activeSessions.length) })
            : m.tasks_linked_sessions({ count: task.session_links.length, countLabel: String(task.session_links.length) })}
        </p>
      {/if}
      {#if evidence.length > 0}
        <ul>
          {#each evidence as item}<li>{evidenceLabel(item)}</li>{/each}
        </ul>
      {/if}
    </section>
  {:else}
    <p class="no-session">{m.tasks_no_session_evidence()}</p>
  {/if}

  <div class="gates">
    <div class="gate-summary">
      <span>{m.tasks_gates()}</span>
      {#if task.gates?.length}
        <strong>{m.tasks_gates_passed({ passed: passedGates, total: task.gates.length })}</strong>
      {:else}
        <strong>{m.tasks_no_gates()}</strong>
      {/if}
    </div>
    {#if failedGates.length > 0}
      <div class="failed-gates">
        {#each failedGates as gate (gate.id)}
          <Chip size="xs" tone={gateTone(gate.status)} uppercase={false}>
            {gate.name} · {gateLabel(gate.status)}
          </Chip>
        {/each}
      </div>
    {/if}
  </div>

  <div class="card-actions">
    <span>{task.id}</span>
    <div>
      <IconButton
        size="sm"
        ariaLabel={m.tasks_move_previous()}
        title={m.tasks_move_previous()}
        disabled={!previousStatus || busy}
        onclick={() => previousStatus && onmove(previousStatus)}
      >
        <ChevronLeftIcon size="14" aria-hidden="true" />
      </IconButton>
      <IconButton
        size="sm"
        ariaLabel={m.tasks_move_next()}
        title={m.tasks_move_next()}
        disabled={!nextStatus || busy}
        onclick={() => nextStatus && onmove(nextStatus)}
      >
        <ChevronRightIcon size="14" aria-hidden="true" />
      </IconButton>
    </div>
  </div>
</article>

<style>
  .task-card {
    position: relative;
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-surface);
    border: 1px solid var(--border-default);
    border-radius: 12px;
    box-shadow: var(--shadow-xs);
    cursor: grab;
  }

  .card-link {
    position: absolute;
    z-index: 0;
    inset: 0;
    border-radius: inherit;
  }

  .card-link:focus-visible {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }

  .task-card > :not(.card-link) {
    position: relative;
    z-index: 1;
    pointer-events: none;
  }

  .task-card :global(button),
  .task-card :global(input) {
    pointer-events: auto;
  }

  .task-card:active {
    cursor: grabbing;
  }

  .task-card.busy {
    opacity: 0.68;
  }

  .task-card.agent-active {
    animation: agent-active-pulse 1.8s ease-in-out infinite;
  }

  @keyframes agent-active-pulse {
    0%, 100% {
      border-color: color-mix(in srgb, var(--accent-blue) 55%, var(--bg-surface));
      box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-blue) 40%, transparent);
    }
    50% {
      border-color: var(--accent-blue);
      box-shadow: 0 0 0 10px color-mix(in srgb, var(--accent-blue) 70%, transparent);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .task-card.agent-active {
      animation: none;
      border-color: var(--accent-blue);
      box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-blue) 40%, transparent);
    }
  }

  .card-topline,
  .phase-heading,
  .gate-summary,
  .card-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .activity,
  .phase-heading span,
  .gate-summary span,
  .assignee > span,
  .card-actions > span {
    color: var(--text-muted);
    font-size: 10px;
  }

  h3 {
    margin: 0;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 650;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .phase-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .phase-heading strong,
  .gate-summary strong {
    color: var(--text-secondary);
    font-size: 10px;
    font-weight: 600;
  }

  .phase-track {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 4px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .phase-track li {
    height: 3px;
    background: var(--border-default);
    border-radius: 2px;
  }

  .phase-track li.complete {
    background: color-mix(in srgb, var(--accent-blue) 55%, var(--border-default));
  }

  .phase-track li.current {
    background: var(--accent-blue);
  }

  .phase-dot {
    display: none;
  }

  .assignee {
    display: grid;
    grid-template-columns: 52px minmax(0, 1fr);
    align-items: center;
    gap: 8px;
  }

  .session {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px;
    background: var(--bg-secondary);
    border-radius: 8px;
  }

  .session p,
  .session ul,
  .no-session {
    margin: 0;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.4;
  }

  .session ul {
    padding-left: 16px;
    color: var(--text-muted);
  }

  .no-session {
    color: var(--text-muted);
  }

  .gates,
  .failed-gates {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .card-actions {
    padding-top: 2px;
  }

  .card-actions > span {
    max-width: 140px;
    overflow: hidden;
    font-family: var(--font-mono);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-actions > div {
    display: flex;
    gap: 4px;
  }

</style>
