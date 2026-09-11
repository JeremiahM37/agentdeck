import { useEffect, useState } from "react";
import type { Target } from "../types";
import { Modal } from "../sessions/Modal";
import type { SettingsApi } from "./Settings";
interface Command {
  name: string;
  state: "available" | "missing" | "unchecked";
  path?: string;
  detail?: string;
}
export function AgentCommands({
  api,
  target,
  onClose,
}: {
  api: SettingsApi;
  target: Target;
  onClose(): void;
}) {
  const [rows, setRows] = useState<Command[]>([]),
    [status, setStatus] = useState("Checking target commands…");
  async function load() {
    setStatus("Checking target commands…");
    try {
      setRows(await api.request<Command[]>(`/targets/${target.id}/agents`));
      setStatus("Command lookup complete. No agents were started.");
    } catch (e) {
      setRows([]);
      setStatus(e instanceof Error ? e.message : String(e));
    }
  }
  useEffect(() => {
    void load();
  }, []);
  return (
    <Modal className="agent-commands" aria-label="Agent commands" onCancel={onClose}>
      <h2>Agent commands</h2>
      <button onClick={onClose}>Close</button>
      <p>{target.name}</p>
      <p>
        Checks commands on this target’s default PATH. This does not check login
        or model access.
      </p>
      <p className="ac-status" role="status">{status}</p>
      <ul className="ac-results">
        {rows.map((r) => (
          <li key={r.name}>
            <b>{r.name}</b>
            <span>
              {r.state === "available"
                ? "Found"
                : r.state === "missing"
                  ? "Not found"
                  : "Not checked"}{" "}
              — {r.path || r.detail}
            </span>
          </li>
        ))}
      </ul>
      <button onClick={() => void load()}>Check again</button>
    </Modal>
  );
}
