import { useEffect, useRef, useState, type CSSProperties } from "react";
import "../terminal/native-search.css";
import { Modal } from "./Modal";
import type { SessionsApi } from "./Sessions";
import type { SessionView, Target } from "../types";
import type { JsonValue } from "../api";
interface Hit {
  id: string;
  title: string;
  target: string;
  agent: string;
  cwd: string;
  snippet: string;
}
interface Scope {
  id: string;
  target: string;
  agent: string;
  state: string;
  error?: string;
  more?: boolean;
  progress: {
    documents: number;
    pending_files?: number;
    oversized_entries?: number;
    issues?: string[];
  };
}
interface Result {
  id: string;
  done: boolean;
  complete: boolean;
  results: Hit[];
  scopes: Scope[];
}
interface Choice {
  id: string;
  label: string;
  model?: string;
  supported: boolean;
}
interface Message {
  role: string;
  text: string;
  matched?: boolean;
  truncated?: boolean;
}
interface SearchPage {
  messages: Message[];
  before: number | null;
  after: number | null;
  fork_options?: Choice[];
  page_mode?: string;
  changed_neighbors?: number;
  index_complete?: boolean;
}
const explain = (e: unknown) => (e instanceof Error ? e.message : String(e));
export function NativeSearch({
  api,
  targets,
  onClose,
  onFork,
  onNotice,
}: {
  api: SessionsApi;
  targets: Target[];
  onClose(): void;
  onFork(s: SessionView): void;
  onNotice(t: string, e?: boolean): void;
}) {
  const [query, setQuery] = useState(""),
    [target, setTarget] = useState(""),
    [agent, setAgent] = useState(""),
    [result, setResult] = useState<Result>(),
    [status, setStatus] = useState(""),
    [starting, setStarting] = useState(false),
    [retry, setRetry] = useState(false),
    [hit, setHit] = useState<Hit>(),
    [page, setPage] = useState<SearchPage>(),
    [readStatus, setReadStatus] = useState(""),
    [reading, setReading] = useState(false),
    [forking, setForking] = useState(false),
    [forkPending, setForkPending] = useState(false),
    [configuration, setConfiguration] = useState(""),
    [name, setName] = useState("Conversation fork"),
    [isolated, setIsolated] = useState(false),
    [branch, setBranch] = useState(""),
    [base, setBase] = useState(""),
    [forkStatus, setForkStatus] = useState(""),
    [height, setHeight] = useState(
      window.visualViewport?.height || innerHeight,
    );
  const alive = useRef(true),
    generation = useRef(0),
    readGeneration = useRef(0),
    pollGeneration = useRef(0),
    job = useRef<string | undefined>(undefined),
    last = useRef<Result | undefined>(undefined),
    timer = useRef<number | undefined>(undefined),
    startBusy = useRef(false),
    forkBusy = useRef(false),
    results = useRef<HTMLDivElement>(null),
    messages = useRef<HTMLDivElement>(null),
    queryInput = useRef<HTMLInputElement>(null),
    backButton = useRef<HTMLButtonElement>(null),
    forkButton = useRef<HTMLButtonElement>(null),
    configInput = useRef<HTMLSelectElement>(null);
  const cancel = (id: string) =>
    api.request<Result>(`/conversation-search/${id}`, { method: "DELETE" });
  function accept(value: Result) {
    last.current = value;
    setResult(value);
    setRetry(false);
    setStatus("");
  }
  function later(id: string, version: number, ms = 600) {
    clearTimeout(timer.current);
    timer.current = window.setTimeout(() => void poll(id, version), ms);
  }
  async function poll(id: string, version: number) {
    const request = ++pollGeneration.current;
    try {
      const value = await api.request<Result>(`/conversation-search/${id}`);
      if (
        !alive.current ||
        version !== generation.current ||
        request !== pollGeneration.current
      )
        return;
      accept(value);
      if (!value.done) later(id, version);
    } catch (e) {
      if (
        alive.current &&
        version === generation.current &&
        request === pollGeneration.current
      ) {
        setStatus(
          `Could not update search: ${explain(e)}. Available results are retained.`,
        );
        setRetry(true);
      }
    }
  }
  async function start(reset = false) {
    if (startBusy.current || !query.trim()) return;
    const version = ++generation.current,
      previous = job.current;
    job.current = undefined;
    startBusy.current = true;
    setStarting(true);
    clearTimeout(timer.current);
    setRetry(false);
    setStatus("Starting search…");
    try {
      if (previous && !last.current?.done) {
        try {
          await cancel(previous);
        } catch (e) {
          if (!(
            e &&
            typeof e === "object" &&
            "status" in e &&
            e.status === 404
          ))
            throw e;
        }
      }
      if (!alive.current || version !== generation.current) return;
      const body: Record<string, JsonValue> = { query, reset };
      if (target) body.target_id = Number(target);
      if (agent) body.agent = agent;
      const value = await api.request<Result>("/conversation-search", {
        method: "POST",
        body,
      });
      if (!alive.current || version !== generation.current) {
        void cancel(value.id).catch(() => {});
        return;
      }
      job.current = value.id;
      accept(value);
      if (!value.done) later(value.id, version, 300);
    } catch (e) {
      if (alive.current && version === generation.current) {
        job.current = previous;
        setStatus(`Could not start search: ${explain(e)}`);
      }
    } finally {
      startBusy.current = false;
      if (alive.current) setStarting(false);
    }
  }
  async function stop() {
    const id = job.current,
      version = generation.current;
    if (!id) return;
    try {
      const value = await cancel(id);
      if (!alive.current || version !== generation.current) return;
      clearTimeout(timer.current);
      pollGeneration.current++;
      accept(value);
      if (!value.done) later(id, version);
    } catch (e) {
      if (alive.current) setStatus(`Could not stop search: ${explain(e)}`);
    }
  }
  function close() {
    if (forkBusy.current) return;
    onClose();
  }
  useEffect(() => {
    alive.current = true;
    queryInput.current?.focus();
    const fit = () => setHeight(window.visualViewport?.height || innerHeight);
    window.visualViewport?.addEventListener("resize", fit);
    return () => {
      alive.current = false;
      generation.current++;
      readGeneration.current++;
      clearTimeout(timer.current);
      if (job.current && !last.current?.done)
        void cancel(job.current).catch(() => {});
      window.visualViewport?.removeEventListener("resize", fit);
    };
  }, []);
  async function read(selected: Hit, suffix = "") {
    const version = ++readGeneration.current,
      id = job.current;
    if (!id) return;
    setHit(selected);
    setForking(false);
    setReading(true);
    setReadStatus("Loading matching message…");
    setPage(undefined);
    requestAnimationFrame(() => backButton.current?.focus());
    try {
      const value = await api.request<SearchPage>(
        `/conversation-search/${id}/results/${selected.id}${suffix}`,
      );
      if (!alive.current || version !== readGeneration.current) return;
      setPage(value);
      setReadStatus(
        `${value.page_mode === "latest" ? "Latest indexed messages" : value.page_mode && value.page_mode !== "match" ? "Saved messages" : "Matching message with nearby context"}${value.changed_neighbors ? ` · ${value.changed_neighbors} changed messages omitted` : ""}${!value.index_complete ? " · indexing is incomplete" : ""}.`,
      );
      requestAnimationFrame(() => {
        if (messages.current) {
          messages.current.scrollTop = 0;
          messages.current
            .querySelector(".ns-match")
            ?.scrollIntoView({ block: "center" });
        }
      });
    } catch (e) {
      if (alive.current && version === readGeneration.current)
        setReadStatus(`${explain(e)}. Return to results and search again.`);
    } finally {
      if (alive.current && version === readGeneration.current)
        setReading(false);
    }
  }
  function back() {
    readGeneration.current++;
    setHit(undefined);
    setForking(false);
    requestAnimationFrame(() => {
      const button = Array.from(
        results.current?.querySelectorAll<HTMLButtonElement>("button") || [],
      ).find((button) => button.dataset.resultId === hit?.id);
      button?.focus({ preventScroll: true });
    });
  }
  const choices =
    page?.fork_options?.filter((choice) => choice.supported) || [];
  function confirmFork() {
    if (!choices.length) return;
    setConfiguration(choices.length === 1 ? choices[0]!.id : "");
    setForkStatus("");
    setForking(true);
    requestAnimationFrame(() => configInput.current?.focus());
  }
  async function createFork() {
    if (forkBusy.current || !hit || !job.current || !configuration) return;
    forkBusy.current = true;
    setForkPending(true);
    setForkStatus("Starting fork…");
    const body: Record<string, JsonValue> = {
      configuration_id: configuration,
      name,
    };
    if (isolated) {
      body.background = true;
      body.worktree = { branch, base };
    }
    try {
      const session = await api.request<SessionView>(
        `/conversation-search/${job.current}/results/${hit.id}/fork`,
        { method: "POST", body },
      );
      if (alive.current) {
        onFork(session);
        onClose();
      }
    } catch (e) {
      if (alive.current) setForkStatus(explain(e));
      else onNotice(explain(e), true);
    } finally {
      forkBusy.current = false;
      if (alive.current) setForkPending(false);
    }
  }
  const unfinished =
      result?.scopes.filter((scope) =>
        ["queued", "indexing"].includes(scope.state),
      ).length || 0,
    problemCount =
      result?.scopes.filter(
        (scope) =>
          scope.error ||
          scope.progress.issues?.length ||
          scope.progress.oversized_entries,
      ).length || 0;
  const summary =
    status ||
    (result
      ? `${result.results.length} ${result.results.length === 1 ? "conversation" : "conversations"} found${unfinished ? ` · searching ${unfinished} ${unfinished === 1 ? "profile" : "profiles"}…` : result.complete ? "" : " · some profiles could not be fully searched"}${result.scopes.some((scope) => scope.more) ? " · more matches available; narrow your search" : ""}${!result.results.length && result.complete ? ". Try different words or filters." : ""}`
      : "Search saved messages across workspaces, including conversations you no longer track.");
  return (
    <Modal
      className="native-history native-search"
      aria-label="Search saved conversations"
      style={{ "--search-height": `${height}px` } as CSSProperties}
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (
          e.nativeEvent.isComposing ||
          hit ||
          !["ArrowDown", "ArrowUp"].includes(e.key)
        )
          return;
        const buttons = Array.from(
            results.current?.querySelectorAll<HTMLButtonElement>(
              ".ns-result",
            ) || [],
          ),
          index = buttons.indexOf(e.target as HTMLButtonElement);
        if (e.target !== queryInput.current && index < 0) return;
        e.preventDefault();
        buttons[
          Math.max(
            0,
            Math.min(
              buttons.length - 1,
              index + (e.key === "ArrowDown" ? 1 : -1),
            ),
          )
        ]?.focus();
      }}
    >
      <header>
        <h2>Search saved conversations</h2>
        <button
          className="ns-close"
          aria-label="Close saved conversation search"
          disabled={forkPending}
          onClick={close}
        >
          Close
        </button>
      </header>
      <section className="ns-browse" hidden={!!hit}>
        <form
          className="ns-form"
          onSubmit={(e) => {
            e.preventDefault();
            void start();
          }}
        >
          <label htmlFor="native-search-query">Conversation text</label>
          <input
            id="native-search-query"
            className="ns-query"
            ref={queryInput}
            type="search"
            required
            maxLength={500}
            placeholder="Find something discussed…"
            autoComplete="off"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <div className="ns-filters">
            <label>
              Target
              <select
                aria-label="Target"
                value={target}
                onChange={(e) => setTarget(e.target.value)}
              >
                <option value="">All targets</option>
                {targets.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Agent
              <select
                aria-label="Agent"
                value={agent}
                onChange={(e) => setAgent(e.target.value)}
              >
                <option value="">Claude and Codex</option>
                <option value="claude">Claude</option>
                <option value="codex">Codex</option>
              </select>
            </label>
            <button disabled={starting}>Search</button>
            {result && !result.done && !starting && (
              <button type="button" onClick={() => void stop()}>
                Stop search
              </button>
            )}
          </div>
        </form>
        <p className="ns-status" role="status">
          {summary}
        </p>
        {retry && (
          <button
            className="ns-retry"
            onClick={() => {
              if (job.current) void poll(job.current, generation.current);
            }}
          >
            Retry connection
          </button>
        )}
        {!!result?.scopes.length && (
          <details className="ns-progress">
            <summary>
              Target progress · {result.scopes.length}{" "}
              {result.scopes.length === 1 ? "profile" : "profiles"}
              {problemCount ? ` · ${problemCount} with issues` : ""}
            </summary>
            <div>
              {result.scopes.map((scope) => (
                <p key={scope.id}>
                  {scope.target} · {scope.agent}: {scope.error || scope.state} ·{" "}
                  {scope.progress.documents} conversations
                  {scope.progress.pending_files
                    ? ` · ${scope.progress.pending_files} pending`
                    : ""}
                  {scope.progress.oversized_entries
                    ? ` · ${scope.progress.oversized_entries} oversized entries skipped`
                    : ""}
                  {scope.progress.issues?.length
                    ? " · " + scope.progress.issues.join("; ")
                    : ""}
                </p>
              ))}
            </div>
          </details>
        )}
        <div
          className="ns-results"
          aria-label="Saved conversation results"
          ref={results}
        >
          {result?.results.map((row) => (
            <button
              type="button"
              className="ns-result"
              data-result-id={row.id}
              key={row.id}
              onClick={() => void read(row)}
            >
              <strong>{row.title}</strong>
              <small>
                {row.target} · {row.agent} · {row.cwd}
              </small>
              <span>{row.snippet}</span>
            </button>
          ))}
        </div>
        <details className="ns-advanced">
          <summary>Search options</summary>
          <p>
            If a transcript was rewritten, rebuild its search index. Saved
            conversations remain unchanged.
          </p>
          <button disabled={starting} onClick={() => void start(true)}>
            Rebuild and search
          </button>
        </details>
      </section>
      <section className="ns-reader" hidden={!hit}>
        <div className="ns-reader-head">
          <button ref={backButton} disabled={forkPending} onClick={back}>
            Back to results
          </button>
          <p className="ns-location">
            {hit && `${hit.target} · ${hit.agent} · ${hit.cwd}`}
          </p>
        </div>
        <div className="ns-page-controls" hidden={forking}>
          <button
            disabled={reading || page?.before == null}
            onClick={() => hit && void read(hit, `?before=${page?.before}`)}
          >
            Earlier messages
          </button>
          <button
            disabled={reading || page?.after == null}
            onClick={() => hit && void read(hit, `?after=${page?.after}`)}
          >
            Later messages
          </button>
          <button
            disabled={reading}
            onClick={() => hit && void read(hit, "?latest=1")}
          >
            Latest indexed
          </button>
          <button disabled={reading} onClick={() => hit && void read(hit)}>
            Back to match
          </button>
          {choices.length > 0 && (
            <button ref={forkButton} disabled={reading} onClick={confirmFork}>
              Fork conversation
            </button>
          )}
        </div>
        <p className="ns-read-status" role="status">
          {readStatus}
        </p>
        <div className="nh-messages" ref={messages} hidden={forking}>
          {page?.messages.map((message, index) =>
            message.role === "tool" ? (
              <details
                key={index}
                className={`nh-message nh-tool${message.matched ? " ns-match" : ""}`}
                open={message.matched || undefined}
              >
                <summary>
                  Tool activity{message.matched ? " · Matching message" : ""}
                </summary>
                <pre>{message.text}</pre>
              </details>
            ) : (
              <article
                key={index}
                className={`nh-message nh-${message.role}${message.matched ? " ns-match" : ""}`}
              >
                <h3>
                  {message.role === "user" ? "You" : "Assistant"}
                  {message.matched ? " · Matching message" : ""}
                </h3>
                <pre>{message.text}</pre>
                {message.truncated && (
                  <small>Long message shortened in this view.</small>
                )}
              </article>
            ),
          )}
        </div>
        {forking && (
          <form
            className="ns-fork-form"
            onSubmit={(e) => {
              e.preventDefault();
              void createFork();
            }}
          >
            <h3>Fork saved conversation</h3>
            <p className="ns-fork-warning">
              Fork the whole saved conversation, including messages after the
              match.{" "}
              {isolated
                ? "Start in a new Git worktree from the selected committed base; uncommitted changes stay in the original workspace."
                : "Both conversations will use the same workspace files."}{" "}
              The original conversation and terminal stay intact.
            </p>
            <label>
              Launch settings
              <select
                ref={configInput}
                aria-label="Launch settings"
                required
                disabled={forkPending}
                value={configuration}
                onChange={(e) => setConfiguration(e.target.value)}
              >
                {choices.length > 1 && (
                  <option value="">Choose launch settings</option>
                )}
                {choices.map((choice) => (
                  <option key={choice.id} value={choice.id}>
                    {choice.label}
                    {choice.model ? " · " + choice.model : ""}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Session name
              <input
                aria-label="Session name"
                value={name}
                maxLength={160}
                disabled={forkPending}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label>
              Workspace
              <select
                aria-label="Fork workspace"
                disabled={forkPending}
                value={isolated ? "isolated" : "shared"}
                onChange={(e) => setIsolated(e.target.value === "isolated")}
              >
                <option value="shared">Use the same workspace files</option>
                <option value="isolated">New isolated Git worktree</option>
              </select>
            </label>
            {isolated && (
              <>
                <label>
                  Branch (blank = automatic)
                  <input
                    aria-label="Branch (blank = automatic)"
                    value={branch}
                    disabled={forkPending}
                    onChange={(e) => setBranch(e.target.value)}
                  />
                </label>
                <label>
                  Base commit or branch (blank = HEAD)
                  <input
                    aria-label="Base commit or branch (blank = HEAD)"
                    value={base}
                    disabled={forkPending}
                    onChange={(e) => setBase(e.target.value)}
                  />
                </label>
              </>
            )}
            <p className="ns-fork-status" role="status">
              {forkStatus}
            </p>
            <button disabled={forkPending}>Create fork</button>
            <button
              type="button"
              disabled={forkPending}
              onClick={() => {
                setForking(false);
                requestAnimationFrame(() => forkButton.current?.focus());
              }}
            >
              Cancel fork
            </button>
          </form>
        )}
      </section>
    </Modal>
  );
}
