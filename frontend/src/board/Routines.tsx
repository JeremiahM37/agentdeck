import { useEffect, useState } from "react";
import type { Project, TaskView } from "../types";
import type { BoardApi } from "./Board";
import { Modal } from "../sessions/Modal";
import "./board.css";

interface Routine {
  id: number;
  name: string;
  prompt: string;
  project_ids: number[];
  schedule: string;
  permission_mode: string;
  agent: string;
  model: string;
  enabled: boolean;
  next_run_at?: number;
}
interface RunOutput {
  tasks: unknown[];
  failed: unknown[];
}
export function Routines({
  api,
  projects,
  onClose,
  onTask,
  onChanged,
  onNotice,
}: {
  api: BoardApi;
  projects: Project[];
  onClose(): void;
  onTask(t: TaskView): void;
  onChanged(): void;
  onNotice(t: string, e?: boolean): void;
}) {
  const [rows, setRows] = useState<Routine[]>([]),
    [runs, setRuns] = useState<TaskView[]>([]),
    [editing, setEditing] = useState<number>();
  const [name, setName] = useState(""),
    [prompt, setPrompt] = useState(""),
    [ids, setIds] = useState<number[]>([]),
    [schedule, setSchedule] = useState(""),
    [permission, setPermission] = useState("acceptEdits"),
    [agent, setAgent] = useState(""),
    [model, setModel] = useState("");
  async function load() {
    const [r, t] = await Promise.all([
      api.request<Routine[]>("/routines"),
      api.tasks(),
    ]);
    setRows(r);
    setRuns(
      t.filter(
        (x) =>
          x.created_by?.startsWith("routine:") &&
          (["queued", "running", "review"].includes(x.status) ||
            Boolean(x.takeover)),
      ),
    );
  }
  useEffect(() => {
    void load().catch((e) => onNotice(String(e), true));
  }, []);
  function reset() {
    setEditing(undefined);
    setName("");
    setPrompt("");
    setIds([]);
    setSchedule("");
    setPermission("acceptEdits");
    setAgent("");
    setModel("");
  }
  function edit(r: Routine) {
    setEditing(r.id);
    setName(r.name);
    setPrompt(r.prompt);
    setIds(r.project_ids);
    setSchedule(r.schedule);
    setPermission(r.permission_mode || "acceptEdits");
    setAgent(r.agent || "");
    setModel(r.model || "");
  }
  async function save() {
    if (!ids.length) {
      onNotice("Pick at least one project", true);
      return;
    }
    const body = {
      name: name.trim(),
      prompt: prompt.trim(),
      project_ids: ids,
      schedule: schedule.trim(),
      permission_mode: permission,
      agent,
      model: model.trim(),
    };
    await api.request(editing ? `/routines/${editing}` : "/routines", {
      method: editing ? "PATCH" : "POST",
      body,
    });
    onNotice(editing ? "Routine updated" : "Routine saved");
    reset();
    await load();
  }
  async function run(r: Routine) {
    const x = await api.request<RunOutput>(`/routines/${r.id}/run`, {
      method: "POST",
    });
    onNotice(
      `Started ${x.tasks.length} task${x.tasks.length === 1 ? "" : "s"}${x.failed.length ? ` · ${x.failed.length} could not run` : ""}`,
    );
    onChanged();
    await load();
  }
  return (
    <Modal id="sheet" open className="sheet routines" aria-label="Routines" onCancel={onClose}>
      <header className="sheet-head">
        <h2>Routines</h2>
        <button onClick={onClose}>✕</button>
      </header>
      <p>
        A job you keep asking for, saved. One button runs it across every
        project you picked; give it a schedule and it runs itself.
      </p>
      {runs.length > 0 && (
        <section>
          <h3>Started routine runs</h3>
          {runs.map((t) => (
            <button key={t.id} onClick={() => onTask(t)}>
              {t.title} · {t.project_name} ·{" "}
              {t.takeover?.status === "ready" ? "interactive" : t.status}
            </button>
          ))}
        </section>
      )}
      {rows.length === 0 && (
        <p>
          No routines yet. If you have typed the same request at an agent twice,
          it belongs here.
        </p>
      )}
      <div id="rt-list">{rows.map((r) => (
        <article className="rowcard" key={r.id}>
          <h3>
            {r.name}
            {!r.enabled && " (off)"}
          </h3>
          <p>
            {r.project_ids
              .map((id) => projects.find((p) => p.id === id)?.name)
              .filter(Boolean)
              .join(", ") || "no projects"}
          </p>
          <p>
            {r.agent || "project default"}
            {r.model && ` · ${r.model}`} · {r.schedule || "manual only"}
            {r.schedule && ` · next ${r.next_run_at ? new Date(r.next_run_at * 1000).toLocaleString() : "pending"}`}
          </p>
          <div className="btnrow">
            <button
              onClick={() =>
                void run(r).catch((e) => onNotice(String(e), true))
              }
            >
              ▶ Run now
            </button>
            {r.schedule && (
              <button
                onClick={() =>
                  void api
                    .request(`/routines/${r.id}`, {
                      method: "PATCH",
                      body: { enabled: !r.enabled },
                    })
                    .then(load)
                }
              >
                {r.enabled ? "Pause" : "Resume"}
              </button>
            )}
            <button onClick={() => edit(r)}>Edit</button>
            <button
              onClick={() => {
                if (
                  confirm(
                    `Delete the routine “${r.name}”? The tasks it already created stay on the board.`,
                  )
                ) {
                  void api
                    .request(`/routines/${r.id}`, { method: "DELETE" })
                    .then(load);
                }
              }}
            >
              Delete
            </button>
          </div>
        </article>
      ))}</div>
      <details open={editing != null}>
        <summary id="rt-legend">{editing ? `Editing “${name}”` : `+ New routine`}</summary>
        <label>
          Name
          <input id="rt-name" value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Projects
          <select
            id="rt-projects"
            multiple
            value={ids.map(String)}
            onChange={(e) =>
              setIds([...e.target.selectedOptions].map((o) => Number(o.value)))
            }
          >
            {projects.map((p) => (
              <option value={p.id} key={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          What should the agent do?
          <textarea
            id="rt-prompt"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
        </label>
        <label>
          Schedule
          <input
            id="rt-schedule"
            value={schedule}
            onChange={(e) => setSchedule(e.target.value)}
            placeholder="daily at 09:00"
          />
        </label>
        <label>
          Agent
          <input
            value={agent}
            onChange={(e) => setAgent(e.target.value)}
            placeholder="project default"
          />
        </label>
        <label>
          Model
          <input value={model} onChange={(e) => setModel(e.target.value)} />
        </label>
        <label>
          Permission mode
          <select
            value={permission}
            onChange={(e) => setPermission(e.target.value)}
          >
            <option>acceptEdits</option>
            <option>bypassPermissions</option>
            <option>plan</option>
            <option>default</option>
          </select>
        </label>
        <button
          id="rt-save"
          onClick={() => void save().catch((e) => onNotice(String(e), true))}
        >
          {editing ? "Save changes" : "Save routine"}
        </button>
        {editing && <button onClick={reset}>Cancel</button>}
      </details>
    </Modal>
  );
}
