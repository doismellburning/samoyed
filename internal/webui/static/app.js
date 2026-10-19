// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Everything shown here that came off the air or from APRS-IS - callsigns,
// comments, whole packets - is attacker-controlled.  It only ever reaches the
// page through textContent or as a text node, never as HTML.

"use strict";

(() => {
  const MAX_PACKETS = 500;
  const STATION_MAX_AGE_MS = 2 * 60 * 60 * 1000; // As the server's DefaultStationMaxAge.

  // A few common APRS symbols, so a glance tells a car from a house.
  // https://github.com/hessu/aprs-symbol-index has the full set.
  const SYMBOLS = {
    "/!": "🚓", "/#": "⭐", "/$": "☎️", "/&": "📡", "/'": "🛩️", "/-": "🏠",
    "/<": "🏍️", "/>": "🚗", "/O": "🎈", "/R": "🚐", "/U": "🚌", "/X": "🚁",
    "/Y": "⛵", "/[": "🚶", "/^": "✈️", "/_": "🌦️", "/a": "🚑", "/b": "🚲",
    "/f": "🚒", "/j": "🚙", "/k": "🛻", "/r": "📡", "/s": "🚤", "/u": "🚚",
    "/v": "🚐", "/y": "🏠", "\\#": "⭐", "\\&": "📡", "\\-": "🏠", "\\>": "🚗",
    "\\_": "🌦️", "\\k": "🛻", "\\n": "🔺", "\\u": "🚚", "\\v": "🚐",
  };

  const $ = (id) => document.getElementById(id);

  function el(tag, props, ...children) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(props || {})) {
      if (k === "class") e.className = v;
      else if (k === "text") e.textContent = v;
      else e.setAttribute(k, v);
    }
    for (const c of children) {
      if (c != null) e.append(c); // A string becomes a text node.
    }
    return e;
  }

  function glyph(symbol) {
    if (!symbol) return "📍";
    return SYMBOLS[symbol] || SYMBOLS["/" + symbol[1]] || "📍";
  }

  function ago(iso) {
    const s = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 1000));
    if (s < 60) return s + "s ago";
    if (s < 3600) return Math.floor(s / 60) + "m ago";
    return Math.floor(s / 3600) + "h " + Math.floor((s % 3600) / 60) + "m ago";
  }

  function clock(iso) {
    return new Date(iso).toLocaleTimeString([], { hour12: false });
  }

  // ---- State ----

  const stations = new Map(); // name -> station
  let channels = new Map(); // number -> channel
  let packets = []; // oldest first
  let paused = false;
  let pendingWhilePaused = 0;
  // What the station last said about itself, and when by this page's own
  // clock, so the uptime can tick on without trusting the two clocks to agree.
  let status = null;
  let statusAt = 0;

  // ---- Channels ----

  function igateCard(ig) {
    const [state, text] = ig.loggedIn ? ["up", "Logged in"]
      : ig.connected ? ["connecting", "Logging in"] : ["down", "Not connected"];
    const card = el("div", { class: "channel igate" },
      el("div", { class: "label", text: `IGate · ${ig.server}` }),
      el("div", { class: "figures" },
        el("span", { class: "state", "data-state": state, text }),
        el("span", {}, el("b", { text: String(ig.uplinkPackets) }), " up"),
        el("span", {}, el("b", { text: String(ig.downlinkPackets) }), " down"),
        el("span", {}, el("b", { text: String(ig.rfPackets) }), " to RF")));
    if (ig.connectedAt) card.title = "Connected since " + new Date(ig.connectedAt).toLocaleString();
    return card;
  }

  function netromCard(nr) {
    const up = nr.neighbours.filter((n) => n.link === "up").length;
    return el("div", { class: "channel netrom" },
      el("div", { class: "label", text: `NET/ROM · ${nr.alias}:${nr.callsign} · channel ${nr.channel}` }),
      el("div", { class: "figures" },
        el("span", {}, el("b", { text: `${up}/${nr.neighbours.length}` }), " links up"),
        el("span", {}, el("b", { text: String(nr.destinations.length) }), " nodes"),
        el("span", {}, el("b", { text: String(nr.circuits.length) }), " circuits")));
  }

  function renderChannels() {
    const box = $("channels");
    box.replaceChildren();
    if (status && status.igate) box.append(igateCard(status.igate));
    if (status && status.netrom) box.append(netromCard(status.netrom));
    for (const c of [...channels.values()].sort((a, b) => a.number - b.number)) {
      box.append(el("div", { class: "channel" },
        el("div", { class: "label", text: `Channel ${c.number} · ${c.description || "unknown"}` +
          (c.callsign ? ` · ${c.callsign}` : "") }),
        el("div", { class: "figures" },
          el("span", { class: "rx" }, el("b", { text: String(c.rx) }), " rx"),
          el("span", { class: "tx" }, el("b", { text: String(c.tx) }), " tx"))));
    }
  }

  function countPacket(p) {
    let c = channels.get(p.channel);
    if (!c) {
      c = { number: p.channel, description: p.via || "", rx: 0, tx: 0 };
      channels.set(p.channel, c);
    }
    c[p.dir === "tx" ? "tx" : "rx"]++;
  }

  // ---- NET/ROM ----

  function netromTable(heads, rows, empty) {
    if (rows.length === 0) return el("p", { class: "empty", text: empty });
    return el("table", {},
      el("thead", {}, el("tr", {}, ...heads.map(([text, cls]) => el("th", cls ? { class: cls, text } : { text })))),
      el("tbody", {}, ...rows.map((cells) => el("tr", {}, ...cells.map(([text, cls]) =>
        el("td", cls ? { class: cls, text: String(text) } : { text: String(text) }))))));
  }

  function renderNetrom() {
    const panel = $("netrom");
    const nr = status && status.netrom;
    panel.hidden = !nr;
    if (!nr) return;

    const neighbours = nr.neighbours.map((n) => [
      [n.callsign, "name"],
      [n.failing ? `${n.link}, failing` : n.link, "state"],
      [n.failures, "num"],
    ]);
    const routes = nr.destinations.flatMap((d) => d.routes.map((r, i) => [
      [i === 0 ? d.callsign : "", "name"],
      [i === 0 ? d.alias : "", "name"],
      [r.neighbour, "name"],
      [r.quality, "num"],
      [r.obsolescence, "num"],
    ]));
    const circuits = nr.circuits.map((c) => [
      [c.ownCall, "name"],
      [c.incoming ? "←" : "→", ""],
      [c.remoteCall, "name"],
      [c.remoteNode, "name"],
      [c.state, "state"],
      [c.outstanding, "num"],
    ]);

    $("netrom-tables").replaceChildren(
      el("div", {}, el("h3", { text: "Neighbours" }),
        netromTable([["Callsign"], ["Link"], ["Failures", "num"]], neighbours, "No neighbours yet.")),
      el("div", {}, el("h3", { text: "Routes" }),
        netromTable([["Node"], ["Alias"], ["Via"], ["Quality", "num"], ["Obs", "num"]], routes, "No routes yet.")),
      el("div", {}, el("h3", { text: "Circuits" }),
        netromTable([["Ours"], [""], ["Theirs"], ["Node"], ["State"], ["Queued", "num"]], circuits, "No circuits open.")));
  }

  // ---- Stations ----

  let stationsDirty = false;
  let freshStation = null;

  function renderStations() {
    stationsDirty = false;
    const list = [...stations.values()].sort((a, b) => Date.parse(b.lastHeard) - Date.parse(a.lastHeard));
    const body = $("stations").tBodies[0];
    body.replaceChildren(...list.map((s) => {
      const name = el("button", { type: "button", text: s.name });
      if (s.position) {
        name.title = "Show on map";
        name.addEventListener("click", () => showOnMap(s.name));
      } else {
        name.disabled = true;
      }
      const row = el("tr", s.name === freshStation ? { class: "fresh" } : {},
        el("td", { class: "sym", text: glyph(s.symbol) }),
        el("td", { class: "name" }, name),
        el("td", { class: "num", text: String(s.channel) }),
        el("td", { text: s.via || "" }),
        el("td", { text: ago(s.lastHeard), title: new Date(s.lastHeard).toLocaleString() }),
        el("td", { class: "num", text: String(s.count) }));
      if (s.comment) row.title = s.comment;
      return row;
    }));
    freshStation = null;
    $("station-count").textContent = list.length ? `(${list.length})` : "";
    $("stations-empty").hidden = list.length > 0;
  }

  function updateStation(s) {
    stations.set(s.name, s);
    freshStation = s.name;
    stationsDirty = true;
    updateMarker(s);
  }

  // ---- Packets ----

  function packetItem(p) {
    return el("li", { "data-text": (p.monitor + " " + (p.description || "")).toLowerCase() },
      el("span", { class: "meta" },
        clock(p.time) + " ",
        el("span", { class: "dir-" + p.dir, text: p.dir === "tx" ? "TX" : "RX" }),
        ` ch${p.channel}` + (p.via && p.via !== "radio" ? ` ${p.via}` : "") +
          (p.audioLevel != null ? ` · audio ${p.audioLevel}` : "")),
      el("span", { class: "raw", text: p.monitor }),
      p.description ? el("span", { class: "desc", text: p.description }) : null);
  }

  function filterText() {
    return $("filter").value.trim().toLowerCase();
  }

  function applyFilter(li, f) {
    li.hidden = f !== "" && !li.dataset.text.includes(f);
  }

  function renderPackets() {
    const f = filterText();
    const items = packets.slice().reverse().map(packetItem);
    items.forEach((li) => applyFilter(li, f));
    $("packets").replaceChildren(...items);
    $("packets-empty").hidden = packets.length > 0;
  }

  function addPacket(p) {
    packets.push(p);
    if (packets.length > MAX_PACKETS) packets.splice(0, packets.length - MAX_PACKETS);
    countPacket(p);
    renderChannels();

    if (paused) {
      pendingWhilePaused++;
      $("pause").textContent = `Resume (${pendingWhilePaused})`;
      return;
    }
    const list = $("packets");
    const li = packetItem(p);
    applyFilter(li, filterText());
    list.prepend(li);
    while (list.children.length > MAX_PACKETS) list.lastElementChild.remove();
    $("packets-empty").hidden = true;
  }

  $("filter").addEventListener("input", () => {
    const f = filterText();
    for (const li of $("packets").children) applyFilter(li, f);
  });

  $("pause").addEventListener("click", () => {
    paused = !paused;
    $("pause").setAttribute("aria-pressed", String(paused));
    $("pause").textContent = paused ? "Resume" : "Pause";
    if (!paused) {
      pendingWhilePaused = 0;
      renderPackets();
    }
  });

  // ---- Map ----

  let map = null;
  const markers = new Map(); // name -> L.Marker
  let fitted = false;

  function stationIcon(s) {
    const root = el("div", {},
      el("span", { class: "glyph", text: glyph(s.symbol) }),
      el("span", { class: "call", text: s.name }));
    return L.divIcon({ html: root, className: "station-icon", iconSize: [24, 24], iconAnchor: [12, 12] });
  }

  function stationPopup(s) {
    return el("div", { class: "popup" },
      el("h3", { text: s.name }),
      s.comment ? el("p", { text: s.comment }) : null,
      el("p", { class: "muted", text: `Channel ${s.channel}${s.via ? " via " + s.via : ""} · heard ${s.count}× · last ${ago(s.lastHeard)}` }),
      el("p", { class: "muted", text: `${s.position.lat.toFixed(4)}, ${s.position.lon.toFixed(4)}` }));
  }

  function updateMarker(s) {
    if (!map || !s.position) return;
    const ll = [s.position.lat, s.position.lon];
    let m = markers.get(s.name);
    if (m) {
      m.setLatLng(ll).setIcon(stationIcon(s));
    } else {
      m = L.marker(ll, { icon: stationIcon(s), title: s.name, keyboard: true }).addTo(map);
      markers.set(s.name, m);
    }
    // Built when opened, so "last heard" is current.
    m.bindPopup(() => stationPopup(stations.get(s.name) || s));
  }

  function pruneMarkers() {
    for (const [name, m] of markers) {
      if (!stations.has(name)) {
        m.remove();
        markers.delete(name);
      }
    }
  }

  function ensureMap() {
    if (map) {
      map.invalidateSize();
      return;
    }
    map = L.map("map", { worldCopyJump: true }).setView([30, 0], 2);
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    }).addTo(map);
    for (const s of stations.values()) updateMarker(s);
  }

  function fitToStations() {
    if (fitted || markers.size === 0) return;
    fitted = true;
    const b = L.latLngBounds([...markers.values()].map((m) => m.getLatLng()));
    map.fitBounds(b.pad(0.2), { maxZoom: 12 });
  }

  function showOnMap(name) {
    selectTab("map");
    const m = markers.get(name);
    if (m) {
      fitted = true;
      map.setView(m.getLatLng(), Math.max(map.getZoom(), 12));
      m.openPopup();
    }
  }

  // ---- Tabs ----

  function selectTab(which) {
    for (const t of ["dashboard", "map"]) {
      $("tab-" + t).setAttribute("aria-selected", String(t === which));
      $("view-" + t).hidden = t !== which;
    }
    if (which === "map") {
      ensureMap();
      fitToStations();
    }
    try { localStorage.setItem("samoyed.tab", which); } catch { /* fine */ }
  }

  $("tab-dashboard").addEventListener("click", () => selectTab("dashboard"));
  $("tab-map").addEventListener("click", () => selectTab("map"));

  // ---- Station ----

  function duration(s) {
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  function renderStatus() {
    if (!status) return;
    const up = status.uptimeSeconds + Math.floor((performance.now() - statusAt) / 1000);
    const info = $("station-info");
    info.textContent = (status.version ? `${status.version} · ` : "") + `up ${duration(up)}`;
    info.title = "Started " + new Date(status.started).toLocaleString();
  }

  function setStationStatus(s) {
    status = s;
    statusAt = performance.now();
    renderStatus();
    renderChannels();
    renderNetrom();
  }

  // The IGate's counts and the NET/ROM node's tables change without any
  // event to say so.
  async function refreshStatus() {
    try {
      setStationStatus(await getJSON("api/status"));
    } catch (e) {
      console.error(e);
    }
  }

  // ---- Data ----

  function setStatus(state, text) {
    $("status").dataset.state = state;
    $("status").textContent = text;
  }

  async function getJSON(path) {
    const r = await fetch(path, { cache: "no-store" });
    if (!r.ok) throw new Error(`${path}: ${r.status}`);
    return r.json();
  }

  // Events that arrive while a snapshot is loading wait here, so nothing is
  // lost between the two and nothing is counted twice.
  let queued = null;

  async function loadSnapshot() {
    queued = [];
    try {
      const [ps, ss, cs, st] = await Promise.all([
        getJSON("api/packets"), getJSON("api/stations"), getJSON("api/channels"), getJSON("api/status")]);
      setStationStatus(st);
      packets = ps.slice(-MAX_PACKETS);
      stations.clear();
      for (const s of ss) stations.set(s.name, s);
      channels = new Map(cs.map((c) => [c.number, c]));

      const pending = queued;
      queued = null;
      const seen = new Set(ps.map((p) => p.time + p.monitor));
      renderPackets();
      renderChannels();
      pruneMarkers();
      for (const s of stations.values()) updateMarker(s);
      for (const [kind, data] of pending) {
        if (kind === "packet" && !seen.has(data.time + data.monitor)) addPacket(data);
        if (kind === "station") updateStation(data);
      }
      renderStations();
      if (map && !$("view-map").hidden) fitToStations();
    } catch (e) {
      queued = null;
      setStatus("down", "Couldn't load");
      console.error(e);
    }
  }

  function connect() {
    const es = new EventSource("events");
    es.addEventListener("open", () => {
      setStatus("live", "Live");
      loadSnapshot();
    });
    es.addEventListener("error", () => {
      // EventSource retries by itself; a reconnect re-runs the snapshot.
      setStatus(es.readyState === EventSource.CLOSED ? "down" : "connecting",
        es.readyState === EventSource.CLOSED ? "Disconnected" : "Reconnecting…");
    });
    es.addEventListener("packet", (e) => {
      const p = JSON.parse(e.data);
      if (queued) queued.push(["packet", p]);
      else addPacket(p);
    });
    es.addEventListener("station", (e) => {
      const s = JSON.parse(e.data);
      if (queued) queued.push(["station", s]);
      else updateStation(s);
    });
  }

  // Station rows show relative times; batch their redraws too, as a busy
  // APRS-IS feed can update several stations a second.
  setInterval(() => {
    const cutoff = Date.now() - STATION_MAX_AGE_MS;
    for (const [name, s] of stations) {
      if (Date.parse(s.lastHeard) < cutoff) stations.delete(name);
    }
    pruneMarkers();
    renderStations();
    refreshStatus();
  }, 10000);
  setInterval(() => { if (stationsDirty) renderStations(); }, 500);

  let initial = "dashboard";
  try { initial = localStorage.getItem("samoyed.tab") || initial; } catch { /* fine */ }
  selectTab(initial === "map" ? "map" : "dashboard");
  connect();
})();
