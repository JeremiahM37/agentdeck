/* agentdeck PWA — vanilla ES module, no build step */
const $ = (s, el = document) => el.querySelector(s);
const COLUMNS = ["backlog", "queued", "running", "review", "done", "failed"];
const state = {
  tab: "board", tasks: [], projects: [], targets: [], approvals: [],
  sheet: null,            // {kind:'task', id} | {kind:'new'} | null
  taskES: null, taskEvents: [], taskDiff: null, diffOpen: false,
  deckPanes: new Map(),   // taskId -> {el, es}: persisted deck panes/streams
};

/* ---------- api ---------- */
function authToken() { return localStorage.getItem("adk-token") || ""; }
// EventSource can't set headers, and fetch needs the bearer too — thread the
// token (when the server runs in token-auth mode) through both surfaces.
function withToken(url) {
  const t = authToken();
  return t ? url + (url.includes("?") ? "&" : "?") + "token=" + encodeURIComponent(t) : url;
}
async function api(path, opts = {}) {
  const headers = { "Content-Type": "application/json" };
  if (authToken()) headers.Authorization = "Bearer " + authToken();
  const r = await fetch(`/api${path}`, {
    headers, ...opts, body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if (r.status === 401) {
    const t = prompt("This agentdeck requires an access token:", authToken());
    if (t !== null) { localStorage.setItem("adk-token", t); location.reload(); }
    throw new Error("unauthorized");
  }
  if (!r.ok) {
    let msg = r.statusText;
    try { msg = (await r.json()).detail || msg; } catch {}
    throw new Error(msg);
  }
  return r.status === 204 ? null : r.json();
}

function toast(msg, err = false) {
  const t = document.createElement("div");
  t.className = "toast" + (err ? " err" : "");
  t.textContent = msg;
  $("#toasts").appendChild(t);
  setTimeout(() => t.remove(), 4200);
}

/* ---------- live board stream ---------- */
let es, sseConnectedOnce = false;
function connectSSE() {
  es = new EventSource(withToken("/api/stream"));
  es.onopen = () => {
    setConn(true);
    // EventSource auto-reconnects but never replays what it missed while down —
    // resync board + approvals on every (re)connect after the first
    if (sseConnectedOnce) { refreshTasks(); refreshApprovals(); }
    sseConnectedOnce = true;
  };
  es.onerror = () => setConn(false);
  es.addEventListener("task", (e) => {
    const task = JSON.parse(e.data);
    refreshTasks();               // authoritative refetch (cheap at homelab scale)
    if (state.sheet?.kind === "task" && state.sheet.id === task.id) loadTaskSheet(task.id, true);
  });
  es.addEventListener("approval", () => { refreshApprovals(); });
  es.addEventListener("task_deleted", (e) => {
    const { id } = JSON.parse(e.data);
    if (state.sheet?.kind === "task" && state.sheet.id === id) closeSheet();
    refreshTasks();
  });
}
function setConn(on) {
  $("#conn-led").className = "led " + (on ? "led-on" : "led-err");
  $("#conn-label").textContent = on ? "LIVE" : "RECONNECTING";
}

/* ---------- data ---------- */
async function refreshTasks() {
  state.tasks = await api("/tasks");
  if (state.tab === "board") ($("#board") ? renderColumns() : renderBoard());
  if (state.tab === "deck") renderDeck();
}
async function refreshApprovals() {
  state.approvals = await api("/approvals?status=pending");
  const b = $("#appr-badge");
  b.hidden = state.approvals.length === 0;
  b.textContent = state.approvals.length;
  if (state.tab === "approvals") renderApprovals();
  if (state.sheet?.kind === "task") renderSheet();
}
async function refreshMeta() {
  [state.projects, state.targets] = await Promise.all([api("/projects"), api("/targets")]);
  if (state.tab === "targets") renderTargets();
}

/* ---------- board ---------- */
function fmtCost(c) { return c == null ? "" : `$${(+c).toFixed(3)}`; }
function attachMic(btn, input) {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) { btn.style.display = "none"; return; }
  btn.onclick = () => {
    const rec = new SR();
    rec.lang = "en-US"; rec.interimResults = false;
    btn.classList.add("rec");
    rec.onresult = (e) => { input.value = (input.value + " " + e.results[0][0].transcript).trim(); };
    rec.onend = () => btn.classList.remove("rec");
    rec.onerror = () => { btn.classList.remove("rec"); toast("voice input failed", true); };
    rec.start();
  };
}

function card(t) {
  const el = document.createElement("div");
  el.className = `card s-${t.status}`;
  el.draggable = true;
  el.ondragstart = (e) => e.dataTransfer.setData("text/adk-task", JSON.stringify(
    { id: t.id, status: t.status }));
  const ds = Array.isArray(t.attempt?.diff_stat) ? t.attempt.diff_stat : [];
  const adds = ds.reduce((a, f) => a + (f.additions || 0), 0);
  const dels = ds.reduce((a, f) => a + (f.deletions || 0), 0);
  el.innerHTML = `
    <div class="t"></div>
    <div class="meta">
      <span class="chip">${esc(t.project_name)}</span>
      <span class="chip tgt">⌁ ${esc(t.target_name)}</span>
      ${t.status === "running" ? '<span class="working"><i></i><i></i><i></i></span>' : ""}
      ${ds.length ? `<span class="chip ds">+${adds} <b>−${dels}</b></span>` : ""}
      ${t.attempt?.result?.cost_usd != null ? `<span class="chip cost">${fmtCost(t.attempt.result.cost_usd)}</span>` : ""}
      ${t.attempt?.verify?.cmd ? (t.attempt.verify.rc === 0
        ? '<span class="chip ds" title="auto-verify passed">✓ verified</span>'
        : '<span class="chip" style="color:var(--red);border-color:rgba(255,93,93,.4)" title="auto-verify FAILED">✗ verify</span>') : ""}
      ${t.priority >= 3 ? '<span class="chip" style="color:var(--amber)">▲ high</span>' : ""}
      ${t.agent && t.agent !== "claude" ? `<span class="chip" style="color:var(--cyan)">${esc(t.agent)}</span>` : ""}
      ${t.created_by === "agent" ? `<span class="chip" style="color:var(--violet)" title="filed by an agent (parent #${t.parent_task_id})">🤖 by agent</span>` : ""}
      ${t.created_by === "reviewer-gate" ? '<span class="chip" style="color:var(--cyan)">⚖ reviewer</span>' : ""}
      ${t.attempt?.result?.review ? (t.attempt.result.review.verdict === "APPROVE"
        ? '<span class="chip ds" title="reviewer approved">⚖ approved</span>'
        : '<span class="chip" style="color:var(--amber);border-color:rgba(255,180,84,.4)" title="reviewer requests changes">⚖ changes</span>') : ""}
      ${(t.attempts || []).length > 1 ? `<span class="chip" style="color:var(--violet)" title="${t.attempts.length} attempts (retries / follow-ups / A/B)">⑂ ×${t.attempts.length}</span>` : ""}
    </div>`;
  $(".t", el).textContent = t.title;
  el.onclick = () => openTaskSheet(t.id);
  return el;
}
function renderBoard() {
  const main = $("#view");
  main.innerHTML = `
    <div id="quickbar">
      <select id="qb-project" title="project"></select>
      <input id="qb-input" placeholder="⚡ describe it, hit ⏎ — instant dispatch" autocomplete="off">
      <button id="qb-mic" title="voice">🎤</button>
      <input id="qb-filter" placeholder="⌕ filter" autocomplete="off">
    </div>
    <div id="board"></div>`;
  $("#qb-filter").value = state.filter || "";
  $("#qb-filter").oninput = (e) => { state.filter = e.target.value; renderColumns(); };
  const sel = $("#qb-project");
  sel.innerHTML = state.projects.map((p) => `<option value="${p.id}">${esc(p.name)}</option>`).join("");
  sel.value = localStorage.getItem("adk-quickproj") || (state.projects[0]?.id ?? "");
  sel.onchange = () => localStorage.setItem("adk-quickproj", sel.value);
  $("#qb-input").onkeydown = async (e) => {
    if (e.key !== "Enter" || !e.target.value.trim()) return;
    const text = e.target.value.trim();
    e.target.value = "";
    try {
      const t = await api("/tasks", { method: "POST", body: {
        project_id: +sel.value, title: text.slice(0, 70), prompt: text } });
      await api(`/tasks/${t.id}/dispatch`, { method: "POST", body: {} });
      toast("Dispatched ▶ " + text.slice(0, 40));
      refreshTasks();
    } catch (err) { toast(err.message, true); }
  };
  attachMic($("#qb-mic"), $("#qb-input"));
  renderColumns();
}

function renderColumns() {
  const board = $("#board");
  if (!board) return;
  board.innerHTML = "";
  const f = (state.filter || "").trim().toLowerCase();
  const visible = f ? state.tasks.filter((t) =>
    `${t.title} ${t.project_name} ${t.target_name}`.toLowerCase().includes(f))
    : state.tasks;
  for (const col of COLUMNS) {
    const items = visible.filter((t) => t.status === col);
    const c = document.createElement("div");
    c.className = `col s-${col}`;
    c.innerHTML = `
      <div class="col-head"><span class="dot"></span>${col}<span class="cnt">${items.length}</span></div>
      <div class="col-body"></div>`;
    c.ondragover = (e) => { e.preventDefault(); c.classList.add("dropok"); };
    c.ondragleave = () => c.classList.remove("dropok");
    c.ondrop = async (e) => {
      e.preventDefault(); c.classList.remove("dropok");
      const data = e.dataTransfer.getData("text/adk-task");
      if (!data) return;
      const { id, status } = JSON.parse(data);
      try {
        if (col === "queued" && ["backlog", "failed", "cancelled"].includes(status))
          await api(`/tasks/${id}/dispatch`, { method: "POST", body: {} });
        else if (col === "done" && status === "review")
          await api(`/tasks/${id}/complete`, { method: "POST" });
        else if (col === "cancelled" || (col === "backlog" && status === "backlog")) return;
        else return toast(`${status} → ${col}: not a thing. Drag to queued (dispatch) or done (complete).`, true);
        refreshTasks();
      } catch (err) { toast(err.message, true); }
    };
    const body = $(".col-body", c);
    if (!items.length) body.innerHTML = '<div class="col-empty">— empty —</div>';
    items.forEach((t) => {
      try { body.appendChild(card(t)); }   // one bad card must never blank the board
      catch (err) { console.error("card render failed", t?.id, err); }
    });
    board.appendChild(c);
  }
  // phone: land on the first column that has work, not an empty backlog
  if (!renderBoard._scrolled && board.scrollWidth > board.clientWidth) {
    const busy = [...board.children].find((c) => c.querySelector(".card"));
    if (busy) { board.scrollLeft = busy.offsetLeft - 14; renderBoard._scrolled = true; }
  }
}

/* ---------- deck view (desktop multi-pane cockpit) ---------- */
// panes are reconciled incrementally: streams persist across task updates so a
// status change elsewhere never tears down and reconnects every pane's SSE
// (that thrash risked dropping mid-stream events under concurrent load).
function closeDeckStreams() {
  for (const p of state.deckPanes.values()) p.es.close();
  state.deckPanes.clear();
}
function makePaneLogger(log) {
  return (e) => {
    const p = e.payload || {};
    const line = document.createElement("div");
    line.className = "pane-line";
    line.textContent =
      e.type === "text" ? p.text :
      e.type === "tool_use" ? `▸ ${p.name} ${snippet(p.input)}` :
      e.type === "tool_result" ? `↳ ${(p.content || "").slice(0, 80)}` :
      e.type === "verify" ? `verify ${p.rc === 0 ? "PASS" : "FAIL"}` :
      e.type === "result" ? `✔ ${p.result || ""}` : e.type;
    log.appendChild(line);
    while (log.children.length > 40) log.firstChild.remove();
    log.scrollTop = log.scrollHeight;
  };
}
function renderDeck() {
  const main = $("#view");
  const active = state.tasks
    .filter((t) => ["running", "review", "queued"].includes(t.status))
    .slice(0, 16);
  let deck = $("#deck");
  if (!active.length) {
    closeDeckStreams();
    main.innerHTML = '<div class="hint">Nothing live. Dispatch tasks and watch them run here, side by side.</div>';
    return;
  }
  if (!deck) { main.innerHTML = '<div id="deck"></div>'; deck = $("#deck"); }
  const activeIds = new Set(active.map((t) => t.id));
  // remove panes whose task left the active set (close their stream)
  for (const [id, p] of [...state.deckPanes]) {
    if (!activeIds.has(id)) { p.es.close(); p.el.remove(); state.deckPanes.delete(id); }
  }
  // add or update
  for (const t of active) {
    let p = state.deckPanes.get(t.id);
    if (!p) {
      const pane = document.createElement("div");
      pane.innerHTML = `
        <div class="pane-head">
          <span class="statpill"></span>
          <span class="pane-title"></span>
          <span class="pane-sub">⌁ ${esc(t.target_name)}</span>
        </div>
        <div class="pane-log"></div>`;
      $(".pane-title", pane).textContent = t.title;
      $(".pane-head", pane).onclick = () => openTaskSheet(t.id);
      deck.appendChild(pane);
      const log = $(".pane-log", pane);
      const add = makePaneLogger(log);
      const es = new EventSource(withToken(`/api/tasks/${t.id}/stream`));
      es.addEventListener("agent_event", (e) => add(JSON.parse(e.data)));
      p = { el: pane, es };
      state.deckPanes.set(t.id, p);
      api(`/tasks/${t.id}/events`).then((evs) => evs.slice(-15).forEach(add));
    }
    // update header status in place (no stream churn)
    p.el.className = `pane s-${t.status}`;
    $(".statpill", p.el).textContent = t.status;
  }
}

/* ---------- approvals tab ---------- */
function renderApprovals() {
  const main = $("#view");
  if (!state.approvals.length) {
    main.innerHTML = '<div class="hint">No pending approvals.<br>When an agent needs permission, it shows up here — and pings your phone.</div>';
    return;
  }
  main.innerHTML = '<div class="list"></div>';
  const list = $(".list", main);
  for (const a of state.approvals) list.appendChild(approvalCard(a));
}
function approvalCard(a, inline = false) {
  const el = document.createElement("div");
  el.className = "rowcard";
  el.innerHTML = `
    <h3>✋ ${esc(a.tool_name)} <span style="color:var(--ink-faint)">wants to run</span></h3>
    <div class="sub">task #${a.task_id} · ${esc(a.task_title || "")}</div>
    <pre></pre>
    <div class="btnrow">
      <button class="b ok grow">Approve</button>
      <button class="b ok" title="approve and never ask again for this pattern in this project">∞ Always</button>
      <button class="b no grow">Deny</button>
    </div>`;
  $("pre", el).textContent = JSON.stringify(a.input, null, 2).slice(0, 1200);
  $(".ok", el).onclick = () => decide(a.id, "approved");
  el.querySelectorAll(".ok")[1].onclick = () => decide(a.id, "approved", "", true);
  $(".no", el).onclick = async () => {
    const note = prompt("Reason (sent back to the agent):", "not safe, find another way") || "";
    decide(a.id, "denied", note);
  };
  return el;
}
async function decide(id, decision, note = "", always = false) {
  try {
    await api(`/approvals/${id}/decision`, { method: "POST",
      body: { decision, note, always_allow: always } });
    toast(decision === "approved" ? "Approved — agent continuing" : "Denied — agent notified");
    refreshApprovals();
  } catch (e) { toast(e.message, true); }
}

/* ---------- targets tab ---------- */
async function renderTargets() {
  const main = $("#view");
  main.innerHTML = '<div class="list"></div>';
  const list = $(".list", main);
  // fetch BEFORE building the form — a late response must never clobber typed input
  let settings = {};
  try { settings = await api("/settings"); } catch {}
  for (const t of state.targets) {
    const el = document.createElement("div");
    el.className = "rowcard";
    const led = t.status === "online" ? "led-on" : t.status === "offline" ? "led-err" : "led-warn";
    const info = safeParse(t.info_json);
    el.innerHTML = `
      <h3><span class="led ${led}"></span> ${esc(t.name)}</h3>
      <div class="sub">${esc(t.kind)}${t.host ? " · " + esc(t.user + "@" + t.host) : ""} · slots ${t.max_concurrent}${t.sandbox ? " · 🏖 sandbox" : ""}</div>
      ${info.claude ? `<div class="sub" style="margin-top:4px">claude ${esc(info.claude)} · ${esc(info.git || "")}</div>` : ""}
      <div class="btnrow"><button class="b">Probe</button></div>`;
    $("button", el).onclick = async (ev) => {
      ev.target.textContent = "Probing…";
      try { await api(`/targets/${t.id}/check`, { method: "POST" }); await refreshMeta(); }
      catch (e) { toast(e.message, true); }
    };
    list.appendChild(el);
  }
  const statsCard = document.createElement("div");
  statsCard.className = "rowcard";
  statsCard.innerHTML = '<h3>Spend</h3><div class="sub">loading…</div>';
  api("/stats").then((s) => {
    statsCard.innerHTML = `<h3>Spend</h3>
      <div class="sub">$${s.total_cost_usd.toFixed(2)} all-time · $${s.last_7d_usd.toFixed(2)} last 7d · ${s.tasks_done} tasks done</div>
      ${s.by_project.slice(0, 5).map((p) =>
        `<div class="sub" style="margin-top:3px">${esc(p.name)} <span style="color:var(--amber)">$${p.cost_usd.toFixed(2)}</span></div>`).join("")}`;
  }).catch(() => { statsCard.querySelector(".sub").textContent = "unavailable"; });
  list.appendChild(statsCard);

  const foot = document.createElement("div");
  foot.className = "rowcard";
  foot.innerHTML = `<h3>Notifications</h3>
    <div class="sub">Get pinged for approvals & finished tasks.</div>
    <div class="btnrow"><button class="b warn grow" id="push-btn">Enable push on this device</button></div>
    <label class="f">Discord webhook URL</label>
    <input class="f" id="s-discord" placeholder="https://discord.com/api/webhooks/…">
    <label class="f">ntfy server / topic <span style="text-transform:none;letter-spacing:0">(gets ✅/⛔ action buttons)</span></label>
    <div style="display:flex;gap:8px">
      <input class="f" id="s-ntfy-server" placeholder="https://ntfy.sh" style="flex:2">
      <input class="f" id="s-ntfy-topic" placeholder="topic" style="flex:1">
    </div>
    <div class="btnrow">
      <button class="b grow" id="s-save">Save sinks</button>
      <button class="b" id="s-test">Send test</button>
    </div>`;
  $("#push-btn", foot).onclick = enablePush;
  $("#s-discord", foot).value = settings.discord_webhook || "";
  $("#s-ntfy-server", foot).value = settings.ntfy_server || "";
  $("#s-ntfy-topic", foot).value = settings.ntfy_topic || "";
  $("#s-save", foot).onclick = async () => {
    try {
      await api("/settings", { method: "PUT", body: {
        discord_webhook: $("#s-discord", foot).value.trim(),
        ntfy_server: $("#s-ntfy-server", foot).value.trim(),
        ntfy_topic: $("#s-ntfy-topic", foot).value.trim() } });
      toast("Sinks saved");
    } catch (e) { toast(e.message, true); }
  };
  $("#s-test", foot).onclick = async () => {
    try { await api("/settings/test-notification", { method: "POST" }); toast("Test sent"); }
    catch (e) { toast(e.message, true); }
  };
  list.appendChild(foot);
}

/* ---------- task sheet ---------- */
async function openTaskSheet(id) {
  state.sheet = { kind: "task", id };
  state.taskEvents = [];
  state.taskDiff = null; state.diffOpen = false; state.attemptView = null;
  await loadTaskSheet(id);
  if (state.taskES) state.taskES.close();
  let taskSseOnce = false;
  state.taskES = new EventSource(withToken(`/api/tasks/${id}/stream`));
  state.taskES.onopen = () => {
    // resync the timeline on reconnect — SSE doesn't replay missed events
    if (taskSseOnce && state.sheet?.kind === "task" && state.sheet.id === id)
      loadTaskSheet(id);
    taskSseOnce = true;
  };
  state.taskES.addEventListener("agent_event", (e) => {
    state.taskEvents.push(JSON.parse(e.data));
    renderSheet();
  });
  state.taskES.addEventListener("approval", () => refreshApprovals());
}
async function loadTaskSheet(id, soft = false) {
  state.sheetTask = await api(`/tasks/${id}`);
  if (!soft || !state.taskEvents.length) {
    const q = state.attemptView ? `?attempt_n=${state.attemptView}` : "";
    state.taskEvents = (await api(`/tasks/${id}/events${q}`)).map((e) => ({
      type: e.type, payload: e.payload, seq: e.seq, attempt_n: e.attempt_n }));
  }
  renderSheet();
}
function closeSheet() {
  state.sheet = null;
  if (state.taskES) { state.taskES.close(); state.taskES = null; }
  $("#sheet").hidden = true; $("#sheet-backdrop").hidden = true;
}

function evRow(e) {
  const el = document.createElement("div");
  el.className = `ev e-${e.type}`;
  const p = e.payload || {};
  if (e.type === "init")
    el.innerHTML = `<div class="k">session start</div><div class="body dim">model <code>${esc(p.model || "?")}</code> · session <code>${esc((p.session_id || "").slice(0, 18))}</code></div>`;
  else if (e.type === "text")
    el.innerHTML = `<div class="k">claude</div><div class="body"></div>`,
    $(".body", el).textContent = p.text;
  else if (e.type === "tool_use")
    el.innerHTML = `<div class="k">tool</div><div class="body"><code>${esc(p.name)}</code> <span class="dim" style="color:var(--ink-dim)">${esc(snippet(p.input))}</span></div>`;
  else if (e.type === "tool_result")
    el.innerHTML = `<div class="k">↳ result${p.is_error ? " · ERROR" : ""}</div><div class="body dim"></div>`,
    $(".body", el).textContent = (p.content || "").slice(0, 400);
  else if (e.type === "verify")
    el.innerHTML = `<div class="k">auto-verify · ${p.rc === 0 ? "PASS ✓" : "FAIL ✗"}</div>
      <div class="body ${p.rc === 0 ? "" : ""}" style="color:${p.rc === 0 ? "var(--phos)" : "var(--red)"}"><code>${esc(p.cmd)}</code>\n${esc((p.output || "").slice(-500))}</div>`;
  else if (e.type === "result")
    el.innerHTML = `<div class="k">finished · ${esc(p.subtype)}</div><div class="body">${esc(p.result || "")}\n<span style="color:var(--ink-dim)">${p.num_turns ?? "?"} turns · ${fmtCost(p.cost_usd)} · ${p.duration_ms ? (p.duration_ms / 1000).toFixed(1) + "s" : ""}</span></div>`;
  else
    el.innerHTML = `<div class="k">${esc(e.type)}</div><div class="body dim"></div>`,
    $(".body", el).textContent = JSON.stringify(p).slice(0, 300);
  return el;
}

function renderSheet() {
  if (!state.sheet) return;
  const sheet = $("#sheet");
  sheet.hidden = false; $("#sheet-backdrop").hidden = false;
  if (state.sheet.kind === "new") return renderNewTask(sheet);
  const t = state.sheetTask;
  if (!t) return;
  sheet.innerHTML = `
    <div class="sheet-grip"><i></i></div>
    <div class="sheet-head"><h2></h2><button class="x">✕</button></div>
    <div class="statline s-${t.status}">
      <span class="statpill">${t.status}</span>
      <span>${esc(t.project_name)} → ⌁ ${esc(t.target_name)}</span>
      ${t.attempt ? `<span>attempt #${t.attempt.n}${t.attempt.branch ? " · <code>" + esc(t.attempt.branch) + "</code>" : ""}</span>` : ""}
    </div>
    <div class="btnrow" id="attempt-chips"></div>
    <div class="btnrow" id="actions"></div>
    <div id="sheet-approvals"></div>
    <div id="sheet-body"></div>`;
  if ((t.attempts || []).length > 1) {
    const chips = $("#attempt-chips", sheet);
    for (const a of t.attempts) {
      const b = document.createElement("button");
      b.className = "b" + ((state.attemptView ?? t.attempt.n) === a.n ? " ok" : "");
      b.textContent = `⑂ A${a.n}${a.model ? " · " + a.model : ""} · ${a.status}` +
        (a.cost_usd != null ? ` · ${fmtCost(a.cost_usd)}` : "");
      b.onclick = async () => {
        state.attemptView = a.n; state.taskEvents = []; state.taskDiff = null;
        await loadTaskSheet(t.id);
      };
      chips.appendChild(b);
    }
  }
  $("h2", sheet).textContent = t.title;
  $(".x", sheet).onclick = closeSheet;

  const actions = $("#actions", sheet);
  const act = (label, cls, fn) => {
    const b = document.createElement("button");
    b.className = `b ${cls}`; b.textContent = label; b.onclick = fn;
    actions.appendChild(b);
  };
  if (["backlog", "failed", "cancelled"].includes(t.status))
    act(t.status === "backlog" ? "▶ Dispatch" : "↻ Retry", "ok grow", () => doAction(`/tasks/${t.id}/dispatch`));
  if (["queued", "running"].includes(t.status))
    act("■ Cancel", "no", () => doAction(`/tasks/${t.id}/cancel`));
  if (t.status === "review") {
    act("✓ Mark done", "ok grow", () => doAction(`/tasks/${t.id}/complete`));
    act("↺ Request changes", "warn grow", async () => {
      const fb = prompt("What should change?");
      if (fb) doAction(`/tasks/${t.id}/followup`, { feedback: fb });
    });
  }
  if (["review", "done"].includes(t.status)) {
    act(state.diffOpen ? "▤ Timeline" : "± Diff", "", toggleDiff);
    act("▶ Replay", "", () => {
      if (state.diffOpen) return toast("switch to timeline first", true);
      const rows = [...document.querySelectorAll("#sheet .tl .ev")];
      rows.forEach((r) => (r.style.display = "none"));
      let i = 0;
      const iv = setInterval(() => {
        if (i >= rows.length) return clearInterval(iv);
        rows[i].style.display = "";
        rows[i].scrollIntoView({ block: "nearest", behavior: "smooth" });
        i++;
      }, 260);
    });
    act("⎇ Commit", "", async () => {
      const message = prompt("Commit message:", t.title);
      if (message == null) return;
      const push = confirm("Also push the branch to origin?");
      const pr = push && confirm("…and open a PR (needs gh on the target)?");
      try {
        const r = await api(`/tasks/${t.id}/commit`, { method: "POST",
          body: { message, push, pr } });
        const prStep = r.steps.find((s) => s.step === "pr");
        toast("Committed" + (push ? " + pushed" : "") +
              (prStep?.url ? ` · PR: ${prStep.url}` : ""));
      } catch (e) { toast(e.message, true); }
    });
  }
  if (["done", "failed", "cancelled"].includes(t.status) && t.attempt?.worktree_path)
    act("🧹 Clean worktree", "", async () => {
      if (!confirm("Remove the worktree(s)? Uncommitted changes are lost.")) return;
      try { await api(`/tasks/${t.id}/cleanup`, { method: "POST" });
            toast("Worktrees removed"); loadTaskSheet(t.id); }
      catch (e) { toast(e.message, true); }
    });
  if (t.status === "running" && t.attempt?.tmux_session) {
    act("⌨ Terminal", "", async () => {
      try {
        const r = await api(`/tasks/${t.id}/terminal`, { method: "POST" });
        window.open(`http://${location.hostname}:${r.port}`, "_blank");
      } catch (e) {
        const sshPrefix = t.target_kind === "ssh"
          ? `ssh -t ${t.target_user}@${t.target_host} ` : "";
        const cmd = `${sshPrefix}tmux attach -t ${t.attempt.tmux_session}`;
        toast(e.message + " — attach manually", true);
        prompt("Attach with:", cmd);
      }
    });
  }
  act("🗑 Delete", "no", async () => {
    if (!confirm(`Delete "${t.title}"? Removes its attempts, events, diffs and worktrees. Cannot be undone.`)) return;
    try { await api(`/tasks/${t.id}`, { method: "DELETE" }); toast("Task deleted"); closeSheet(); refreshTasks(); }
    catch (e) { toast(e.message, true); }
  });

  const apDiv = $("#sheet-approvals", sheet);
  state.approvals.filter((a) => a.task_id === t.id).forEach((a) => apDiv.appendChild(approvalCard(a, true)));

  const body = $("#sheet-body", sheet);
  if (state.diffOpen && state.taskDiff) renderDiff(body);
  else {
    const tl = document.createElement("div");
    tl.className = "tl";
    if (!state.taskEvents.length && t.prompt) {
      const pr = document.createElement("div");
      pr.className = "ev";
      pr.innerHTML = '<div class="k">prompt</div><div class="body dim"></div>';
      $(".body", pr).textContent = t.prompt;
      tl.appendChild(pr);
    }
    state.taskEvents.forEach((e) => tl.appendChild(evRow(e)));
    body.appendChild(tl);
  }
}

async function toggleDiff() {
  if (!state.diffOpen) {
    const q = state.attemptView ? `?attempt_n=${state.attemptView}` : "";
    try { state.taskDiff = await api(`/tasks/${state.sheet.id}/diff${q}`); }
    catch (e) { return toast(e.message, true); }
  }
  state.diffOpen = !state.diffOpen;
  renderSheet();
}
function renderDiff(body) {
  const d = state.taskDiff;
  const stats = d.stats || [];
  const head = document.createElement("div");
  head.className = "sub";
  head.style.cssText = "margin:6px 0 10px;color:var(--ink-dim);font-size:11px";
  head.textContent = `attempt #${d.attempt_n} · ${stats.length} file(s) changed`;
  body.appendChild(head);
  for (const f of d.files || []) {
    const st = stats.find((s) => s.path === f.path) || {};
    const det = document.createElement("details");
    det.className = "dfile"; det.open = (d.files.length <= 3);
    det.innerHTML = `<summary><span>${esc(f.path)}</span>
      <span class="pm"><b class="a">+${st.additions ?? "?"}</b> <b class="d">−${st.deletions ?? "?"}</b></span></summary>
      <div class="dcode"></div>`;
    const code = $(".dcode", det);
    for (const line of f.patch.split("\n")) {
      const div = document.createElement("div");
      div.textContent = line || " ";
      if (line.startsWith("+") && !line.startsWith("+++")) div.className = "dl-add";
      else if (line.startsWith("-") && !line.startsWith("---")) div.className = "dl-del";
      else if (line.startsWith("@@")) div.className = "dl-hunk";
      else if (line.startsWith("diff ") || line.startsWith("index ")) div.className = "dl-meta";
      code.appendChild(div);
    }
    body.appendChild(det);
  }
}

/* ---------- new task sheet ---------- */
function renderNewTask(sheet) {
  sheet.innerHTML = `
    <div class="sheet-grip"><i></i></div>
    <div class="sheet-head"><h2>NEW TASK</h2><button class="x">✕</button></div>
    <label class="f">Template</label>
    <select class="f" id="f-template"><option value="">— none —</option></select>
    <label class="f">Project</label>
    <select class="f" id="f-project">${state.projects.map((p) =>
      `<option value="${p.id}">${esc(p.name)} — ⌁ ${esc(p.target_name)}</option>`).join("")}</select>
    <label class="f">Title</label>
    <input class="f" id="f-title" placeholder="Add /health endpoint">
    <label class="f">Prompt — what should the agent do? <button id="f-mic" style="float:right;background:none;border:1px solid var(--line2);border-radius:2px;cursor:pointer">🎤</button></label>
    <textarea class="f" id="f-prompt" placeholder="Describe intent. Be specific about files, behavior, and how to verify."></textarea>
    <label class="f">Permissions</label>
    <select class="f" id="f-perm">
      <option value="default">Gated — ask me before running anything (push)</option>
      <option value="acceptEdits" selected>Accept edits — file changes auto-approved</option>
      <option value="plan">Plan only — no changes</option>
      <option value="bypassPermissions">Bypass — sandboxed targets only</option>
    </select>
    <label class="f">Agent</label>
    <select class="f" id="f-agent">
      <option value="claude" selected>Claude Code</option>
      <option value="codex">Codex CLI (experimental)</option>
      <option value="gemini">Gemini CLI (experimental)</option>
    </select>
    <label class="f">Model</label>
    <select class="f" id="f-model">
      <option value="">default</option><option>fable</option><option>opus</option><option>sonnet</option><option>haiku</option>
    </select>
    <label class="f">A/B second attempt (parallel, compare diffs)</label>
    <select class="f" id="f-modelb">
      <option value="">off</option><option>fable</option><option>opus</option><option>sonnet</option><option>haiku</option>
    </select>
    <label class="f">Priority</label>
    <select class="f" id="f-prio">
      <option value="1">low</option><option value="2" selected>normal</option><option value="3">high</option>
    </select>
    <div class="btnrow" style="margin-top:18px">
      <button class="b grow" id="f-save">Save to backlog</button>
      <button class="b ok grow" id="f-go">▶ Dispatch now</button>
    </div>`;
  $(".x", sheet).onclick = closeSheet;
  attachMic($("#f-mic"), $("#f-prompt"));
  api("/templates").then((tpls) => {
    const sel = $("#f-template");
    tpls.forEach((t, i) => {
      const o = document.createElement("option");
      o.value = i; o.textContent = t.name;
      sel.appendChild(o);
    });
    sel.onchange = () => {
      const t = tpls[+sel.value];
      if (!t) return;
      if (t.title) $("#f-title").value = t.title;
      if (t.prompt) $("#f-prompt").value = t.prompt;
      if (t.permission_mode) $("#f-perm").value = t.permission_mode;
      if (t.model !== undefined) $("#f-model").value = t.model;
    };
  }).catch(() => {});
  const collect = () => ({
    project_id: +$("#f-project").value,
    title: $("#f-title").value.trim(),
    prompt: $("#f-prompt").value.trim(),
    agent: $("#f-agent").value,
    permission_mode: $("#f-perm").value,
    model: $("#f-model").value,
    priority: +$("#f-prio").value,
  });
  const create = async (dispatch) => {
    const data = collect();
    if (!data.title) return toast("Title required", true);
    const modelB = $("#f-modelb").value;
    // Fable 5 is the most capable — and highest-usage — model; confirm before
    // dispatching an agent on it so it's never an accidental quota burn.
    if (dispatch && (data.model === "fable" || modelB === "fable") &&
        !confirm("Dispatch on Fable 5? It's the most capable model and uses the "
                 + "most of your Claude Code plan. Continue?")) return;
    try {
      const t = await api("/tasks", { method: "POST", body: data });
      if (dispatch) await api(`/tasks/${t.id}/dispatch`, { method: "POST",
        body: modelB ? { model_b: modelB } : {} });
      closeSheet(); refreshTasks();
      toast(dispatch ? "Dispatched ▶" : "Saved to backlog");
    } catch (e) { toast(e.message, true); }
  };
  $("#f-save").onclick = () => create(false);
  $("#f-go").onclick = () => create(true);
}

async function doAction(path, body = {}) {
  try {
    await api(path, { method: "POST", body });
    await loadTaskSheet(state.sheet.id);
    refreshTasks();
  } catch (e) { toast(e.message, true); }
}

/* ---------- push ---------- */
async function enablePush() {
  try {
    if (!("serviceWorker" in navigator)) throw new Error("no service worker support");
    const reg = await navigator.serviceWorker.ready;
    const perm = await Notification.requestPermission();
    if (perm !== "granted") throw new Error("notifications not granted");
    const keyResp = await fetch("/api/push/vapid").catch(() => null);
    let appServerKey;
    if (keyResp && keyResp.ok) appServerKey = (await keyResp.json()).key;
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      ...(appServerKey ? { applicationServerKey: urlB64(appServerKey) } : {}),
    });
    await api("/push/subscribe", { method: "POST", body: sub.toJSON() });
    toast("Push enabled on this device");
  } catch (e) { toast("Push: " + e.message, true); }
}
function urlB64(s) {
  const pad = "=".repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from([...raw].map((c) => c.charCodeAt(0)));
}

/* ---------- helpers / boot ---------- */
function esc(s) { const d = document.createElement("i"); d.textContent = s ?? ""; return d.innerHTML; }
function snippet(input) {
  if (!input) return "";
  const s = input.command || input.file_path || JSON.stringify(input);
  return String(s).slice(0, 120);
}
function safeParse(s) { try { return JSON.parse(s || "{}"); } catch { return {}; } }

function switchTab(tab) {
  state.tab = tab;
  if (tab !== "deck") closeDeckStreams();
  document.querySelectorAll(".tab").forEach((b) => b.classList.toggle("on", b.dataset.tab === tab));
  if (tab === "board") renderBoard();
  if (tab === "deck") renderDeck();
  if (tab === "approvals") renderApprovals();
  if (tab === "targets") renderTargets();
}
document.querySelectorAll(".tab").forEach((b) => (b.onclick = () => switchTab(b.dataset.tab)));
$("#fab").onclick = () => { state.sheet = { kind: "new" }; renderSheet(); };
$("#sheet-backdrop").onclick = closeSheet;
addEventListener("keydown", (e) => { if (e.key === "Escape" && state.sheet) closeSheet(); });
// mobile: a phone that was locked/backgrounded drops SSE silently — resync on
// return to foreground (shares the reconnect resync path)
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") { refreshTasks(); refreshApprovals(); }
});

if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js");

(async function boot() {
  connectSSE();
  await Promise.all([refreshMeta(), refreshTasks(), refreshApprovals()]);
  renderBoard();
  setInterval(refreshTasks, 30000);   // safety net if SSE hiccups
})();
