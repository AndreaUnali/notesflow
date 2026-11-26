"use strict";

const $ = (id) => document.getElementById(id);
const canvas = $("canvas");
const context = canvas.getContext("2d");
const styles = getComputedStyle(document.documentElement);
const themeColor = (name) => styles.getPropertyValue("--" + name).trim();
const palette = ["cyan", "magenta", "violet", "yellow"].map(themeColor);
const graphColors = {explicit: palette[0], semantic: palette[1], text: themeColor("text"), muted: themeColor("muted")};
const kinds = {hierarchy: "Gerarchia", semantic: "Semantico", ref: "LaTeX · ref",
  eqref: "LaTeX · eqref", input: "LaTeX · input", include: "LaTeX · include"};
const filters = new Map([...document.querySelectorAll("[data-kind]")].map(input => [input.dataset.kind, input]));
let nodes = new Map();
let edges = [];
let adjacency = new Map();
let selected = null;
let hovered = null;
let drag = null;
let view = {x: 0, y: 0, scale: 1};
let width = 0;
let height = 0;
let density = 1;
let heat = 1;
let firstSnapshot = true;
let revision = 0;
const graphPageSize = 14;
const mapChaptersPerPage = 3;
let catalog = null;
let chapterId = null;
let graphPage = 0;
let searchNode = null;
const courseColors = new Map();
const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

function element(tag, text, className) {
  const el = document.createElement(tag);
  if (text) el.textContent = text;
  if (className) el.className = className;
  return el;
}

function color(course) {
  if (!courseColors.has(course)) courseColors.set(course, palette[courseColors.size % palette.length]);
  return courseColors.get(course);
}

function activeEdges() {
  return edges.filter(edge => (filters.get(edge.kind) || filters.get("explicit")).checked);
}

function refreshFilters() {
  adjacency = new Map([...nodes.keys()].map(id => [id, new Map()]));
  for (const edge of activeEdges()) {
    adjacency.get(edge.source).set(edge.target, [...(adjacency.get(edge.source).get(edge.target) || []), edge]);
    adjacency.get(edge.target).set(edge.source, [...(adjacency.get(edge.target).get(edge.source) || []), edge]);
  }
  renderDetails();
}

function ingest(snapshot) {
  if (snapshot.revision === revision) return;
  revision = snapshot.revision;
  catalog = snapshot;
  if (chapterId && !catalog.groups.some(group => group.id === chapterId)) {
    chapterId = null;
    graphPage = 0;
  }
  if (searchNode) searchNode = catalog.headings.find(node => node.id === searchNode.id) || null;
  showScope();
}

function showScope(reset = false) {
  if (!catalog) return;
  const chapter = catalog.groups.find(group => group.id === chapterId);
  const members = new Set(chapter?.members || []);
  const overview = !chapter && !searchNode;
  let all = overview ? catalog.groups : catalog.nodes.filter(node => members.has(node.id));
  if (searchNode && !members.has(searchNode.id)) all = [searchNode, ...all];
  const limit = overview ? mapChaptersPerPage : graphPageSize;
  const pages = Math.max(1, Math.ceil(all.length / limit));
  graphPage = Math.max(0, Math.min(graphPage, pages - 1));
  const page = all.slice(graphPage * limit, (graphPage + 1) * limit);
  const index = new Map(catalog.nodes.map(node => [node.id, node]));
  const family = new Map();
  const visible = overview ? page.flatMap(group => {
    family.set(group.id, group.id);
    const topics = group.topics.map(id => index.get(id)).filter(Boolean);
    for (const topic of topics) family.set(topic.id, group.id);
    return [group, ...topics];
  }) : page;
  const connections = overview ? catalog.overview_edges : catalog.edges;
  $("overview").disabled = overview && graphPage === 0;
  $("graph-prev").disabled = graphPage === 0;
  $("graph-next").disabled = graphPage === pages - 1;
  const previous = nodes;
  const keyFor = node => family.get(node.id) || node.course;
  const courses = [...new Set(visible.map(keyFor))].sort();
  for (const course of courses) color(course);
  nodes = new Map(visible.map((data, index) => {
    const known = previous.get(data.id);
    const course = courses.indexOf(keyFor(data));
    const angle = index * 2.39996;
    const spread = 29 * Math.sqrt(index + 1);
    const center = 100 * (course - (courses.length - 1) / 2);
    const motion = known || {
      x: center + Math.cos(angle) * spread, y: Math.sin(angle) * spread, vx: 0, vy: 0,
    };
    return [data.id, {...data, x: motion.x, y: motion.y, vx: motion.vx, vy: motion.vy,
      center, color: color(keyFor(data)), radius: data.kind === "section" ? 5 : 8}];
  }));
  edges = connections.filter(edge => nodes.has(edge.source) && nodes.has(edge.target));
  if (!nodes.has(selected)) selected = null;
  if (!nodes.has(hovered)) hovered = null;
  if (drag?.node && !nodes.has(drag.node)) drag = null;
  heat = Math.max(heat, previous.size === 0 ? 1 : .35);
  $("metrics").textContent = [
    overview ? "Panoramica" : chapter?.title || "Risultato",
    overview ? `${page.length} capitoli · ${visible.length - page.length} concetti` : `${visible.length} di ${all.length} nodi`,
    ...(pages > 1 ? [`Pagina ${graphPage + 1}/${pages}`] : []),
  ].join("  ·  ");
  $("courses").replaceChildren(...courses.map(course => {
    const label = element("span", nodes.get(course)?.title || course);
    const marker = element("i", "", "swatch");
    marker.style.setProperty("--node-color", color(course));
    label.prepend(marker);
    return label;
  }));
  $("empty").hidden = nodes.size > 0;
  $("empty").textContent = "Aggiungi un file .tex o .md al workspace per iniziare.";
  refreshFilters();
  updateSearch();
  if ((firstSnapshot || reset) && nodes.size) {
    firstSnapshot = false;
    fit();
  }
}

function resize() {
  const rect = canvas.getBoundingClientRect();
  width = rect.width;
  height = rect.height;
  density = Math.min(devicePixelRatio || 1, 2);
  canvas.width = Math.round(width * density);
  canvas.height = Math.round(height * density);
}

function pixelToWorld(x, y) {
  return {x: (x - width / 2 - view.x) / view.scale, y: (y - height / 2 - view.y) / view.scale};
}

function zoom(x, y, factor) {
  const point = pixelToWorld(x, y);
  view.scale = Math.max(.06, Math.min(4, view.scale * factor));
  view.x = x - width / 2 - point.x * view.scale;
  view.y = y - height / 2 - point.y * view.scale;
  $("zoom-level").value = Math.round(view.scale * 100) + "%";
}

function fit() {
  let left = -50, right = 50, top = -50, bottom = 50;
  for (const node of nodes.values()) {
    left = Math.min(left, node.x);
    right = Math.max(right, node.x);
    top = Math.min(top, node.y);
    bottom = Math.max(bottom, node.y);
  }
  view.scale = Math.max(.06, Math.min(1.5, (width - 68) / (right - left), (height - 100) / (bottom - top)));
  view.x = -(right + left) / 2 * view.scale;
  view.y = -(bottom + top) / 2 * view.scale;
  $("zoom-level").value = Math.round(view.scale * 100) + "%";
}

function simulate() {
  if (heat < .002 || !nodes.size) return;
  const grid = new Map();
  const cell = 110;
  for (const node of nodes.values()) {
    const key = Math.floor(node.x / cell) + "," + Math.floor(node.y / cell);
    if (!grid.has(key)) grid.set(key, []);
    grid.get(key).push(node);
  }
  for (const node of nodes.values()) {
    const column = Math.floor(node.x / cell);
    const row = Math.floor(node.y / cell);
    for (let x = column - 1; x <= column + 1; x++) {
      for (let y = row - 1; y <= row + 1; y++) {
        for (const other of grid.get(x + "," + y) || []) {
          if (node.id >= other.id) continue;
          const dx = other.x - node.x || .01;
          const dy = other.y - node.y || .01;
          const distance = Math.max(2, Math.hypot(dx, dy));
          if (distance > cell) continue;
          const push = Math.min(8, 1300 / (distance * distance)) * heat;
          node.vx -= dx / distance * push;
          node.vy -= dy / distance * push;
          other.vx += dx / distance * push;
          other.vy += dy / distance * push;
        }
      }
    }
  }
  for (const edge of edges) {
    if (edge.kind === "semantic") continue;
    const a = nodes.get(edge.source), b = nodes.get(edge.target);
    const dx = b.x - a.x, dy = b.y - a.y;
    const distance = Math.max(1, Math.hypot(dx, dy));
    const semantic = edge.kind === "semantic";
    const strength = (distance - (semantic ? 133 : 80)) * heat * (semantic ? .009 * edge.weight : .024);
    a.vx += dx / distance * strength;
    a.vy += dy / distance * strength;
    b.vx -= dx / distance * strength;
    b.vy -= dy / distance * strength;
  }
  for (const node of nodes.values()) {
    if (drag?.node === node.id && drag.moved) {
      node.vx = node.vy = 0;
      continue;
    }
    node.vx = (node.vx + (node.center - node.x) * .0018 * heat) * .82;
    node.vy = (node.vy - node.y * .0024 * heat) * .82;
    node.x += Math.max(-20, Math.min(20, node.vx));
    node.y += Math.max(-20, Math.min(20, node.vy));
  }
  if (!drag?.node) heat *= .975;
}

function draw(time) {
  if (!reducedMotion.matches) simulate();
  context.setTransform(density, 0, 0, density, 0, 0);
  context.clearRect(0, 0, width, height);
  context.translate(width / 2 + view.x, height / 2 + view.y);
  context.scale(view.scale, view.scale);
  const focus = hovered || selected;
  const neighbors = adjacency.get(focus);
  const visible = (id) => !focus || id === focus || neighbors?.has(id);
  const pulse = reducedMotion.matches ? .5 : (1 + Math.sin(time / 550)) / 2;
  for (const edge of activeEdges()) {
    const a = nodes.get(edge.source), b = nodes.get(edge.target);
    if (edge.kind === "semantic" && (chapterId || searchNode) && a.id !== focus && b.id !== focus) continue;
    const related = !focus || a.id === focus || b.id === focus;
    const semantic = edge.kind === "semantic";
    const hierarchy = edge.kind === "hierarchy";
    context.globalAlpha = related ? (semantic ? .44 + pulse * .34 : hierarchy ? .42 : .75) : .08;
    context.setLineDash(semantic ? [5, 5] : []);
    context.lineDashOffset = semantic && !reducedMotion.matches ? -time / 100 : 0;
    context.strokeStyle = semantic ? graphColors.semantic : hierarchy ? graphColors.muted : graphColors.explicit;
    context.lineWidth = semantic ? .85 + edge.weight * 2.0 : hierarchy ? .85 : 1.55;
    context.beginPath();
    context.moveTo(a.x, a.y);
    context.lineTo(b.x, b.y);
    context.stroke();
  }
  const query = $("search").value.trim().toLocaleLowerCase();
  const labels = nodes.size < 150 || view.scale > 1.2;
  context.setLineDash([]);
  context.font = `${Math.min(15, 11 / Math.max(.65, view.scale))}px system-ui, sans-serif`;
  context.textBaseline = "middle";
  for (const node of nodes.values()) {
    const highlighted = node.id === focus;
    const matches = !query || searchText(node).includes(query);
    context.globalAlpha = visible(node.id) && matches ? 1 : .20;
    context.shadowBlur = highlighted ? 23 : 12;
    context.shadowColor = node.color;
    context.fillStyle = node.color;
    context.beginPath();
    context.arc(node.x, node.y, node.radius + (highlighted ? 2 : 0), 0, Math.PI * 2);
    context.fill();
    context.shadowBlur = 0;
    if (highlighted || (labels && matches && view.scale > .36 && (!focus || visible(node.id)))) {
      context.fillStyle = node.kind === "file" || highlighted ? graphColors.text : graphColors.muted;
      context.fillText(node.title.length > 34 ? node.title.slice(0, 32) + "…" : node.title,
        node.x + node.radius + 5, node.y);
    }
  }
  context.globalAlpha = 1;
  requestAnimationFrame(draw);
}

function searchText(node) {
  return (node.title + " " + node.file + " " + node.course + " " + node.summary).toLocaleLowerCase();
}

function searchRank(node, query) {
  const title = node.title.toLocaleLowerCase();
  if (title === query) return 0;
  if (title.startsWith(query)) return 1;
  if (title.includes(query)) return 2;
  if ((node.file + " " + node.course).toLocaleLowerCase().includes(query)) return 3;
  return node.summary.toLocaleLowerCase().includes(query) ? 4 : Infinity;
}

function updateSearch() {
  const query = $("search").value.trim().toLocaleLowerCase();
  const results = $("results");
  results.hidden = !query;
  results.replaceChildren();
  if (!query) return;
  const index = new Map();
  for (const node of [...(catalog?.groups || []), ...(catalog?.headings || []), ...(catalog?.nodes || [])]) {
    index.set(node.id, node);
  }
  const hits = [...index.values()].map(node => ({node, rank: searchRank(node, query)}))
    .filter(hit => Number.isFinite(hit.rank)).sort((a, b) => a.rank - b.rank).slice(0, 10);
  if (!hits.length) results.append(element("p", "Nessun nodo corrispondente."));
  for (const {node} of hits) {
    const button = element("button", node.title + " · " + node.course);
    button.type = "button";
    button.addEventListener("click", () => visit(node));
    results.append(button);
  }
}

function visit(node) {
  if (node.kind === "group") {
    chapterId = node.id;
    graphPage = 0;
    searchNode = null;
    $("search").value = "";
    showScope(true);
    select(nodes.keys().next().value);
    return;
  }
  const group = catalog.groups.find(item => item.members.includes(node.id)) ||
    catalog.groups.find(item => item.file === node.file);
  chapterId = group?.id || null;
  const member = group?.members.indexOf(node.id) ?? -1;
  searchNode = member < 0 ? node : null;
  graphPage = member < 0 ? 0 : Math.floor(member / graphPageSize);
  $("search").value = "";
  showScope(true);
  select(node.id, true);
}

function select(id, center = false) {
  selected = id;
  const node = nodes.get(id);
  if (node && center) {
    view.scale = Math.max(.9, view.scale);
    view.x = -node.x * view.scale;
    view.y = -node.y * view.scale;
    $("zoom-level").value = Math.round(view.scale * 100) + "%";
  }
  renderDetails();
}

function renderDetails() {
  const details = $("details");
  const node = nodes.get(selected);
  if (!node) {
    details.replaceChildren(element("h2", "Dettagli nodo"),
      element("p", "Seleziona un nodo per leggere l’anteprima e vedere i collegamenti."));
    return;
  }
  if (node.kind === "group") {
    const enter = element("button", "Entra nel capitolo", "action");
    enter.type = "button";
    enter.addEventListener("click", () => visit(node));
    details.replaceChildren(element("h2", node.title), element("p", node.summary), enter);
    return;
  }
  const neighbors = adjacency.get(node.id) || new Map();
  const open = element("button", "Apri in Neovim ↗", "action");
  open.type = "button";
  const message = element("div", "", "message");
  message.setAttribute("role", "status");
  open.addEventListener("click", async () => {
    open.disabled = true;
    message.textContent = "";
    try {
      const response = await fetch("/api/edit", {method: "POST",
        headers: {"Content-Type": "application/json"}, body: JSON.stringify({id: node.id})});
      message.textContent = response.ok ? "Aperto in Neovim." : (await response.text()).trim();
    } catch {
      message.textContent = "Connessione locale interrotta.";
    } finally {
      open.disabled = false;
    }
  });
  details.replaceChildren(element("h2", node.title),
    element("p", node.course + " · " + (node.kind === "file" ? "File" : "Sezione")),
    element("p", node.file + ":" + node.line, "path"),
    element("p", node.summary || "Nessun estratto per questo nodo.", "preview"),
    open, message, element("h3", `Nodi connessi · ${neighbors.size}`));
  const list = element("div", "", "neighbors");
  for (const [id, links] of neighbors) {
    const neighbor = nodes.get(id);
    const button = element("button", neighbor.title);
    button.type = "button";
    const descriptions = links.map(link => {
      const type = link.origin === "mock" ? "Affinità locale" : kinds[link.kind];
      return type + (link.reason ? " · " + link.reason : "");
    });
    button.append(element("small", descriptions.join(" / ")));
    button.addEventListener("click", () => select(id, true));
    list.append(button);
  }
  if (!neighbors.size) list.append(element("p", "Nessun collegamento con i filtri attivi."));
  details.append(list);
}

function hit(x, y) {
  const point = pixelToWorld(x, y);
  let closest = null, best = Infinity;
  for (const node of nodes.values()) {
    const distance = Math.hypot(point.x - node.x, point.y - node.y);
    if (distance < Math.max(node.radius + 3, 10 / view.scale) && distance < best) {
      closest = node.id;
      best = distance;
    }
  }
  return closest;
}

canvas.addEventListener("pointerdown", event => {
  if (drag || event.button !== 0) return;
  const {offsetX: x, offsetY: y} = event;
  const node = hit(x, y);
  const point = pixelToWorld(x, y);
  const target = nodes.get(node);
  drag = {node, x, y, viewX: view.x, viewY: view.y, pointer: event.pointerId,
    offsetX: (target?.x ?? point.x) - point.x, offsetY: (target?.y ?? point.y) - point.y, moved: false};
  canvas.setPointerCapture(event.pointerId);
});
canvas.addEventListener("pointermove", event => {
  const {offsetX: x, offsetY: y} = event;
  if (!drag) {
    hovered = hit(x, y);
    return;
  }
  if (event.pointerId !== drag.pointer) return;
  drag.moved ||= Math.hypot(x - drag.x, y - drag.y) > 4;
  if (!drag.moved) return;
  canvas.classList.add("dragging");
  const node = nodes.get(drag.node);
  if (node) {
    const point = pixelToWorld(x, y);
    node.x = point.x + drag.offsetX;
    node.y = point.y + drag.offsetY;
    heat = Math.max(heat, .32);
  } else {
    view.x = drag.viewX + x - drag.x;
    view.y = drag.viewY + y - drag.y;
  }
});
function finishDrag(event) {
  if (event.pointerId !== drag?.pointer) return;
  if (event.type === "pointerup" && !drag.moved) select(drag.node);
  drag = null;
  hovered = null;
  canvas.classList.remove("dragging");
}
canvas.addEventListener("pointerup", finishDrag);
canvas.addEventListener("pointercancel", finishDrag);
canvas.addEventListener("lostpointercapture", finishDrag);
canvas.addEventListener("pointerleave", () => { hovered = null; });
canvas.addEventListener("wheel", event => {
  event.preventDefault();
  zoom(event.offsetX, event.offsetY, event.deltaY < 0 ? 1.14 : 1 / 1.14);
}, {passive: false});
$("search").addEventListener("input", updateSearch);
$("search").addEventListener("keydown", event => {
  if (event.key === "Enter") $("results").querySelector("button")?.click();
});
for (const filter of filters.values()) filter.addEventListener("change", refreshFilters);
$("reset").addEventListener("click", fit);
$("zoom-in").addEventListener("click", () => zoom(width / 2, height / 2, 1.2));
$("zoom-out").addEventListener("click", () => zoom(width / 2, height / 2, 1 / 1.2));
new ResizeObserver(resize).observe(canvas);
resize();
requestAnimationFrame(draw);

$("overview").addEventListener("click", () => {
  chapterId = searchNode = null;
  graphPage = 0;
  showScope(true);
});
$("graph-prev").addEventListener("click", () => { graphPage--; showScope(true); });
$("graph-next").addEventListener("click", () => { graphPage++; showScope(true); });
canvas.addEventListener("dblclick", event => {
  const node = nodes.get(hit(event.offsetX, event.offsetY));
  if (node?.kind === "group") visit(node);
});

const events = new EventSource("/api/events");
events.addEventListener("graph", event => ingest(JSON.parse(event.data)));
events.onopen = () => {
  revision = 0;
  $("live").textContent = "In diretta";
  $("live-light").classList.add("online");
};
events.onerror = () => {
  $("live").textContent = "Riconnessione…";
  $("live-light").classList.remove("online");
};
