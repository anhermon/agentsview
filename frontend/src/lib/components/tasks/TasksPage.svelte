<script lang="ts">
  import { onMount } from "svelte";
  import {
    Button,
    EmptyState,
    Spinner,
    Toggle,
    Typeahead,
    type TypeaheadOption,
  } from "@kenn-io/kit-ui";
  import { LayoutGridIcon } from "../../icons.js";
  import { m } from "../../i18n/index.js";
  import { router } from "../../stores/router.svelte.js";
  import { watchEvents } from "../../api/client.js";
  import {
    createTask,
    createTaskAgent,
    fetchTaskAgents,
    fetchTasks,
    fetchTaskWorkflow,
    putTaskWorkflow,
    updateTask,
  } from "../../api/tasks.js";
  import type {
    CreateTaskAgentRequest,
    CreateTaskRequest,
    Task,
    TaskAgent,
    TaskWorkflow,
    TaskWorkflowColumn,
    UpdateTaskRequest,
  } from "../../api/types/tasks.js";
  import AgentsModal from "./AgentsModal.svelte";
  import CreateTaskModal from "./CreateTaskModal.svelte";
  import TaskCard from "./TaskCard.svelte";

  const DEFAULT_PROJECT = "default";

  let tasks: Task[] = $state([]);
  let agents: TaskAgent[] = $state([]);
  let workflow: TaskWorkflow = $state(defaultWorkflow(DEFAULT_PROJECT));
  let project = $state(router.params.project || DEFAULT_PROJECT);
  let loading = $state(true);
  let error: string | null = $state(null);
  let operationError: string | null = $state(null);
  let createOpen = $state(false);
  let agentsOpen = $state(false);
  let savingTask = $state(false);
  let savingAgent = $state(false);
  let workflowSaving = $state(false);
  let busyTaskIds: string[] = $state([]);
  let draggedTaskId: string | null = $state(null);
  let dragTargetStatus: string | null = $state(null);

  const sortedColumns = $derived(
    [...workflow.columns].sort((a, b) => a.position - b.position),
  );
  const visibleTasks = $derived(tasks.filter((task) => task.project === project));
  const projectOptions: TypeaheadOption[] = $derived.by(() => {
    const names = new Set([DEFAULT_PROJECT, project, ...tasks.map((task) => task.project)]);
    return [...names].sort().map((name) => ({ name, label: name, displayLabel: name }));
  });

  function defaultColumns(): TaskWorkflowColumn[] {
    return [
      { id: "backlog", label: m.tasks_status_backlog(), position: 0 },
      { id: "ready", label: m.tasks_status_ready(), position: 1 },
      { id: "in_progress", label: m.tasks_status_in_progress(), position: 2 },
      { id: "blocked", label: m.tasks_status_blocked(), position: 3 },
      { id: "review", label: m.tasks_status_review(), position: 4 },
      { id: "done", label: m.tasks_status_done(), position: 5 },
    ];
  }

  function defaultWorkflow(forProject: string): TaskWorkflow {
    return {
      project: forProject,
      columns: defaultColumns(),
      phases: ["understand", "plan", "execute", "verify", "deliver"],
      automatic_transitions_enabled: false,
    };
  }

  function message(value: unknown, fallback: string): string {
    return value instanceof Error && value.message ? value.message : fallback;
  }

  async function loadWorkflow(nextProject: string): Promise<void> {
    try {
      workflow = await fetchTaskWorkflow(nextProject);
    } catch {
      workflow = defaultWorkflow(nextProject);
    }
  }

  async function load(): Promise<void> {
    loading = true;
    error = null;
    try {
      const [nextTasks, nextAgents] = await Promise.all([fetchTasks(), fetchTaskAgents()]);
      tasks = nextTasks;
      agents = nextAgents;
      if (!router.params.project && project === DEFAULT_PROJECT && nextTasks.length > 0) {
        project = nextTasks[0]?.project || DEFAULT_PROJECT;
      }
      await loadWorkflow(project);
    } catch (cause) {
      error = message(cause, m.tasks_load_failed());
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void load();
    const events = watchEvents((event) => {
      if (event.scope === "tasks") void load();
    });
    return () => events.close();
  });

  async function selectProject(value: string): Promise<void> {
    if (!value || value === project) return;
    project = value;
    operationError = null;
    router.replaceParams({ ...router.params, project: value });
    await loadWorkflow(value);
  }

  async function create(input: CreateTaskRequest): Promise<void> {
    savingTask = true;
    operationError = null;
    try {
      const created = await createTask(input);
      tasks = [...tasks, created];
      createOpen = false;
      if (created.project !== project) await selectProject(created.project);
    } catch (cause) {
      operationError = message(cause, m.tasks_create_failed());
    } finally {
      savingTask = false;
    }
  }

  async function registerAgent(input: CreateTaskAgentRequest): Promise<void> {
    savingAgent = true;
    operationError = null;
    try {
      const created = await createTaskAgent(input);
      agents = [...agents, created];
    } catch (cause) {
      operationError = message(cause, m.tasks_agent_create_failed());
    } finally {
      savingAgent = false;
    }
  }

  async function patchTask(task: Task, patch: UpdateTaskRequest): Promise<void> {
    if (busyTaskIds.includes(task.id)) return;
    const previous = task;
    operationError = null;
    busyTaskIds = [...busyTaskIds, task.id];
    tasks = tasks.map((item) => item.id === task.id ? { ...item, ...patch } : item);
    try {
      const updated = await updateTask(task.id, patch);
      tasks = tasks.map((item) => item.id === task.id ? updated : item);
    } catch (cause) {
      tasks = tasks.map((item) => item.id === task.id ? previous : item);
      operationError = message(cause, m.tasks_update_failed());
    } finally {
      busyTaskIds = busyTaskIds.filter((id) => id !== task.id);
    }
  }

  async function toggleAutomaticTransitions(enabled: boolean): Promise<void> {
    if (workflowSaving) return;
    const previous = workflow;
    workflowSaving = true;
    operationError = null;
    workflow = { ...workflow, automatic_transitions_enabled: enabled };
    try {
      workflow = await putTaskWorkflow(project, workflow);
    } catch (cause) {
      workflow = previous;
      operationError = message(cause, m.tasks_workflow_update_failed());
    } finally {
      workflowSaving = false;
    }
  }

  function tasksFor(status: string): Task[] {
    return visibleTasks.filter((task) => task.status === status);
  }

  function adjacentStatus(status: string, offset: number): string | undefined {
    const index = sortedColumns.findIndex((column) => column.id === status);
    return sortedColumns[index + offset]?.id;
  }

  function startDrag(event: DragEvent, taskId: string): void {
    draggedTaskId = taskId;
    event.dataTransfer?.setData("text/plain", taskId);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
  }

  function dragOver(event: DragEvent, status: string): void {
    event.preventDefault();
    dragTargetStatus = status;
    if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  }

  function leaveColumn(event: DragEvent, status: string): void {
    const target = event.currentTarget as HTMLElement;
    if (!target.contains(event.relatedTarget as Node | null) && dragTargetStatus === status) {
      dragTargetStatus = null;
    }
  }

  async function drop(event: DragEvent, status: string): Promise<void> {
    event.preventDefault();
    const id = draggedTaskId || event.dataTransfer?.getData("text/plain");
    draggedTaskId = null;
    dragTargetStatus = null;
    const task = tasks.find((item) => item.id === id);
    if (task && task.status !== status) await patchTask(task, { status });
  }
</script>

<div class="tasks-page">
  <header class="tasks-header">
    <div class="title-block">
      <div class="title-row">
        <LayoutGridIcon size="19" strokeWidth="1.8" aria-hidden="true" />
        <h1>{m.tasks_title()}</h1>
      </div>
      <p>{m.tasks_description()}</p>
    </div>
    <div class="header-actions">
      <Button
        label={m.tasks_manage_agents()}
        tone="neutral"
        surface="outline"
        onclick={() => (agentsOpen = true)}
      />
      <Button
        label={m.tasks_new_task()}
        tone="info"
        surface="solid"
        onclick={() => (createOpen = true)}
      />
    </div>
  </header>

  <div class="toolbar">
    <div class="project-picker">
      <span>{m.tasks_project()}</span>
      <Typeahead
        options={projectOptions}
        value={project}
        fallbackLabel={project}
        placeholder={m.tasks_project_filter_placeholder()}
        title={m.tasks_project_filter()}
        emptyLabel={m.tasks_no_projects()}
        onselect={selectProject}
      />
    </div>
    <div class="board-summary">
      <span>{m.tasks_task_count({ count: visibleTasks.length, countLabel: String(visibleTasks.length) })}</span>
      <span aria-hidden="true">·</span>
      <span>{m.tasks_agent_count({ count: agents.length, countLabel: String(agents.length) })}</span>
    </div>
    <div class="automation-control">
      <div>
        <span>{m.tasks_automatic_transitions()}</span>
        <small>{m.tasks_experimental()}</small>
      </div>
      <Toggle
        checked={workflow.automatic_transitions_enabled}
        disabled={workflowSaving}
        ariaLabel={m.tasks_automatic_transitions()}
        onchange={toggleAutomaticTransitions}
      >
        {workflow.automatic_transitions_enabled ? m.tasks_enabled() : m.tasks_disabled()}
      </Toggle>
    </div>
  </div>

  {#if operationError}
    <div class="operation-error" role="alert">
      <span>{operationError}</span>
      <Button size="sm" label={m.tasks_dismiss()} onclick={() => (operationError = null)} />
    </div>
  {/if}

  {#if loading}
    <div class="center-state"><Spinner size={22} /> <span>{m.tasks_loading()}</span></div>
  {:else if error}
    <EmptyState title={m.tasks_load_failed()} description={error}>
      <Button label={m.tasks_retry()} tone="info" surface="solid" onclick={load} />
    </EmptyState>
  {:else}
    <div class="board" aria-label={m.tasks_board_label()}>
      {#each sortedColumns as column (column.id)}
        {@const columnTasks = tasksFor(column.id)}
        <section
          class="board-column"
          class:drop-target={dragTargetStatus === column.id}
          aria-labelledby={`task-column-${column.id}`}
          ondragover={(event) => dragOver(event, column.id)}
          ondragleave={(event) => leaveColumn(event, column.id)}
          ondrop={(event) => drop(event, column.id)}
        >
          <header>
            <h2 id={`task-column-${column.id}`}>{column.label}</h2>
            <span>{columnTasks.length}</span>
          </header>
          <div class="column-body">
            {#if columnTasks.length === 0}
              <p class="column-empty">{m.tasks_column_empty()}</p>
            {:else}
              {#each columnTasks as task (task.id)}
                <TaskCard
                  {task}
                  {agents}
                  busy={busyTaskIds.includes(task.id)}
                  previousStatus={adjacentStatus(task.status, -1)}
                  nextStatus={adjacentStatus(task.status, 1)}
                  onmove={(status) => patchTask(task, { status })}
                  onphase={(phase) => patchTask(task, { phase })}
                  onassign={(agentId) => patchTask(task, { assignee_id: agentId })}
                  ondragstart={(event) => startDrag(event, task.id)}
                />
              {/each}
            {/if}
          </div>
        </section>
      {/each}
    </div>
  {/if}
</div>

<CreateTaskModal
  open={createOpen}
  {project}
  saving={savingTask}
  error={savingTask ? null : operationError}
  onclose={() => (createOpen = false)}
  oncreate={create}
/>

<AgentsModal
  open={agentsOpen}
  {agents}
  saving={savingAgent}
  error={savingAgent ? null : operationError}
  onclose={() => (agentsOpen = false)}
  oncreate={registerAgent}
/>

<style>
  .tasks-page {
    display: flex;
    min-height: 100%;
    flex-direction: column;
    background: var(--bg-primary);
  }

  .tasks-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 24px;
    padding: 18px 20px 14px;
    border-bottom: 1px solid var(--border-default);
  }

  .title-block,
  .title-row {
    display: flex;
  }

  .title-block {
    flex-direction: column;
    gap: 4px;
  }

  .title-row {
    align-items: center;
    gap: 8px;
  }

  h1,
  h2,
  p {
    margin: 0;
  }

  h1 {
    font-size: 17px;
    line-height: 1.3;
  }

  .title-block p {
    max-width: 68ch;
    color: var(--text-muted);
    font-size: 11px;
  }

  .header-actions {
    display: flex;
    gap: 8px;
  }

  .toolbar {
    display: flex;
    min-height: 48px;
    align-items: center;
    gap: 16px;
    padding: 8px 20px;
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-default);
  }

  .project-picker {
    display: grid;
    width: min(240px, 28vw);
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: 8px;
  }

  .project-picker > span,
  .board-summary,
  .automation-control span,
  .automation-control small {
    color: var(--text-muted);
    font-size: 10px;
  }

  .board-summary {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .automation-control {
    display: flex;
    margin-left: auto;
    align-items: center;
    gap: 8px;
  }

  .automation-control > div {
    display: flex;
    align-items: baseline;
    gap: 6px;
  }

  .automation-control small {
    color: var(--accent-amber);
  }

  .operation-error {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 7px 20px;
    color: var(--text-danger, var(--accent-red));
    background: color-mix(in srgb, var(--accent-red) 8%, var(--bg-primary));
    border-bottom: 1px solid color-mix(in srgb, var(--accent-red) 25%, var(--border-default));
    font-size: 11px;
  }

  .center-state {
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--text-muted);
    font-size: 12px;
  }

  .board {
    display: grid;
    flex: 1;
    min-height: 0;
    grid-auto-columns: minmax(250px, 1fr);
    grid-auto-flow: column;
    gap: 8px;
    padding: 12px;
    overflow-x: auto;
    overflow-y: hidden;
  }

  .board-column {
    display: flex;
    min-width: 250px;
    min-height: 0;
    flex-direction: column;
    background: var(--bg-secondary);
    border: 1px solid var(--border-subtle);
    border-radius: 12px;
    transition: background-color 160ms ease-out, border-color 160ms ease-out;
  }

  .board-column.drop-target {
    background: color-mix(in srgb, var(--accent-blue) 7%, var(--bg-secondary));
    border-color: color-mix(in srgb, var(--accent-blue) 48%, var(--border-default));
  }

  .board-column > header {
    display: flex;
    min-height: 38px;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 0 11px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .board-column h2 {
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 700;
  }

  .board-column header span {
    min-width: 19px;
    padding: 1px 5px;
    color: var(--text-muted);
    background: var(--bg-surface);
    border-radius: 999px;
    font-size: 10px;
    text-align: center;
  }

  .column-body {
    display: flex;
    min-height: 120px;
    padding: 8px;
    overflow-y: auto;
    flex-direction: column;
    gap: 8px;
  }

  .column-empty {
    margin: 22px 8px;
    color: var(--text-muted);
    font-size: 10px;
    text-align: center;
  }

  @media (max-width: 760px) {
    .tasks-header {
      align-items: stretch;
      flex-direction: column;
      gap: 12px;
      padding: 14px 12px 12px;
    }

    .header-actions {
      justify-content: flex-end;
    }

    .toolbar {
      align-items: stretch;
      flex-wrap: wrap;
      gap: 8px 16px;
      padding: 8px 12px;
    }

    .project-picker {
      width: min(280px, 100%);
      flex: 1 1 220px;
    }

    .automation-control {
      width: 100%;
      margin-left: 0;
      justify-content: space-between;
    }

    .board {
      grid-auto-columns: minmax(270px, 88vw);
      scroll-snap-type: x proximity;
    }

    .board-column {
      scroll-snap-align: start;
    }
  }
</style>
