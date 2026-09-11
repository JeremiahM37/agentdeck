import { subscribeLayout } from "./layout";
import { errorMessage } from "./model";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { Engine, type Snapshot } from "./engine";
import { Appearance, Desktop, HistoryDialog, WorkspaceFiles } from "./dialogs";
import {
  copyClipboard,
  json,
  loadPrefs,
  quote,
  themes,
  type Attachment,
  type Prefs,
  type TerminalInfo,
} from "./model";
import "@xterm/xterm/css/xterm.css";
import "./terminal.css";
export type SharedTool = "review" | "saved" | "search";
interface PaneSpec {
  id: string;
  url: string;
  label: string;
}
interface Callbacks {
  engine: (id: string, engine: Engine | null) => void;
  state: (id: string, state: Snapshot) => void;
  select: (id: string) => void;
  notice: (text: string) => void;
  history: (id: string) => void;
  matches: (id: string, index: number, count: number) => void;
  preview: (path: string) => void;
}
function Pane({
  spec,
  prefs,
  active,
  info,
  callbacks,
}: {
  spec: PaneSpec;
  prefs: Prefs;
  active: boolean;
  info: TerminalInfo;
  callbacks: Callbacks;
}) {
  const el = useRef<HTMLElement>(null),
    host = useRef<HTMLDivElement>(null),
    frozen = useRef<HTMLPreElement>(null),
    engine = useRef<Engine | null>(null);
  const latest = useRef({ prefs, callbacks });
  latest.current = { prefs, callbacks };
  const [state, setState] = useState<Snapshot>({
    connected: false,
    paused: false,
    status: "Connecting…",
    frozen: "",
    retained: false,
  });
  useEffect(() => {
    if (!el.current || !host.current || !frozen.current) return;
    const instance = new Engine({
      url: spec.url,
      host: host.current,
      pane: el.current,
      frozen: frozen.current,
      prefs: () => latest.current.prefs,
      select: () => latest.current.callbacks.select(spec.id),
      change: (state) => {
        setState(state);
        latest.current.callbacks.state(spec.id, state);
      },
      notice: (text) => latest.current.callbacks.notice(text),
      history: () => latest.current.callbacks.history(spec.id),
      matches: (index, count) =>
        latest.current.callbacks.matches(spec.id, index, count),
    });
    engine.current = instance;
    latest.current.callbacks.engine(spec.id, instance);
    if (info.files_available)
      instance.fileLinks(info.workdir, (path) =>
        latest.current.callbacks.preview(path),
      );
    return () => {
      instance.dispose();
      latest.current.callbacks.engine(spec.id, null);
      engine.current = null;
    };
  }, [spec.id, spec.url, info.files_available, info.workdir]);
  useEffect(() => {
    engine.current?.applyPrefs();
  }, [prefs]);
  useLayoutEffect(() => {
    engine.current?.syncFrozenLayout();
  }, [state.retained, state.frozen]);
  return (
    <section
      ref={el}
      id={spec.id === "agent" ? "agent-pane" : "shell-pane"}
      className={`pane ${active ? "active" : ""} ${state.connected ? "connected" : ""}`}
      onPointerDown={() => callbacks.select(spec.id)}
      onFocus={() => callbacks.select(spec.id)}
    >
      <div className="pane-title">
        <span id={spec.id === "agent" ? "agent-label" : undefined}>
          {spec.label}
        </span>
        <span className="pane-status">{state.status}</span>
      </div>
      <div
        id={spec.id === "agent" ? "agent-terminal" : "shell-terminal"}
        className="terminal-host"
        ref={host}
        hidden={state.paused}
        style={{
          background: (themes[prefs.theme] || themes.slate)?.background,
        }}
      />
      <pre
        className={`frozen ${state.retained ? "retained-history" : ""}`}
        ref={frozen}
        hidden={!state.paused && !state.retained}
        style={{ fontSize: prefs.fontSize }}
        onScroll={(event) => {
          const node = event.currentTarget;
          if (
            state.retained &&
            node.scrollHeight - node.clientHeight - node.scrollTop <= 2
          )
            engine.current?.leaveRetainedHistory();
        }}
        onClick={() => {
          if (state.retained && !window.getSelection()?.toString())
            engine.current?.term.focus();
        }}
      >
        {state.frozen}
      </pre>
    </section>
  );
}
declare global {
  interface Window {
    __adkTerminalState?: () => {
      hasSelection: boolean;
      mouseTrackingMode: string;
    };
  }
}
const keys = {
  escape: "\x1b",
  tab: "\t",
  left: "\x1b[D",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  interrupt: "\x03",
};
const labels = {
  escape: ["Esc", "Send Escape"],
  tab: ["Tab", "Send Tab"],
  left: ["←", "Send Left arrow"],
  up: ["↑", "Send Up arrow"],
  down: ["↓", "Send Down arrow"],
  right: ["→", "Send Right arrow"],
  interrupt: ["^C", "Send Ctrl-C"],
};
export function TerminalApp({
  kind,
  id,
  onShared,
  externalNotice = "",
}: {
  kind: string;
  id: string;
  onShared: (tool: SharedTool, info: TerminalInfo) => void;
  externalNotice?: string;
}) {
  const base = `/api/term/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`;
  const embedded = new URLSearchParams(location.search).get("embed") === "1";
  const [info, setInfo] = useState<TerminalInfo>();
  const [prefs, setPrefs] = useState(loadPrefs);
  const [active, setActive] = useState("agent");
  const [split, setSplit] = useState(false);
  const [notice, setNotice] = useState("");
  const [uploading, setUploading] = useState(0);
  const [dialog, setDialog] = useState<
    "appearance" | "history" | "files" | "desktop" | null
  >(null);
  const [historyPane, setHistoryPane] = useState("agent");
  const [previewPath, setPreviewPath] = useState<string>();
  const [search, setSearch] = useState(false);
  const [query, setQuery] = useState("");
  const [matches, setMatches] = useState("");
  const [states, setStates] = useState<Record<string, Snapshot>>({});
  const engines = useRef(new Map<string, Engine>()),
    activeRef = useRef(active),
    infoRef = useRef(info),
    file = useRef<HTMLInputElement>(null),
    searchInput = useRef<HTMLInputElement>(null),
    tools = useRef<HTMLDetailsElement>(null);
  activeRef.current = active;
  infoRef.current = info;
  useEffect(() => {
    if (externalNotice) setNotice(externalNotice);
  }, [externalNotice]);
  const current = () => engines.current.get(activeRef.current);
  const state = states[active];
  const showHistory = useCallback((pane: string) => {
    setHistoryPane(pane);
    setDialog("history");
  }, []);
  const preview = useCallback((path: string) => {
    setPreviewPath(path);
    setDialog("files");
  }, []);
  const register = useCallback((id: string, engine: Engine | null) => {
    if (engine) engines.current.set(id, engine);
    else engines.current.delete(id);
  }, []);
  const update = useCallback(
    (id: string, value: Snapshot) =>
      setStates((old) => ({ ...old, [id]: value })),
    [],
  );
  const match = useCallback((id: string, index: number, count: number) => {
    if (id === activeRef.current)
      setMatches(count ? `${index + 1} / ${count}` : "No matches");
  }, []);
  const callbacks: Callbacks = {
    engine: register,
    state: update,
    select: setActive,
    notice: setNotice,
    history: showHistory,
    matches: match,
    preview,
  };
  useEffect(() => {
    const abort = new AbortController();
    void json<TerminalInfo>(base + "/info", { signal: abort.signal })
      .then((data) => {
        setInfo(data);
        document.title = data.tmux_session + " · AgentDeck";
      })
      .catch((error) => {
        if (!abort.signal.aborted) setNotice(errorMessage(error));
      });
    return () => abort.abort();
  }, [base]);
  useEffect(() => {
    window.__adkTerminalState = () => ({
      hasSelection: !!current()?.term.hasSelection(),
      mouseTrackingMode: current()?.term.modes.mouseTrackingMode || "none",
    });
    return () => {
      delete window.__adkTerminalState;
    };
  }, []);
  useEffect(() => {
    const abort = new AbortController(),
      signal = abort.signal,
      mobile = matchMedia("(max-width:1023px)"),
      short = matchMedia("(max-height:380px)");
    document.body.classList.toggle("embedded", embedded);
    const chrome = () =>
      document.body.classList.toggle(
        "compact-chrome",
        document.body.classList.contains("compact-terminal") ||
          (document.body.classList.contains("mobile-terminal") &&
            short.matches),
      );
    const layout = () => {
      if (!embedded)
        document.body.classList.toggle("mobile-terminal", mobile.matches);
      chrome();
    };
    layout();
    mobile.addEventListener("change", layout, { signal });
    short.addEventListener("change", chrome, { signal });
    const fit = () => engines.current.forEach((engine) => engine.scheduleFit());
    window.visualViewport?.addEventListener("resize", fit, { signal });
    window.addEventListener("focus", fit, { signal });
    window.addEventListener("pageshow", fit, { signal });
    void document.fonts?.ready.then(() => {
      if (!signal.aborted) fit();
    });
    const unsubscribe = subscribeLayout((data) => {
      if (embedded) {
        document.body.classList.toggle("compact-terminal", data.compact);
        document.body.classList.toggle("mobile-terminal", data.mobile);
        chrome();
        fit();
      }
    });
    document.addEventListener(
      "visibilitychange",
      () => {
        if (!document.hidden) fit();
      },
      { signal },
    );
    window.addEventListener(
      "pagehide",
      (event) => {
        if (!event.persisted)
          engines.current.forEach((engine) => engine.dispose());
      },
      { signal },
    );
    document.addEventListener(
      "keydown",
      (event) => {
        if (event.key === "Escape" && tools.current?.open) {
          event.preventDefault();
          event.stopImmediatePropagation();
          tools.current.open = false;
          tools.current.querySelector("summary")?.focus();
          return;
        }
        if (!((event.ctrlKey || event.metaKey) && event.code === "KeyC"))
          return;
        const selection = window.getSelection(),
          text = selection?.toString();
        if (!text || !selection?.anchorNode) return;
        const pane = [...engines.current.values()].find((engine) =>
          engine.options.frozen.contains(selection.anchorNode),
        );
        if (!pane) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        void copyClipboard(text, () => pane.term.focus()).catch((error) =>
          setNotice(errorMessage(error)),
        );
      },
      { capture: true, signal },
    );
    document.addEventListener("pointerdown", (event) => {
      if (tools.current?.open && event.target instanceof Node && !tools.current.contains(event.target))
        tools.current.open = false;
    }, { signal });
    return () => {
      unsubscribe();
      abort.abort();
      document.body.classList.remove(
        "embedded",
        "mobile-terminal",
        "compact-terminal",
        "compact-chrome",
        "dragging",
      );
    };
  }, [embedded]);
  const upload = useCallback(
    async (files: File[], pane = engines.current.get(activeRef.current)) => {
      const data = infoRef.current;
      if (!data?.files_available)
        throw new Error("Uploads unavailable for this workspace.");
      if (!pane?.connected)
        throw new Error("Connect to the terminal before uploading.");
      setUploading((old) => old + 1);
      try {
        for (const file of files) {
          if (file.size > 25 * 1024 * 1024)
            throw new Error(`${file.name}: exceeds 25 MiB`);
          setNotice(`Uploading ${file.name}…`);
          const form = new FormData();
          form.append(
            "file",
            file,
            file.name || `screenshot-${Date.now()}.png`,
          );
          const attachment = await json<Attachment>(base + "/attachments", {
            method: "POST",
            body: form,
          });
          try {
            pane.paste(quote(attachment.path) + " ");
            setNotice(
              `${attachment.name} uploaded. Path inserted; press Enter when ready.`,
            );
          } catch (error) {
            setNotice(`Uploaded to ${attachment.path}. ${errorMessage(error)}`);
            return;
          }
        }
      } finally {
        setUploading((old) => old - 1);
      }
    },
    [base],
  );
  useEffect(() => {
    const abort = new AbortController(),
      signal = abort.signal;
    let depth = 0;
    const run = (files: File[], pane?: Engine) => {
      void upload(files, pane).catch((error) => setNotice(errorMessage(error)));
    };
    document.addEventListener(
      "dragenter",
      (event) => {
        if (Array.from(event.dataTransfer?.types || []).includes("Files")) {
          event.preventDefault();
          depth++;
          document.body.classList.add("dragging");
        }
      },
      { signal },
    );
    document.addEventListener(
      "dragover",
      (event) => {
        if (Array.from(event.dataTransfer?.types || []).includes("Files"))
          event.preventDefault();
      },
      { signal },
    );
    document.addEventListener(
      "dragleave",
      () => {
        if (--depth <= 0) document.body.classList.remove("dragging");
      },
      { signal },
    );
    document.addEventListener(
      "drop",
      (event) => {
        const files = Array.from(event.dataTransfer?.files || []);
        if (!files.length) return;
        event.preventDefault();
        depth = 0;
        document.body.classList.remove("dragging");
        const pane = [...engines.current.values()].find(
          (engine) =>
            event.target instanceof Node &&
            engine.options.pane.contains(event.target),
        );
        run(files, pane);
      },
      { signal },
    );
    document.addEventListener(
      "paste",
      (event) => {
        const files = Array.from(event.clipboardData?.items || [])
          .filter((item) => item.kind === "file")
          .map((item) => item.getAsFile())
          .filter((file): file is File => file !== null);
        if (!files.length) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        run(files);
      },
      { capture: true, signal },
    );
    return () => abort.abort();
  }, [upload]);
  function find(back = false, value = query) {
    const addon = current()?.search;
    if (!value) {
      addon?.clearDecorations();
      return;
    }
    const options = {
      decorations: {
        matchBackground: "#66512c",
        activeMatchBackground: "#b7a0ff",
        matchOverviewRuler: "#66512c",
        activeMatchColorOverviewRuler: "#b7a0ff",
      },
    };
    if (back) addon?.findPrevious(value, options);
    else addon?.findNext(value, options);
  }
  function close() {
    setDialog(null);
    setPreviewPath(undefined);
  }
  const specs: PaneSpec[] = info
    ? [
        {
          id: "agent",
          url: info.terminal_url,
          label: kind === "project" ? "Project shell" : "Agent",
        },
        ...(split && info.shell_url
          ? [{ id: "shell", url: info.shell_url, label: "Companion shell" }]
          : []),
      ]
    : [];
  return (
    <>
      <header>
        <a href="/" title="Back to AgentDeck">
          ◈ agentdeck
        </a>
        <span id="identity">
          {info ? info.tmux_session + " · " + info.target : "Connecting…"}
        </span>
        <span id="connection" role="status">
          {state?.connected
            ? "Connected"
            : state
              ? "Reconnecting…"
              : "Connecting"}
        </span>
      </header>
      <nav aria-label="Terminal tools">
        <span
          id="compact-status"
          className={state?.connected ? "connected" : ""}
          role="status"
          aria-label={
            state?.connected ? "Terminal connected" : "Terminal reconnecting"
          }
          title={state?.status || "Connecting"}
        />
        <button
          id="upload"
          disabled={!info?.files_available || uploading > 0}
          onClick={() => file.current?.click()}
        >
          Attach files
        </button>
        <input
          ref={file}
          id="file-input"
          type="file"
          multiple
          hidden
          onChange={(event) => {
            const files = Array.from(event.target.files || []);
            event.target.value = "";
            void upload(files).catch((error) => setNotice(errorMessage(error)));
          }}
        />
        <button
          id="files"
          disabled={!info?.files_available}
          onClick={() => setDialog("files")}
        >
          Files
        </button>
        <a id="desktop" className="button" href={info?.desktop_uri}>
          Open in terminal
        </a>
        <details id="terminal-tools" className="action-menu" ref={tools}>
          <summary id="terminal-tools-summary">
            {state?.paused ? "Paused · Tools" : "Tools"}
          </summary>
          <div
            className="action-menu-panel"
            onClick={(event) => {
              if (
                event.target instanceof Element &&
                event.target.closest("button,a") &&
                tools.current
              )
                tools.current.open = false;
            }}
          >
            <a id="compact-desktop" className="button" href={info?.desktop_uri}>
              Open in terminal
            </a>
            <button
              id="compact-upload"
              disabled={!info?.files_available || uploading > 0}
              onClick={() => file.current?.click()}
            >
              Attach files
            </button>
            <button
              id="compact-files"
              disabled={!info?.files_available}
              onClick={() => setDialog("files")}
            >
              Files
            </button>
            <span id="compact-workspace">{info?.workdir}</span>
            <button
              id="search-conversations"
              disabled={!info}
              onClick={() => info && onShared("search", info)}
            >
              Search saved conversations
            </button>
            {kind === "session" && (
              <button
                id="saved-conversations"
                disabled={!info}
                onClick={() => info && onShared("saved", info)}
              >
                Saved conversations
              </button>
            )}
            <button
              id="review"
              disabled={!info}
              onClick={() => info && onShared("review", info)}
            >
              Review changes
            </button>
            <button
              id="find"
              onClick={() => {
                setSearch(true);
                requestAnimationFrame(() => searchInput.current?.focus());
              }}
            >
              Find in terminal
            </button>
            <button
              id="history"
              disabled={!state}
              onClick={() => showHistory(active)}
            >
              Session history
            </button>
            <button
              id="desktop-setup"
              disabled={!info}
              onClick={() => setDialog("desktop")}
            >
              Terminal connection setup
            </button>
            <a
              className="button"
              href="/#projects"
              aria-label="Open project MCP settings"
            >
              Project MCP settings
            </a>
            <button
              id="shell"
              disabled={!info?.shell_url}
              onClick={() => {
                setSplit((old) => !old);
                if (split) setActive("agent");
              }}
            >
              {split ? "Hide shell" : "Split shell"}
            </button>
            <button id="preferences" onClick={() => setDialog("appearance")}>
              Appearance
            </button>
            <button
              id="pause"
              aria-pressed={state?.paused || false}
              onClick={() => current()?.freeze(!state?.paused)}
            >
              {state?.paused ? "Resume view" : "Pause view"}
            </button>
            <button
              id="bottom"
              onClick={() => {
                current()?.freeze(false);
                current()?.term.scrollToBottom();
              }}
            >
              Jump to live
            </button>
            <button
              id="reconnect"
              title="Reconnect this view and redraw the terminal; the agent keeps running"
              onClick={() => {
                const engine = current();
                if (engine?.paused) engine.freeze(false);
                void engine?.connect();
              }}
            >
              Reconnect view
            </button>
          </div>
        </details>
      </nav>
      <div id="notice" role="status" hidden={!notice}>
        {notice}
      </div>
      {search && (
        <div id="searchbar">
          <input
            id="search-input"
            ref={searchInput}
            placeholder="Find in terminal"
            aria-label="Find in terminal"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              find(false, event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") find(event.shiftKey);
              if (event.key === "Escape") {
                setSearch(false);
                current()?.search.clearDecorations();
              }
            }}
          />
          <button id="previous" onClick={() => find(true)}>
            ↑
          </button>
          <button id="next" onClick={() => find()}>
            ↓
          </button>
          <span id="matches">{matches}</span>
          <button
            id="search-close"
            onClick={() => {
              setSearch(false);
              current()?.search.clearDecorations();
              current()?.term.focus();
            }}
          >
            Close
          </button>
        </div>
      )}
      <main id="workspace">
        {info &&
          specs.map((spec) => (
            <Pane
              key={spec.id}
              spec={spec}
              prefs={prefs}
              active={active === spec.id}
              info={info}
              callbacks={callbacks}
            />
          ))}
      </main>
      <div id="terminal-keybar" role="group" aria-label="Terminal keys">
        {(Object.keys(keys) as (keyof typeof keys)[]).map((key) => (
          <button
            key={key}
            data-terminal-key={key}
            aria-label={labels[key][1]}
            disabled={!state?.connected || state.paused}
            onPointerDown={(event) => event.preventDefault()}
            onClick={() => {
              const engine = current();
              if (!engine) return;
              let text = keys[key];
              if (
                engine.term.modes.applicationCursorKeysMode &&
                /^\x1b\[[ABCD]$/.test(text)
              )
                text = text.replace("[", "O");
              engine.input(text);
              engine.term.scrollToBottom();
              engine.term.focus();
            }}
          >
            {labels[key][0]}
          </button>
        ))}
      </div>
      <footer>
        <span id="workspace-path">{info?.workdir}</span>
        <span>
          Drop files or paste a screenshot · Ctrl+Shift+F history · Ctrl+Shift+V
          paste
        </span>
      </footer>
      {dialog === "appearance" && (
        <Appearance
          prefs={prefs}
          onPrefs={(prefs) => {
            setPrefs(prefs);
            localStorage.setItem("adk-terminal-prefs", JSON.stringify(prefs));
          }}
          onClose={close}
        />
      )}
      {dialog === "history" && engines.current.get(historyPane) && (
        <HistoryDialog
          url={engines.current.get(historyPane)!.options.url}
          shell={historyPane !== "agent"}
          onClose={close}
        />
      )}
      {dialog === "files" && info && (
        <WorkspaceFiles
          base={base}
          info={info}
          initialPath={previewPath}
          onClose={close}
          onInsert={(text) => {
            const engine = current();
            if (!engine)
              throw new Error(
                "Terminal disconnected. Reconnect before inserting a path.",
              );
            engine.paste(text);
          }}
        />
      )}
      {dialog === "desktop" && info && (
        <Desktop info={info} onClose={close} onNotice={setNotice} />
      )}
    </>
  );
}
