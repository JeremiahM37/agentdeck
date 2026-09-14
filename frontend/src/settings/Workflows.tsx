import { useEffect, useRef, useState } from "react";
import type { SettingsApi } from "./Settings";

export interface ProjectWorkflow {
  id: "spec-kit" | "maestro" | string;
  name: string;
  description: string;
  version: string;
  upstream_url: string;
  enabled: boolean;
  commands: string[];
}

interface WorkflowsResponse {
  workflows?: ProjectWorkflow[];
  reload_required?: boolean;
}

const providerLabel = (agent: string) => (agent === "codex" ? "Codex" : "Claude Code");
const displayCommand = (command: string, agent: string) => {
  if (command.startsWith("/") || command.startsWith("$")) return command;
  return `${agent === "codex" ? "$" : "/"}${command}`;
};

export function Workflows({
  api,
  projectId,
  defaultAgent,
  onNotice,
}: {
  api: SettingsApi;
  projectId: number;
  defaultAgent: string;
  onNotice(t: string, e?: boolean): void;
}) {
  const [agent, setAgent] = useState(defaultAgent === "codex" ? "codex" : "claude");
  const [workflows, setWorkflows] = useState<ProjectWorkflow[]>([]);
  const [busy, setBusy] = useState(false);
  const [hasError, setHasError] = useState(false);
  const [status, setStatus] = useState("Loading available workflows…");
  const generation = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function load() {
    const mine = ++generation.current;
    setBusy(true);
    setHasError(false);
    setStatus("Loading available workflows…");
    try {
      const result = await api.request<WorkflowsResponse>(
        `/projects/${projectId}/workflows?agent=${encodeURIComponent(agent)}`,
      );
      if (!mounted.current || mine !== generation.current) return;
      const rows = Array.isArray(result.workflows) ? result.workflows : [];
      setWorkflows(rows);
      setStatus(`${rows.length} workflow${rows.length === 1 ? "" : "s"} available for ${providerLabel(agent)}`);
    } catch (error) {
      if (!mounted.current || mine !== generation.current) return;
      const message = error instanceof Error ? error.message : String(error);
      setWorkflows([]);
      setHasError(true);
      setStatus(message);
      onNotice(message, true);
    } finally {
      if (mounted.current && mine === generation.current) setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, [agent, projectId]);

  async function toggle(workflow: ProjectWorkflow) {
    const mine = generation.current;
    setBusy(true);
    try {
      const result = await api.request<{ enabled: boolean; reload_required?: boolean }>(
        `/projects/${projectId}/workflows/${encodeURIComponent(workflow.id)}`,
        { method: "PUT", body: { agent, enabled: !workflow.enabled } },
      );
      // A provider/project change while this request was in flight must win.
      if (mounted.current && mine === generation.current) {
        setWorkflows((rows) =>
          rows.map((row) =>
            row.id === workflow.id ? { ...row, enabled: Boolean(result.enabled) } : row,
          ),
        );
        setStatus(`${workflow.name} ${result.enabled ? "enabled" : "disabled"}. Start a new ${providerLabel(agent)} session to use the change.`);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (mounted.current && mine === generation.current) {
        setStatus(message);
        onNotice(message, true);
      }
    } finally {
      if (mounted.current && mine === generation.current) setBusy(false);
    }
  }

  return (
    <section className="project-workflows">
      <h4>Project workflows</h4>
      <p>
        Choose a workflow, then use its commands in a new agent session. Disabling keeps
        your specs, plans, and other project documents.
      </p>
      <p className="workflows-status" role="status">{status}</p>
      <label>
        Provider
        <select
          className="workflows-agent"
          aria-label="Workflows provider"
          value={agent}
          disabled={busy}
          onChange={(event) => setAgent(event.target.value)}
        >
          <option value="claude">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      <button className="workflows-reload" disabled={busy} onClick={() => void load()}>
        {busy ? "Loading…" : hasError ? "Retry" : "Reload"}
      </button>
      {!workflows.length && !busy && (
        <p>No optional workflows are available for this provider.</p>
      )}
      {workflows.map((workflow) => (
        <article className="workflow-card" key={workflow.id}>
          <div className="workflow-card-copy">
            <h5>{workflow.name}</h5>
            <p>{workflow.description}</p>
            <p className="workflow-version">
              Pinned version: <code>{workflow.version}</code>{" · "}
              <a href={workflow.upstream_url} target="_blank" rel="noreferrer">
                Upstream project
              </a>
            </p>
          </div>
          <button
            className="workflow-toggle"
            aria-label={`${workflow.enabled ? "Disable" : "Enable"} ${workflow.name}`}
            disabled={busy}
            onClick={() => void toggle(workflow)}
          >
            {workflow.enabled ? "Enabled" : "Enable"}
          </button>
          {workflow.enabled && (
            <div className="workflow-commands">
              <b>Commands</b>
              <ul>
                {workflow.commands.map((command) => (
                  <li key={command}><code>{displayCommand(command, agent)}</code></li>
                ))}
              </ul>
              <p className="workflow-reload-note">
                Start a new {providerLabel(agent)} session after enabling or disabling this workflow.
              </p>
            </div>
          )}
        </article>
      ))}
    </section>
  );
}
