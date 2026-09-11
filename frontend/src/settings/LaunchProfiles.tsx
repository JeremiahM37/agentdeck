import { useEffect, useState } from "react";
import { Modal } from "../sessions/Modal";
import type { JsonValue } from "../api";

export type LaunchProfile = { id: number; name: string; agent: string; command: string; model: string; env_json: string };
type Agent = { name: string };
type Api = { request<T>(path: string, options?: { method?: string; body?: JsonValue }): Promise<T> };

export function LaunchProfiles({ api, onClose, onChange = () => {} }: { api: Api; onClose(): void; onChange?(profile?: LaunchProfile): void }) {
  const [profiles, setProfiles] = useState<LaunchProfile[]>([]), [agents, setAgents] = useState<Agent[]>([]);
  const [selected, setSelected] = useState(0), [name, setName] = useState(""), [agent, setAgent] = useState("claude"), [command, setCommand] = useState(""), [model, setModel] = useState(""), [environment, setEnvironment] = useState("{}"), [busy, setBusy] = useState(false), [status, setStatus] = useState("");
  async function load(preferred = selected) {
    const [p, a] = await Promise.all([api.request<LaunchProfile[]>("/launch-profiles"), api.request<Agent[]>("/agents")]);
    setProfiles(p); setAgents(a);
    const found = p.find((row) => row.id === preferred);
    if (found) choose(found); else if (!preferred) choose(undefined);
    return p;
  }
  function choose(p?: LaunchProfile) {
    setSelected(p?.id || 0); setName(p?.name || ""); setAgent(p?.agent || agents[0]?.name || "claude"); setCommand(p?.command || ""); setModel(p?.model || "");
    try { setEnvironment(JSON.stringify(JSON.parse(p?.env_json || "{}"), null, 2)); } catch { setEnvironment("{}"); }
    setStatus("");
  }
  useEffect(() => { void load().catch((e) => setStatus(String(e))); }, []);
  async function save() {
    let env: unknown;
    try { env = JSON.parse(environment); } catch { return setStatus("Environment must be valid JSON."); }
    if (!env || Array.isArray(env) || typeof env !== "object" || Object.values(env).some((v) => typeof v !== "string")) return setStatus("Environment must be a JSON object with string values.");
    if (!name.trim()) return setStatus("Name is required.");
    setBusy(true); setStatus("Saving profile…");
    try {
      const saved = await api.request<LaunchProfile>(selected ? `/launch-profiles/${selected}` : "/launch-profiles", { method: selected ? "PUT" : "POST", body: { name: name.trim(), agent, command: command.trim(), model: model.trim(), env_json: JSON.stringify(env) } });
      await load(saved.id); setSelected(saved.id); setStatus("Profile saved"); onChange(saved);
    } catch (e) { setStatus(String(e)); } finally { setBusy(false); }
  }
  async function remove() {
    if (!selected || !confirm(`Delete ${name}?`)) return;
    setBusy(true);
    try { await api.request(`/launch-profiles/${selected}`, { method: "DELETE" }); await load(0); setStatus("Profile deleted"); onChange(undefined); } catch (e) { setStatus(String(e)); } finally { setBusy(false); }
  }
  return <Modal open className="launch-profiles" aria-label="Launch profiles" onCancel={(e) => { if (busy) e.preventDefault(); else onClose(); }}>
    <header><h2>Launch profiles</h2><button disabled={busy} onClick={onClose}>Close</button></header>
    <div className="lp-picker"><label>Saved profile<select aria-label="Saved profile" className="lp-select" value={selected} onChange={(e) => choose(profiles.find((p) => p.id === Number(e.target.value)))}><option value={0}>New profile</option>{profiles.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label><button disabled={busy} onClick={() => choose(undefined)}>New</button></div>
    <form onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <label>Name<input value={name} onChange={(e) => setName(e.target.value)} /></label>
      <label>Agent<select aria-label="Agent" className="lp-agent" value={agent} onChange={(e) => setAgent(e.target.value)}>{agents.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}</select></label>
      <label>Command override<input value={command} onChange={(e) => setCommand(e.target.value)} /></label>
      <label>Default model<input value={model} onChange={(e) => setModel(e.target.value)} /></label>
      <label>Environment (JSON)<textarea value={environment} onChange={(e) => setEnvironment(e.target.value)} /></label>
      <div className="lp-buttons"><button disabled={busy}>Save profile</button><button type="button" disabled={busy || !selected} onClick={() => void remove()}>Delete profile</button></div>
      <p className="lp-status" role="status">{status}</p>
    </form>
  </Modal>;
}
