<script lang="ts">
  import {
    Button,
    Modal,
    TextInput,
    Typeahead,
    type TypeaheadOption,
  } from "@kenn-io/kit-ui";
  import { m } from "../../i18n/index.js";
  import type { CreateTaskRequest } from "../../api/types/tasks.js";

  interface Props {
    open: boolean;
    project: string;
    saving?: boolean;
    error?: string | null;
    onclose: () => void;
    oncreate: (input: CreateTaskRequest) => void;
  }

  let { open, project, saving = false, error = null, onclose, oncreate }: Props = $props();
  let title = $state("");
  let description = $state("");
  let taskProject = $state("");
  let type = $state("feature");

  const typeOptions: TypeaheadOption[] = $derived([
    { name: "feature", label: m.tasks_type_feature(), displayLabel: m.tasks_type_feature() },
    { name: "bug", label: m.tasks_type_bug(), displayLabel: m.tasks_type_bug() },
    { name: "research", label: m.tasks_type_research(), displayLabel: m.tasks_type_research() },
    { name: "maintenance", label: m.tasks_type_maintenance(), displayLabel: m.tasks_type_maintenance() },
    { name: "review", label: m.tasks_type_review(), displayLabel: m.tasks_type_review() },
  ]);

  $effect(() => {
    if (open) taskProject = project;
  });

  function close(): void {
    if (saving) return;
    title = "";
    description = "";
    type = "feature";
    onclose();
  }

  function submit(): void {
    if (!title.trim() || !taskProject.trim() || saving) return;
    oncreate({
      title: title.trim(),
      description: description.trim() || undefined,
      project: taskProject.trim(),
      type,
      status: "backlog",
    });
  }
</script>

{#snippet actions()}
  <Button
    label={m.tasks_cancel()}
    tone="neutral"
    surface="outline"
    disabled={saving}
    onclick={close}
  />
  <Button
    label={saving ? m.tasks_creating() : m.tasks_create()}
    tone="info"
    surface="solid"
    disabled={!title.trim() || !taskProject.trim() || saving}
    onclick={submit}
  />
{/snippet}

{#if open}
  <Modal
    title={m.tasks_create_title()}
    closeLabel={m.tasks_close_create()}
    width="520px"
    maxWidth="min(520px, 94vw)"
    closable={!saving}
    closeOnOverlayClick={!saving}
    onclose={close}
    footer={actions}
  >
    <div class="form">
      <label>
        <span>{m.tasks_field_title()}</span>
        <TextInput
          block
          bind:value={title}
          ariaLabel={m.tasks_field_title()}
          placeholder={m.tasks_title_placeholder()}
        />
      </label>
      <label>
        <span>{m.tasks_field_description()}</span>
        <TextInput
          block
          bind:value={description}
          ariaLabel={m.tasks_field_description()}
          placeholder={m.tasks_description_placeholder()}
        />
      </label>
      <div class="row">
        <label>
          <span>{m.tasks_field_project()}</span>
          <TextInput
            block
            bind:value={taskProject}
            ariaLabel={m.tasks_field_project()}
            placeholder={m.tasks_project_placeholder()}
          />
        </label>
        <label>
          <span>{m.tasks_field_type()}</span>
          <Typeahead
            options={typeOptions}
            value={type}
            fallbackLabel={typeOptions.find((option) => option.name === type)?.label ?? type}
            placeholder={m.tasks_type_placeholder()}
            title={m.tasks_field_type()}
            emptyLabel={m.tasks_no_task_types()}
            onselect={(value) => { type = value; }}
          />
        </label>
      </div>
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
    </div>
  </Modal>
{/if}

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  label {
    display: flex;
    min-width: 0;
    flex: 1;
    flex-direction: column;
    gap: 6px;
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 600;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 12px;
  }

  .error {
    margin: 0;
    color: var(--text-danger, var(--accent-red));
    font-size: 12px;
  }

  @media (max-width: 640px) {
    .row {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
