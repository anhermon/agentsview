<script lang="ts">
  import {
    Button,
    Chip,
    Modal,
    TextInput,
    Typeahead,
    type TypeaheadOption,
  } from "@kenn-io/kit-ui";
  import { m } from "../../i18n/index.js";
  import type { CreateTaskAgentRequest, TaskAgent } from "../../api/types/tasks.js";

  interface Props {
    open: boolean;
    agents: TaskAgent[];
    saving?: boolean;
    error?: string | null;
    onclose: () => void;
    oncreate: (input: CreateTaskAgentRequest) => void;
  }

  let { open, agents, saving = false, error = null, onclose, oncreate }: Props = $props();
  let name = $state("");
  let harness = $state("codex");

  const harnessOptions: TypeaheadOption[] = [
    "claude",
    "codex",
    "agy",
    "pi",
    "hermes",
    "dsh",
  ].map((value) => ({ name: value, label: value, displayLabel: value }));

  function statusTone(status: string): "success" | "warning" | "neutral" {
    if (status === "available") return "success";
    if (status === "working") return "warning";
    return "neutral";
  }

  function statusLabel(status: string): string {
    if (status === "available") return m.tasks_agent_available();
    if (status === "working") return m.tasks_agent_working();
    if (status === "offline") return m.tasks_agent_offline();
    return status;
  }

  function submit(): void {
    if (!name.trim() || saving) return;
    oncreate({ name: name.trim(), harness });
  }
</script>

{#snippet actions()}
  <Button label={m.tasks_close()} tone="neutral" surface="outline" onclick={onclose} />
{/snippet}

{#if open}
  <Modal
    title={m.tasks_agents_title()}
    closeLabel={m.tasks_close_agents()}
    width="600px"
    maxWidth="min(600px, 94vw)"
    onclose={onclose}
    footer={actions}
  >
    <div class="agents-layout">
      <section aria-labelledby="task-agent-list-heading">
        <h3 id="task-agent-list-heading">{m.tasks_agents_registered()}</h3>
        {#if agents.length === 0}
          <p class="empty">{m.tasks_agents_empty()}</p>
        {:else}
          <ul class="agent-list">
            {#each agents as agent (agent.id)}
              <li>
                <div>
                  <strong>{agent.name}</strong>
                  <span>{agent.harness}</span>
                </div>
                <Chip size="xs" tone={statusTone(agent.status)}>{statusLabel(agent.status)}</Chip>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="register" aria-labelledby="task-agent-register-heading">
        <h3 id="task-agent-register-heading">{m.tasks_agents_register()}</h3>
        <label>
          <span>{m.tasks_agent_name()}</span>
          <TextInput
            block
            bind:value={name}
            ariaLabel={m.tasks_agent_name()}
            placeholder={m.tasks_agent_name_placeholder()}
          />
        </label>
        <label>
          <span>{m.tasks_agent_harness()}</span>
          <Typeahead
            options={harnessOptions}
            value={harness}
            fallbackLabel={harness}
            placeholder={m.tasks_agent_harness_placeholder()}
            title={m.tasks_agent_harness()}
            emptyLabel={m.tasks_no_harnesses()}
            onselect={(value) => { harness = value; }}
          />
        </label>
        {#if error}<p class="error" role="alert">{error}</p>{/if}
        <Button
          label={saving ? m.tasks_agents_registering() : m.tasks_agents_register_action()}
          tone="info"
          surface="solid"
          disabled={!name.trim() || saving}
          onclick={submit}
        />
      </section>
    </div>
  </Modal>
{/if}

<style>
  .agents-layout {
    display: grid;
    grid-template-columns: minmax(0, 1.25fr) minmax(210px, 0.75fr);
    gap: 24px;
  }

  section {
    min-width: 0;
  }

  h3 {
    margin: 0 0 10px;
    color: var(--text-primary);
    font-size: 13px;
  }

  .agent-list {
    display: flex;
    max-height: 280px;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    flex-direction: column;
    list-style: none;
  }

  .agent-list li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 9px 0;
    border-bottom: 1px solid var(--border-subtle);
  }

  .agent-list div {
    display: flex;
    min-width: 0;
    flex-direction: column;
  }

  .agent-list strong {
    overflow: hidden;
    color: var(--text-primary);
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .agent-list span,
  .empty {
    color: var(--text-muted);
    font-size: 11px;
  }

  .register {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-left: 18px;
    border-left: 1px solid var(--border-default);
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 600;
  }

  .error {
    margin: 0;
    color: var(--text-danger, var(--accent-red));
    font-size: 12px;
  }

  @media (max-width: 640px) {
    .agents-layout {
      grid-template-columns: minmax(0, 1fr);
    }

    .register {
      padding-top: 16px;
      padding-left: 0;
      border-top: 1px solid var(--border-default);
      border-left: 0;
    }
  }
</style>
