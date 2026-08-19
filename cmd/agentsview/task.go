package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"go.kenn.io/agentsview/internal/config"
	"go.kenn.io/agentsview/internal/taskcontrol"
)

type taskCommandDeps struct {
	resolve func(*cobra.Command) (taskcontrol.TaskService, error)
}

func defaultTaskCommandDeps() taskCommandDeps {
	return taskCommandDeps{resolve: resolveTaskService}
}

func resolveTaskService(cmd *cobra.Command) (taskcontrol.TaskService, error) {
	if remote, _ := cmd.Flags().GetString("server"); strings.TrimSpace(remote) != "" {
		token, err := explicitServerToken(cmd)
		if err != nil {
			return nil, err
		}
		return taskcontrol.NewHTTPClient(remote, token), nil
	}
	cfg, err := config.LoadPFlags(cmd.Flags())
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	tr, err := ensureTransportContext(
		cmd.Context(), &cfg, transportIntentArchiveWrite, 0,
	)
	if err != nil {
		return nil, err
	}
	if tr.Mode != transportHTTP {
		return nil, errors.New(
			"task commands require the local daemon; start it with agentsview daemon start",
		)
	}
	return taskcontrol.NewHTTPClient(tr.URL, cfg.AuthToken), nil
}

func newTaskCommand() *cobra.Command {
	return newTaskCommandWithDeps(defaultTaskCommandDeps())
}

func newTaskCommandWithDeps(deps taskCommandDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "task",
		Short:        "Programmatic task-board control for agents",
		GroupID:      groupData,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	registerFormatFlags(cmd.PersistentFlags())
	cmd.PersistentFlags().String("server", "", "Remote daemon URL")
	cmd.PersistentFlags().String("server-token-file", "",
		"File containing bearer token for explicit --server requests")
	cmd.AddCommand(
		newTaskListCommand(deps), newTaskCreateCommand(deps),
		newTaskShowCommand(deps), newTaskUpdateCommand(deps),
		newTaskTransitionCommand(deps), newTaskAssignCommand(deps),
		newTaskEventCommand(deps), newTaskCompleteCommand(deps),
		newTaskGateCommand(deps),
	)
	return cmd
}

func taskService(cmd *cobra.Command, deps taskCommandDeps) (taskcontrol.TaskService, error) {
	if deps.resolve == nil {
		return nil, errors.New("task service resolver is not configured")
	}
	return deps.resolve(cmd)
}

func newTaskListCommand(deps taskCommandDeps) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List board tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			tasks, err := svc.ListTasks(cmd.Context(), project)
			if err != nil {
				return err
			}
			if outputFormat(cmd) == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Items []taskcontrol.Task `json:"items"`
				}{Items: tasks})
			}
			return printTaskList(cmd.OutOrStdout(), tasks)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Filter by project")
	return cmd
}

func printTaskList(w io.Writer, tasks []taskcontrol.Task) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tPROJECT\tSTATUS\tPHASE\tASSIGNEE\tTITLE"); err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			task.ID, task.Project, task.Status, task.Phase, task.AssigneeID, task.Title); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func newTaskCreateCommand(deps taskCommandDeps) *cobra.Command {
	var task taskcontrol.Task
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(task.Project) == "" || strings.TrimSpace(task.Title) == "" {
				return errors.New("--project and --title are required")
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			created, err := svc.CreateTask(cmd.Context(), task)
			if err != nil {
				return err
			}
			return printTask(cmd, created)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&task.Project, "project", "", "Project identifier")
	flags.StringVar(&task.Title, "title", "", "Task title")
	flags.StringVar(&task.Description, "description", "", "Task description")
	flags.StringVar(&task.TaskType, "type", "", "Task type")
	flags.StringVar(&task.Status, "status", "", "Initial workflow status")
	flags.StringVar(&task.Phase, "phase", "", "Initial universal phase")
	flags.IntVar(&task.Priority, "priority", 0, "Task priority")
	flags.StringVar(&task.AssigneeID, "assignee", "", "Agent ID to assign")
	flags.StringVar(&task.ActiveRunID, "run", "", "Active harness run/session ID")
	return cmd
}

func newTaskShowCommand(deps taskCommandDeps) *cobra.Command {
	return &cobra.Command{
		Use: "show <id>", Short: "Show one task", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			task, err := svc.GetTask(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printTask(cmd, task)
		},
	}
}

type taskUpdateFlags struct {
	title, description, taskType, status, phase, assignee, run string
	priority                                                   int
}

func bindTaskUpdateFlags(cmd *cobra.Command, flags *taskUpdateFlags) {
	f := cmd.Flags()
	f.StringVar(&flags.title, "title", "", "Replace task title")
	f.StringVar(&flags.description, "description", "", "Replace task description")
	f.StringVar(&flags.taskType, "type", "", "Replace task type")
	f.StringVar(&flags.status, "status", "", "Move to workflow status")
	f.StringVar(&flags.phase, "phase", "", "Set universal phase")
	f.IntVar(&flags.priority, "priority", 0, "Set task priority")
	f.StringVar(&flags.assignee, "assignee", "", "Assign agent ID; empty clears")
	f.StringVar(&flags.run, "run", "", "Set active run/session ID; empty clears")
}

func patchFromTaskFlags(cmd *cobra.Command, flags taskUpdateFlags) (taskcontrol.TaskPatch, bool) {
	var patch taskcontrol.TaskPatch
	changed := false
	setString := func(name string, value string, target **string) {
		if cmd.Flags().Changed(name) {
			copy := value
			*target = &copy
			changed = true
		}
	}
	setString("title", flags.title, &patch.Title)
	setString("description", flags.description, &patch.Description)
	setString("type", flags.taskType, &patch.TaskType)
	setString("status", flags.status, &patch.Status)
	setString("phase", flags.phase, &patch.Phase)
	setString("assignee", flags.assignee, &patch.AssigneeID)
	setString("run", flags.run, &patch.ActiveRunID)
	if cmd.Flags().Changed("priority") {
		value := flags.priority
		patch.Priority = &value
		changed = true
	}
	return patch, changed
}

func newTaskUpdateCommand(deps taskCommandDeps) *cobra.Command {
	var flags taskUpdateFlags
	cmd := &cobra.Command{
		Use: "update <id>", Short: "Update task fields", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch, changed := patchFromTaskFlags(cmd, flags)
			if !changed {
				return errors.New("at least one update flag is required")
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			task, err := svc.UpdateTask(cmd.Context(), args[0], patch)
			if err != nil {
				return err
			}
			return printTask(cmd, task)
		},
	}
	bindTaskUpdateFlags(cmd, &flags)
	return cmd
}

func newTaskTransitionCommand(deps taskCommandDeps) *cobra.Command {
	var phase string
	cmd := &cobra.Command{
		Use: "transition <id> <status>", Short: "Move a task between workflow states", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch := taskcontrol.TaskPatch{Status: &args[1]}
			if cmd.Flags().Changed("phase") {
				patch.Phase = &phase
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			task, err := svc.UpdateTask(cmd.Context(), args[0], patch)
			if err != nil {
				return err
			}
			return printTask(cmd, task)
		},
	}
	cmd.Flags().StringVar(&phase, "phase", "", "Set phase with the transition")
	return cmd
}

func newTaskAssignCommand(deps taskCommandDeps) *cobra.Command {
	var run string
	cmd := &cobra.Command{
		Use: "assign <id> <agent-id>", Short: "Assign one agent to a task", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch := taskcontrol.TaskPatch{AssigneeID: &args[1]}
			if cmd.Flags().Changed("run") {
				patch.ActiveRunID = &run
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			task, err := svc.UpdateTask(cmd.Context(), args[0], patch)
			if err != nil {
				return err
			}
			return printTask(cmd, task)
		},
	}
	cmd.Flags().StringVar(&run, "run", "", "Active harness run/session ID")
	return cmd
}

func newTaskEventCommand(deps taskCommandDeps) *cobra.Command {
	var eventType, source, payloadJSON string
	cmd := &cobra.Command{
		Use: "event <id>", Short: "Append a structured task event", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(eventType) == "" {
				return errors.New("--type is required")
			}
			payload := map[string]any{}
			if payloadJSON != "" {
				if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
					return fmt.Errorf("invalid --payload JSON: %w", err)
				}
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			event, err := svc.AppendTaskEvent(cmd.Context(), taskcontrol.TaskEvent{TaskID: args[0], Type: eventType, Source: source, Payload: payload})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(event)
		},
	}
	cmd.Flags().StringVar(&eventType, "type", "", "Event type, for example agent.progress")
	cmd.Flags().StringVar(&source, "source", "agent", "Event source")
	cmd.Flags().StringVar(&payloadJSON, "payload", "", "JSON object payload")
	return cmd
}

func newTaskGateCommand(deps taskCommandDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use: "gate", Short: "Manage task completion gates", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newTaskGateListCommand(deps), newTaskGateCreateCommand(deps), newTaskGateEvaluateCommand(deps),
	)
	return cmd
}

func newTaskGateListCommand(deps taskCommandDeps) *cobra.Command {
	return &cobra.Command{
		Use: "list <task-id>", Short: "List a task's completion gates", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			gates, err := svc.ListGates(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Items []taskcontrol.Gate `json:"items"`
			}{Items: gates})
		},
	}
}

func newTaskGateCreateCommand(deps taskCommandDeps) *cobra.Command {
	var name, kind, rule string
	var required bool
	var sortOrder int
	cmd := &cobra.Command{
		Use: "create <task-id>", Short: "Create a task completion gate", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			gate, err := svc.CreateGate(cmd.Context(), taskcontrol.Gate{
				TaskID: args[0], Name: name, Kind: taskcontrol.GateKind(kind),
				Rule: rule, Required: required, SortOrder: sortOrder,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(gate)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Gate name")
	cmd.Flags().StringVar(&kind, "kind", string(taskcontrol.GateKindDeterministic), "Gate kind: deterministic, human, or llm")
	cmd.Flags().StringVar(&rule, "rule", "", "Rule identifier evaluated by the gate")
	cmd.Flags().BoolVar(&required, "required", true, "Whether the gate blocks completion until it passes")
	cmd.Flags().IntVar(&sortOrder, "sort-order", 0, "Display order among a task's gates")
	return cmd
}

func newTaskGateEvaluateCommand(deps taskCommandDeps) *cobra.Command {
	var evidenceJSON string
	var approved bool
	cmd := &cobra.Command{
		Use: "evaluate <task-id> <gate-id>", Short: "Evaluate a task completion gate", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			evidence := map[string]any{}
			if evidenceJSON != "" {
				if err := json.Unmarshal([]byte(evidenceJSON), &evidence); err != nil {
					return fmt.Errorf("invalid --evidence JSON: %w", err)
				}
			}
			var approvedPtr *bool
			if cmd.Flags().Changed("approved") {
				approvedPtr = &approved
			}
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			gate, err := svc.EvaluateGate(cmd.Context(), args[0], args[1], approvedPtr, evidence)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(gate)
		},
	}
	cmd.Flags().StringVar(&evidenceJSON, "evidence", "", "JSON object evidence, for example {\"passed\":true}")
	cmd.Flags().BoolVar(&approved, "approved", false, "Explicit human approval, overrides evidence.passed")
	return cmd
}

func newTaskCompleteCommand(deps taskCommandDeps) *cobra.Command {
	var status, phase string
	cmd := &cobra.Command{
		Use: "complete <id>", Short: "Complete a task after its required gates pass", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := taskService(cmd, deps)
			if err != nil {
				return err
			}
			task, err := svc.CompleteTask(cmd.Context(), args[0], status, phase)
			if err != nil {
				return err
			}
			return printTask(cmd, task)
		},
	}
	cmd.Flags().StringVar(&status, "status", "Done", "Terminal workflow status")
	cmd.Flags().StringVar(&phase, "phase", "Deliver", "Completion phase")
	return cmd
}

func printTask(cmd *cobra.Command, task taskcontrol.Task) error {
	if outputFormat(cmd) == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(task)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n",
		task.ID, task.Status, task.Phase, task.AssigneeID, task.Title)
	return err
}
