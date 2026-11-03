// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// The BBS's read-only web interface.  Titles, bodies and callsigns arrive off
// the air, so everything goes in with textContent, never innerHTML.

"use strict";

const TYPES = { P: "Personal", B: "Bulletin", T: "Traffic" };
const STATUSES = { N: "New", Y: "Read", F: "Forwarded", H: "Held", K: "Killed" };

const $ = (id) => document.getElementById(id);

// el makes an element with text, and a class if given.
function el(tag, text, className) {
  const e = document.createElement(tag);
  if (text !== undefined && text !== null) e.textContent = String(text);
  if (className) e.className = className;
  return e;
}

function row(cells) {
  const tr = document.createElement("tr");
  for (const c of cells) tr.append(c instanceof Node ? c : el("td", c));
  return tr;
}

function fmtTime(iso) {
  if (!iso) return "never";
  const d = new Date(iso);
  return d.toISOString().replace("T", " ").replace(/:\d\d\.\d+Z$|:\d\dZ$/, "Z");
}

function fmtDuration(seconds) {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  return `${m}m`;
}

async function getJSON(path) {
  const r = await fetch(path, { headers: { Accept: "application/json" } });
  if (!r.ok) throw new Error(`${path}: ${r.status} ${(await r.text()).trim()}`);
  return r.json();
}

function settings(dl, pairs) {
  dl.replaceChildren();
  for (const [k, v] of pairs) {
    if (v === undefined || v === null || v === "") continue;
    dl.append(el("dt", k), el("dd", v));
  }
}

let info = null;

async function loadInfo() {
  info = await getJSON("api/info");
  $("bbs-call").textContent = info.call;
  document.title = `${info.call} - Samoyed BBS`;
  $("bbs-info").textContent = `${info.sid} · up ${fmtDuration(info.uptimeSeconds)}`;
}

function partnerCards(container, partners, detailed) {
  container.replaceChildren();
  if (!partners.length) {
    container.append(el("p", "No partners configured.", "empty"));
    return;
  }
  for (const p of partners) {
    const card = el("div", null, "partner");
    const h = el("h3", p.call);
    if (!p.configured) h.append(el("span", " (no longer configured)", "muted"));
    card.append(h);

    const totals = el("div", null, "totals");
    for (const [label, n, cls] of [
      ["Pending", p.pending, "state-pending"],
      ["Forwarded", p.forwarded, "state-forwarded"],
      ["Refused", p.refused, ""],
      ["Held", p.held, "state-held"],
    ]) {
      const s = el("span", `${label}: `);
      s.append(el("strong", n, n ? cls : ""));
      totals.append(s);
    }
    totals.append(el("span", `Last tried: ${fmtTime(p.lastTry)}`, "muted"));
    card.append(totals);

    if (detailed && p.configured) {
      const dl = el("dl", null, "settings");
      const c = p.connect;
      settings(dl, [
        ["Covers", (p.covers || []).join(" ") || "-"],
        ["Floods", (p.floods || []).join(" ") || "-"],
        ["Calls", c ? `port ${c.port} to ${c.to}${c.via ? " via " + c.via.join(",") : ""}, every ${fmtDuration(c.everySeconds)}` : "No: it calls in"],
      ]);
      card.append(dl);
    }
    container.append(card);
  }
}

async function showOverview() {
  const [partners, messages] = await Promise.all([
    getJSON("api/partners"),
    getJSON("api/messages?status=N,Y,F,H,K"),
  ]);

  settings($("settings"), [
    ["Callsign", info.call],
    ["Hierarchical route", info.hroute],
    ["System ID", info.sid],
    ["FBB protocol", info.fbb ? "on" : "off"],
    ["AGW server", info.agw],
    ["Version", info.version],
    ["Started", fmtTime(info.started)],
  ]);

  // Counts by type and status.
  const counts = {};
  for (const m of messages) {
    counts[m.type] ??= {};
    counts[m.type][m.status] = (counts[m.type][m.status] || 0) + 1;
  }
  const head = $("counts").tHead.rows[0];
  head.replaceChildren(el("th", "Type"));
  for (const s of Object.values(STATUSES)) head.append(el("th", s, "num"));
  head.append(el("th", "Total", "num"));
  const body = $("counts").tBodies[0];
  body.replaceChildren();
  for (const [t, name] of Object.entries(TYPES)) {
    const cells = [el("td", name)];
    let total = 0;
    for (const s of Object.keys(STATUSES)) {
      const n = (counts[t] && counts[t][s]) || 0;
      total += n;
      cells.push(el("td", n, `num ${n ? "status-" + s : "muted"}`));
    }
    cells.push(el("td", total, "num"));
    const tr = row(cells);
    tr.className = "clickable";
    tr.addEventListener("click", () => { location.hash = `#/messages?type=${t}&status=N,Y,F,H,K`; });
    body.append(tr);
  }

  partnerCards($("overview-partners"), partners, false);
}

async function showMessages(params) {
  const form = $("filters");
  for (const name of ["type", "status", "from", "to"]) form.elements[name].value = params.get(name) || "";

  const messages = await getJSON("api/messages?" + params.toString());
  const body = $("messages").tBodies[0];
  body.replaceChildren();
  for (const m of messages) {
    const tr = row([
      el("td", m.number, "num"),
      el("td", m.type),
      el("td", STATUSES[m.status] || m.status, "status-" + m.status),
      el("td", m.from, "mono"),
      el("td", m.to, "mono"),
      el("td", m.title),
      el("td", m.size, "num"),
      el("td", fmtTime(m.created)),
      el("td", m.mid, "mono"),
    ]);
    tr.className = "clickable";
    tr.addEventListener("click", () => { location.hash = `#/messages/${m.number}`; });
    body.append(tr);
  }
  $("messages-empty").hidden = messages.length > 0;
}

async function showMessage(n) {
  const m = await getJSON(`api/messages/${encodeURIComponent(n)}`);

  $("message-title").textContent = `#${m.number}: ${m.title}`;
  settings($("message-headers"), [
    ["Type", TYPES[m.type] || m.type],
    ["Status", STATUSES[m.status] || m.status],
    ["From", m.from],
    ["To", m.to],
    ["Date", fmtTime(m.created)],
    ["ID", m.mid],
    ["Size", `${m.size} bytes`],
    ["Path", m.path.join(" ")],
  ]);

  $("message-rlines").hidden = m.rlines.length === 0;
  $("message-rlines").querySelector("pre").textContent = m.rlines.join("\n");
  $("message-text").textContent = m.text;

  const body = $("message-forwarding").tBodies[0];
  body.replaceChildren();
  for (const f of m.forwarding) {
    body.append(row([
      el("td", f.partner, "mono"),
      el("td", f.state, "state-" + f.state),
      el("td", f.attempts, "num"),
      el("td", fmtTime(f.lastTry)),
    ]));
  }
  $("message-forwarding").hidden = m.forwarding.length === 0;
  $("message-forwarding-empty").hidden = m.forwarding.length > 0;
}

async function showPartners() {
  partnerCards($("partners"), await getJSON("api/partners"), true);
}

// route shows the view the location's hash names.
async function route() {
  const [path, query] = (location.hash.replace(/^#/, "") || "/").split("?");
  const params = new URLSearchParams(query || "");
  const parts = path.split("/").filter(Boolean);

  let view = "overview";
  let show = showOverview;
  if (parts[0] === "messages" && parts[1]) {
    view = "message";
    show = () => showMessage(parts[1]);
  } else if (parts[0] === "messages") {
    view = "messages";
    show = () => showMessages(params);
  } else if (parts[0] === "partners") {
    view = "partners";
    show = showPartners;
  }

  for (const s of document.querySelectorAll("main > section")) s.hidden = s.id !== `view-${view}`;
  const tab = view === "message" ? "messages" : view;
  for (const a of document.querySelectorAll("nav a")) {
    if (a.dataset.view === tab) a.setAttribute("aria-current", "page");
    else a.removeAttribute("aria-current");
  }

  $("error").hidden = true;
  try {
    if (!info) await loadInfo();
    await show();
  } catch (e) {
    $("error").textContent = e.message;
    $("error").hidden = false;
  }
}

$("filters").addEventListener("submit", (e) => {
  e.preventDefault();
  const params = new URLSearchParams();
  for (const [k, v] of new FormData(e.target)) if (String(v).trim()) params.set(k, String(v).trim());
  location.hash = "#/messages" + (params.size ? "?" + params : "");
});

window.addEventListener("hashchange", route);
route();
