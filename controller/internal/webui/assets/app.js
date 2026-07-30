const state = {
  machines: [],
  nextCursor: null,
  selected: null,
  timer: null,
  loading: false,
};

const elements = Object.fromEntries([
  "status-dot", "connection-label", "updated-at", "search", "active-filter",
  "refresh-interval", "refresh-button", "machine-count", "machine-detail",
  "instance-count", "instance-detail", "peer-count", "peer-detail", "freshness",
  "collection-detail", "topology-canvas", "empty-state", "details-title",
  "details-content", "machine-table", "result-count", "load-more", "toast",
].map((id) => [id, document.getElementById(id)]));

function apiURL(cursor = "") {
  const params = new URLSearchParams({ limit: "200" });
  const active = elements["active-filter"].value;
  if (active) params.set("active", active);
  if (cursor) params.set("cursor", cursor);
  return `/v1/topology?${params}`;
}

async function loadTopology({ append = false, quiet = false } = {}) {
  if (state.loading) return;
  state.loading = true;
  elements["refresh-button"].disabled = true;
  if (!quiet) setConnection("loading", "Refreshing");
  try {
    const response = await fetch(apiURL(append ? state.nextCursor : ""), {
      headers: { Accept: "application/json" },
      cache: "no-store",
    });
    if (!response.ok) throw new Error(`Controller returned ${response.status}`);
    const payload = await response.json();
    state.machines = append ? state.machines.concat(payload.machines || []) : (payload.machines || []);
    state.nextCursor = payload.page?.next_cursor || null;
    state.latestCollection = payload.latest_collection || null;
    state.latestErrors = payload.latest_errors || [];
    setConnection("online", "Live");
    elements["updated-at"].textContent = `Updated ${formatDate(new Date())}`;
    render();
  } catch (error) {
    setConnection("offline", "Unavailable");
    showToast(error.message || "Unable to load topology");
  } finally {
    state.loading = false;
    elements["refresh-button"].disabled = false;
  }
}

function setConnection(mode, label) {
  elements["status-dot"].className = `status-dot ${mode}`;
  elements["connection-label"].textContent = label;
}

function filteredMachines() {
  const query = elements.search.value.trim().toLowerCase();
  if (!query) return state.machines;
  return state.machines.filter((machine) => {
    const instanceText = (machine.network_instances || []).flatMap((instance) => [
      instance.instance_id,
      instance.node?.hostname,
      instance.node?.ipv4,
      ...(instance.peers || []).flatMap((peer) => [peer.hostname, peer.ipv4, String(peer.peer_id)]),
    ]);
    return [machine.hostname, machine.machine_id, machine.remote_url, ...instanceText]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(query));
  });
}

function render() {
  const machines = filteredMachines();
  renderSummary(machines);
  renderGraph(machines);
  renderTable(machines);
  if (!state.selected) renderCollectionDetails();
  elements["load-more"].hidden = !state.nextCursor;
  elements["result-count"].textContent = `${machines.length} result${machines.length === 1 ? "" : "s"}`;
}

function renderSummary(machines) {
  const instances = machines.flatMap((machine) => machine.network_instances || []);
  const peers = instances.flatMap((instance) => instance.peers || []);
  const inactive = machines.filter((machine) => !machine.active).length;
  const nodes = instances.filter((instance) => instance.node).length;
  const direct = peers.filter((peer) => peer.direct).length;
  elements["machine-count"].textContent = machines.length;
  elements["machine-detail"].textContent = `${inactive} inactive`;
  elements["instance-count"].textContent = instances.length;
  elements["instance-detail"].textContent = `${nodes} with node data`;
  elements["peer-count"].textContent = peers.length;
  elements["peer-detail"].textContent = `${direct} direct`;
  if (state.latestCollection) {
    const completed = new Date(state.latestCollection.completed_at);
    elements.freshness.textContent = relativeTime(completed);
    elements["collection-detail"].textContent = `${state.latestCollection.status} · ${state.latestCollection.error_count} errors · ${shortID(state.latestCollection.collection_id)}`;
  } else {
    elements.freshness.textContent = "No data";
    elements["collection-detail"].textContent = "Waiting for collection";
  }
}

function renderCollectionDetails() {
  const collection = state.latestCollection;
  elements["details-title"].textContent = collection ? "Latest collection" : "Waiting for data";
  if (!collection) {
    elements["details-content"].innerHTML = "<p>The controller has not persisted a topology collection yet.</p>";
    return;
  }
  const issues = (state.latestErrors || []).slice(0, 8);
  elements["details-content"].innerHTML = `
    <dl class="detail-grid">
      <dt>Collection</dt><dd>${escapeHTML(shortID(collection.collection_id))}</dd>
      <dt>Status</dt><dd>${escapeHTML(collection.status)}</dd>
      <dt>Machines</dt><dd>${collection.machine_count}</dd>
      <dt>Errors</dt><dd>${collection.error_count}</dd>
      <dt>Completed</dt><dd>${escapeHTML(formatDate(new Date(collection.completed_at)))}</dd>
      <dt>Ingested</dt><dd>${escapeHTML(formatDate(new Date(collection.ingested_at)))}</dd>
    </dl>
    ${issues.length ? `<div class="issue-list">${issues.map((issue) => `<article><strong>${escapeHTML(issue.code)}</strong><span>${escapeHTML(issue.operation)} · ${escapeHTML(issue.message)}</span></article>`).join("")}</div>` : "<p class=\"healthy-copy\">No structured collection errors.</p>"}`;
}

function renderGraph(machines) {
  const svg = elements["topology-canvas"];
  svg.replaceChildren();
  const nodes = [];
  const edges = [];
  const peerNodeByKey = new Map();
  machines.forEach((machine) => {
    const machineKey = `machine:${machine.machine_id}`;
    nodes.push({ key: machineKey, type: "machine", label: machine.hostname || shortID(machine.machine_id), sub: shortID(machine.machine_id), data: machine, active: machine.active });
    (machine.network_instances || []).forEach((instance) => {
      (instance.peers || []).forEach((peer) => {
        const peerKey = `peer:${instance.instance_id}:${peer.peer_id}`;
        if (!peerNodeByKey.has(peerKey)) {
          peerNodeByKey.set(peerKey, true);
          nodes.push({ key: peerKey, type: "peer", label: peer.hostname || `Peer ${peer.peer_id}`, sub: peer.ipv4 || `ID ${peer.peer_id}`, data: peer, instance });
        }
        edges.push({ source: machineKey, target: peerKey, direct: peer.direct, data: peer });
      });
    });
  });
  elements["empty-state"].hidden = nodes.length > 0;
  if (!nodes.length) return;

  const width = svg.clientWidth || 900;
  const height = svg.clientHeight || 465;
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  const machinesOnly = nodes.filter((node) => node.type === "machine");
  const peersOnly = nodes.filter((node) => node.type === "peer");
  positionRing(machinesOnly, width * 0.5, height * 0.5, Math.min(width, height) * 0.18, -Math.PI / 2);
  positionRing(peersOnly, width * 0.5, height * 0.5, Math.min(width, height) * 0.4, -Math.PI / 2);
  const byKey = new Map(nodes.map((node) => [node.key, node]));

  edges.forEach((edge) => {
    const source = byKey.get(edge.source);
    const target = byKey.get(edge.target);
    const line = svgElement("line", {
      x1: source.x, y1: source.y, x2: target.x, y2: target.y,
      class: `edge ${edge.direct ? "direct" : "relayed"}`,
    });
    svg.appendChild(line);
  });
  nodes.forEach((node) => {
    const group = svgElement("g", {
      class: `graph-node ${node.type} ${node.active === false ? "inactive" : ""} ${state.selected?.key === node.key ? "selected" : ""}`,
      transform: `translate(${node.x} ${node.y})`,
      tabindex: "0",
      role: "button",
      "aria-label": node.label,
    });
    const radius = node.type === "machine" ? 13 : 9;
    group.appendChild(svgElement("rect", {
      x: -radius - 5,
      y: -radius - 5,
      width: 170,
      height: radius * 2 + 18,
      fill: "transparent",
      class: "node-hitbox",
    }));
    group.appendChild(svgElement("circle", { r: radius }));
    const label = svgElement("text", { x: radius + 7, y: -2 });
    label.textContent = truncate(node.label, 22);
    group.appendChild(label);
    const sub = svgElement("text", { x: radius + 7, y: 11, class: "sub-label" });
    sub.textContent = truncate(node.sub, 24);
    group.appendChild(sub);
    group.addEventListener("click", () => selectNode(node));
    group.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") selectNode(node);
    });
    svg.appendChild(group);
  });
}

function positionRing(nodes, centerX, centerY, radius, start) {
  nodes.forEach((node, index) => {
    const angle = nodes.length === 1 ? 0 : start + (Math.PI * 2 * index) / nodes.length;
    node.x = centerX + Math.cos(angle) * radius;
    node.y = centerY + Math.sin(angle) * radius;
  });
}

function renderTable(machines) {
  const body = elements["machine-table"];
  body.replaceChildren();
  machines.forEach((machine) => {
    const instances = machine.network_instances || [];
    const peers = instances.reduce((sum, instance) => sum + (instance.peers || []).length, 0);
    const row = document.createElement("tr");
    row.innerHTML = `
      <td><span class="machine-name"><strong>${escapeHTML(machine.hostname || "Unnamed")}</strong><small>${escapeHTML(shortID(machine.machine_id))}</small></span></td>
      <td><span class="state-badge ${machine.active ? "" : "inactive"}">${machine.active ? "Active" : "Inactive"}</span></td>
      <td>${instances.length}</td>
      <td>${peers}</td>
      <td>${escapeHTML(relativeTime(new Date(machine.last_observed_at)))}</td>
      <td>${escapeHTML(machine.easytier_version || "Unknown")}</td>`;
    row.addEventListener("click", () => selectNode({ key: `machine:${machine.machine_id}`, type: "machine", data: machine, label: machine.hostname }));
    body.appendChild(row);
  });
}

function selectNode(node) {
  state.selected = node;
  if (node.type === "machine") renderMachineDetails(node.data);
  else renderPeerDetails(node.data, node.instance);
  renderGraph(filteredMachines());
}

function renderMachineDetails(machine) {
  const instances = machine.network_instances || [];
  elements["details-title"].textContent = machine.hostname || "Unnamed machine";
  elements["details-content"].innerHTML = `
    <dl class="detail-grid">
      <dt>Machine ID</dt><dd>${escapeHTML(machine.machine_id)}</dd>
      <dt>Status</dt><dd>${machine.active ? "Active" : "Inactive"}</dd>
      <dt>Remote</dt><dd>${escapeHTML(machine.remote_url || "Unknown")}</dd>
      <dt>Instances</dt><dd>${instances.length}</dd>
      <dt>Last heartbeat</dt><dd>${escapeHTML(formatDate(new Date(machine.last_heartbeat_at)))}</dd>
      <dt>Observed</dt><dd>${escapeHTML(formatDate(new Date(machine.last_observed_at)))}</dd>
      <dt>EasyTier</dt><dd>${escapeHTML(machine.easytier_version || "Unknown")}</dd>
      <dt>Operating system</dt><dd>${escapeHTML(deviceLabel(machine.device))}</dd>
    </dl>
    <div class="tag-list">${instances.map((instance) => `<span class="tag">${escapeHTML(shortID(instance.instance_id))}</span>`).join("")}</div>`;
}

function renderPeerDetails(peer, instance) {
  elements["details-title"].textContent = peer.hostname || `Peer ${peer.peer_id}`;
  elements["details-content"].innerHTML = `
    <dl class="detail-grid">
      <dt>Peer ID</dt><dd>${peer.peer_id}</dd>
      <dt>Instance</dt><dd>${escapeHTML(shortID(instance.instance_id))}</dd>
      <dt>IPv4</dt><dd>${escapeHTML(peer.ipv4 || "Not reported")}</dd>
      <dt>Path</dt><dd>${peer.direct ? "Direct" : `Relayed via ${peer.next_hop_peer_id}`}</dd>
      <dt>Path cost</dt><dd>${peer.path_cost}</dd>
      <dt>Latency</dt><dd>${formatNumber(peer.latency_ms, " ms")}</dd>
      <dt>Loss</dt><dd>${peer.loss_rate == null ? "Not reported" : `${(peer.loss_rate * 100).toFixed(2)}%`}</dd>
      <dt>RX / TX</dt><dd>${formatBytes(peer.rx_bytes)} / ${formatBytes(peer.tx_bytes)}</dd>
      <dt>Observed</dt><dd>${escapeHTML(formatDate(new Date(peer.last_observed_at)))}</dd>
    </dl>
    <div class="tag-list">${(peer.tunnel_protocols || []).map((protocol) => `<span class="tag">${escapeHTML(protocol)}</span>`).join("")}</div>`;
}

function scheduleRefresh() {
  if (state.timer) clearInterval(state.timer);
  const interval = Number(elements["refresh-interval"].value);
  if (interval > 0) state.timer = setInterval(() => loadTopology({ quiet: true }), interval);
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.hidden = false;
  window.setTimeout(() => { elements.toast.hidden = true; }, 5000);
}

function svgElement(name, attributes) {
  const element = document.createElementNS("http://www.w3.org/2000/svg", name);
  Object.entries(attributes).forEach(([key, value]) => element.setAttribute(key, value));
  return element;
}

function shortID(value) { return value ? String(value).slice(0, 8) : "unknown"; }
function truncate(value, length) { return value.length > length ? `${value.slice(0, length - 1)}…` : value; }
function formatDate(date) { return Number.isNaN(date.getTime()) ? "Unknown" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" }).format(date); }
function relativeTime(date) {
  if (Number.isNaN(date.getTime())) return "Unknown";
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  if (Math.abs(seconds) < 60) return formatter.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(hours / 24), "day");
}
function formatNumber(value, suffix) { return value == null ? "Not reported" : `${Number(value).toFixed(2)}${suffix}`; }
function formatBytes(value) {
  if (value == null) return "Not reported";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let number = Number(value);
  let index = 0;
  while (number >= 1024 && index < units.length - 1) { number /= 1024; index++; }
  return `${number.toFixed(index ? 1 : 0)} ${units[index]}`;
}
function deviceLabel(device) { return device ? [device.distribution, device.os_version].filter(Boolean).join(" ") || device.os_type : "Not reported"; }
function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]);
}

elements["refresh-button"].addEventListener("click", () => loadTopology());
elements["load-more"].addEventListener("click", () => loadTopology({ append: true }));
elements.search.addEventListener("input", render);
elements["active-filter"].addEventListener("change", () => loadTopology());
elements["refresh-interval"].addEventListener("change", scheduleRefresh);
window.addEventListener("resize", () => renderGraph(filteredMachines()));

scheduleRefresh();
loadTopology();
