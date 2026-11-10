/*
 * TRACEY.diy presentation inspiration: layered SVG graph, timeline and privacy display.
 * Upstream: https://github.com/L0laL33tz/TRACEY.diy
 * Copyright (c) 2026 tracey.diy. MIT License.
 * This report application and its source-of-funds behavior are independent work.
 */
(function () {
  "use strict";

  const loadIssues = [];
  const byId = function (id) { return document.getElementById(id); };
  const isRecord = function (value) {
    return Boolean(value) && typeof value === "object" && !Array.isArray(value);
  };
  const asArray = function (value) { return Array.isArray(value) ? value : []; };
  const text = function (value, fallback) {
    return typeof value === "string" && value.length > 0 ? value : (fallback || "");
  };
  const make = function (tag, className, content) {
    const element = document.createElement(tag);
    if (className) {
      element.className = className;
    }
    if (content !== undefined) {
      element.textContent = content;
    }
    return element;
  };
  const append = function (parent) {
    for (let index = 1; index < arguments.length; index += 1) {
      if (arguments[index]) {
        parent.appendChild(arguments[index]);
      }
    }
    return parent;
  };
  const parseEmbedded = function (id, description) {
    const element = byId(id);
    if (!element) {
      loadIssues.push(description + " is missing; a safe empty value is being used.");
      return {};
    }
    try {
      const parsed = JSON.parse(element.textContent || "");
      if (!isRecord(parsed)) {
        loadIssues.push(description + " is not a JSON object; a safe empty value is being used.");
        return {};
      }
      return parsed;
    } catch (error) {
      loadIssues.push(description + " could not be parsed. Replace its template placeholder with valid generated JSON.");
      return {};
    }
  };

  const report = parseEmbedded("report-data", "Embedded report data");
  const config = parseEmbedded("server-config", "Server configuration");
  const api = globalThis.SOFGraph;
  if (!api) {
    const notice = make("div", "notice", "The graph module is unavailable. The report cannot be rendered safely.");
    append(byId("load-notices"), notice);
    return;
  }
  if (report.schema_version !== 1) {
    loadIssues.push("Missing or unsupported schema_version. Known values are rendered conservatively; check the report generator.");
  }

  const baseGraph = api.normalizeGraph(report.graph);
  const allUtxos = asArray(report.utxos);
  const allPurchases = asArray(report.purchases);
  const state = {
    redacted: true,
    status: "all",
    purchaseId: "all",
    zoom: 1,
    panX: 0,
    panY: 0,
    viewportWidth: 900,
    viewportHeight: 520,
    layout: null,
    selectedRoots: new Set(),
    annotations: { transaction: new Map(), address: new Map() },
    activeAnnotation: null,
    saving: false,
    lastFocus: null,
    drag: null,
    dragged: false
  };

  function secret(value, redactedText) {
    const present = text(value);
    if (!present) {
      return "—";
    }
    return state.redacted ? (redactedText || "[redacted]") : present;
  }

  function formatSats(value) {
    if (state.redacted && Number.isSafeInteger(value)) {
      return "[redacted value]";
    }
    return api.formatSats(value);
  }

  function formatCount(value) {
    return Number.isSafeInteger(value) && value >= 0 ? value.toLocaleString("en-US") : "—";
  }

  function shortIdentifier(value) {
    const string = text(value, "—");
    if (state.redacted || string === "—" || string.length <= 25) {
      return string;
    }
    return string.slice(0, 12) + "…" + string.slice(-9);
  }

  function notes(value) {
    if (typeof value === "string") {
      return value ? [value] : [];
    }
    return asArray(value).filter(function (item) {
      return typeof item === "string" && item.length > 0;
    });
  }

  function humanStatus(value, allowed) {
    return allowed.includes(value) ? value.replaceAll("_", " ") : "unavailable";
  }

  function statusPill(value, allowed) {
    const known = allowed.includes(value);
    return make("span", "status-pill " + (known ? "status-" + value : ""), humanStatus(value, allowed));
  }

  function initialAnnotation(kind, identifier) {
    let label = "";
    for (const node of baseGraph.nodes) {
      const matches = kind === "transaction"
        ? node.kind === "transaction" && (node.txid === identifier || node.id === identifier)
        : typeof node.address === "string" && node.address === identifier;
      if (matches && typeof node.label === "string") {
        label = node.label;
        break;
      }
    }
    return { label: label, note: "" };
  }

  function getAnnotation(kind, identifier) {
    const saved = state.annotations[kind].get(identifier);
    return saved || initialAnnotation(kind, identifier);
  }

  function annotationLabel(kind, identifier) {
    if (!identifier) {
      return "";
    }
    const annotation = getAnnotation(kind, identifier);
    if (state.redacted && (annotation.label || annotation.note)) {
      return "Redacted user annotation";
    }
    return annotation.label;
  }

  function annotationBlock(kind, identifier) {
    const wrapper = make("div");
    if (!identifier) {
      wrapper.appendChild(make("span", "empty-cell", "No user label"));
      return wrapper;
    }
    const saved = getAnnotation(kind, identifier);
    wrapper.appendChild(make("span", "identifier", "User label: " + (annotationLabel(kind, identifier) || "None")));
    if (saved.note) {
      wrapper.appendChild(make("span", "note-box", "User note: " + secret(saved.note, "[redacted note]")));
    }
    return wrapper;
  }

  function annotationButton(kind, identifier, compact) {
    if (!identifier) {
      return null;
    }
    const button = make("button", "annotation-button", compact
      ? "Edit label"
      : (kind === "transaction" ? "Edit transaction label" : "Edit address label"));
    button.type = "button";
    button.addEventListener("click", function () {
      openAnnotation(kind, identifier, button);
    });
    return button;
  }

  function renderNotices() {
    const container = byId("load-notices");
    container.replaceChildren();
    for (const issue of loadIssues) {
      container.appendChild(make("div", "notice", issue));
    }
  }

  function renderStorageStatus(message) {
    if (message) {
      byId("storage-status").textContent = message;
      return;
    }
    if (config.server === true) {
      byId("storage-status").textContent = config.save_url === "/api/annotations"
        ? "Server mode: labels are POSTed to the same-origin annotation endpoint only when you save."
        : "Server mode has an invalid save_url; annotation saving is blocked. Expected /api/annotations.";
    } else {
      byId("storage-status").textContent = "Offline mode: annotations change this page only and are not written back to the HTML file.";
    }
  }

  function renderMetadata() {
    const chain = isRecord(report.chain) ? report.chain : {};
    const policy = isRecord(report.trace_policy) ? report.trace_policy : {};
    const policyValue = function (value) {
      return Number.isSafeInteger(value) && value > 0 ? String(value) : "Disabled";
    };
    const entries = [
      ["Network", text(report.network, "—")],
      ["Generated at", secret(report.generated_at, "[redacted timestamp]")],
      ["Best block", secret(chain.best_block)],
      ["Best height", formatCount(chain.best_height)],
      ["Maximum hops", policyValue(policy.max_hops)],
      ["Transaction budget", policyValue(policy.max_transactions)],
      ["Stop ancestor value at", policyValue(policy.stop_if_prevout_value_sats_gte) + (policy.stop_if_prevout_value_sats_gte > 0 ? " sats" : "")],
      ["Fan-in / fan-out stop", policyValue(policy.stop_if_input_count_gte) + " / " + policyValue(policy.stop_if_output_count_gte)],
      ["Oldest traced block", policyValue(policy.stop_before_block_height)]
    ];
    const metadata = byId("report-metadata");
    metadata.replaceChildren();
    for (const entry of entries) {
      const wrapper = make("div");
      append(wrapper, make("dt", "", entry[0]), make("dd", "", entry[1]));
      metadata.appendChild(wrapper);
    }
  }

  function renderSummary() {
    const summary = isRecord(report.summary) ? report.summary : {};
    const cards = [
      ["Current UTXOs", formatCount(summary.total_utxos), formatSats(summary.total_sats)],
      ["Documented", formatCount(summary.documented_utxos), formatSats(summary.documented_sats)],
      ["Partially documented", formatCount(summary.partial_utxos), "Independent documentation status"],
      ["Undocumented", formatCount(summary.undocumented_utxos), "Not a statement of trace completeness"],
      ["Incomplete traces", formatCount(summary.incomplete_utxos), "Review warnings before conclusions"],
      ["Review required", formatCount(summary.review_utxos), "Policy or data boundaries needing review"],
      ["Visible after filters", formatCount(api.filterUtxos(allUtxos, state).length), "Current selection"]
    ];
    const container = byId("summary-cards");
    container.replaceChildren();
    for (const card of cards) {
      const wrapper = make("div", "summary-card");
      append(wrapper, make("span", "", card[0]), make("strong", "", card[1]), make("small", "", card[2]));
      container.appendChild(wrapper);
    }
  }

  function renderWarnings() {
    const warnings = asArray(report.warnings);
    const list = byId("warnings-list");
    list.replaceChildren();
    if (warnings.length === 0) {
      list.appendChild(make("li", "muted", "No generator warnings were supplied. This does not expand the stated evidence limits."));
    } else {
      for (const warning of warnings) {
        list.appendChild(make("li", "", typeof warning === "string" ? warning : "Malformed warning entry supplied by generator."));
      }
    }
  }

  function purchaseOptions() {
    const identifiers = [];
    for (const purchase of allPurchases) {
      if (isRecord(purchase)) {
        identifiers.push(purchase.id);
      }
    }
    for (const utxo of allUtxos) {
      if (isRecord(utxo)) {
        identifiers.push.apply(identifiers, asArray(utxo.purchase_ids));
      }
    }
    return api.uniqueStrings(identifiers).sort();
  }

  function renderPurchaseOptions() {
    const select = byId("purchase-filter");
    const current = state.purchaseId;
    select.replaceChildren();
    const all = make("option", "", "All purchase IDs");
    all.value = "all";
    select.appendChild(all);
    purchaseOptions().forEach(function (identifier, index) {
      const option = make("option", "", state.redacted ? "Redacted purchase " + (index + 1) : identifier);
      option.value = identifier;
      select.appendChild(option);
    });
    select.value = current;
    if (select.value !== current) {
      state.purchaseId = "all";
      select.value = "all";
    }
  }

  function renderUtxos(visible) {
    const body = byId("utxo-table-body");
    body.replaceChildren();
    byId("filter-count").textContent = formatCount(visible.length) + " of " + formatCount(allUtxos.length) + " current UTXOs selected";
    byId("utxos-caption").textContent = "Selected unspent transaction outputs; purchase and note columns are user evidence";
    if (visible.length === 0) {
      const row = make("tr");
      const cell = make("td", "empty-state", "No current UTXOs match these filters.");
      cell.colSpan = 7;
      row.appendChild(cell);
      body.appendChild(row);
      return;
    }
    for (const utxo of visible) {
      const row = make("tr");
      const txid = text(utxo.txid);
      const outpoint = text(utxo.outpoint);
      const address = text(utxo.address);
      const txCell = make("td");
      append(txCell,
        make("code", "identifier", shortIdentifier(secret(outpoint))),
        annotationBlock("transaction", txid),
        annotationButton("transaction", txid, true));
      const addressCell = make("td");
      append(addressCell,
        make("code", "identifier", shortIdentifier(secret(address))),
        annotationBlock("address", address),
        annotationButton("address", address, true));
      const evidence = api.uniqueStrings(utxo.purchase_ids);
      const evidenceCell = make("td");
      if (evidence.length === 0) {
        evidenceCell.appendChild(make("span", "empty-cell", "No linked purchase evidence"));
      } else {
        const evidenceList = make("ul", "evidence-list");
        for (const identifier of evidence) {
          evidenceList.appendChild(make("li", "", secret(identifier, "[redacted purchase]")));
        }
        evidenceCell.appendChild(evidenceList);
      }
      const noteCell = make("td");
      const noteValues = notes(utxo.notes);
      if (noteValues.length === 0) {
        noteCell.appendChild(make("span", "empty-cell", "None"));
      } else {
        const noteList = make("ul", "node-note-list");
        for (const note of noteValues) {
          noteList.appendChild(make("li", "", secret(note, "[redacted note]")));
        }
        noteCell.appendChild(noteList);
      }
      append(row,
        txCell,
        addressCell,
        make("td", "", formatSats(utxo.value_sats)),
        make("td", "", ""),
        make("td", "", ""),
        evidenceCell,
        noteCell);
      row.cells[3].appendChild(statusPill(utxo.documentation_status,
        ["documented", "partially_documented", "undocumented"]));
      row.cells[4].appendChild(statusPill(utxo.trace_status, ["complete", "incomplete"]));
      if (utxo.review_required === true) {
        row.cells[4].appendChild(make("span", "review-flag", " · review required"));
        row.cells[4].title = asArray(utxo.review_reasons).join(", ");
      }
      body.appendChild(row);
    }
  }

  function svgElement(tag, attributes, content) {
    const element = document.createElementNS("http://www.w3.org/2000/svg", tag);
    if (attributes) {
      for (const name of Object.keys(attributes)) {
        element.setAttribute(name, attributes[name]);
      }
    }
    if (content !== undefined) {
      element.textContent = content;
    }
    return element;
  }

  function svgText(group, className, x, y, content) {
    const value = text(content, "—");
    const clipped = value.length > 38 ? value.slice(0, 36) + "…" : value;
    group.appendChild(svgElement("text", { "class": className, x: x, y: y }, clipped));
  }

  function graphKind(node) {
    return ["transaction", "output", "terminal"].includes(node.kind) ? node.kind : "terminal";
  }

  function graphTarget(node) {
    if (node.kind === "transaction") {
      return { kind: "transaction", identifier: text(node.txid) || text(node.id) };
    }
    if (typeof node.address === "string" && node.address) {
      return { kind: "address", identifier: node.address };
    }
    return { kind: "", identifier: "" };
  }

  function graphNode(node, position) {
    const kind = graphKind(node);
    const target = graphTarget(node);
    const selected = state.selectedRoots.has(node.id);
    const annotation = annotationLabel(target.kind, target.identifier);
    let heading = "Ancestry endpoint";
    let kicker = "TRACE BOUNDARY · NOT AN OWNERSHIP CLAIM";
    if (kind === "transaction") {
      heading = annotation || "Transaction";
      kicker = "BLOCKCHAIN FACT · TRANSACTION";
    } else if (kind === "output") {
      heading = annotation || (selected ? "Current selected UTXO" : "Previous output");
      kicker = selected ? "BLOCKCHAIN FACT · CURRENT UTXO" : "BLOCKCHAIN FACT · OUTPUT";
    } else {
      heading = annotation || text(node.terminal_reason, "Ancestry endpoint");
      kicker = "TRACE BOUNDARY";
    }
    const group = svgElement("g", {
      "class": "sof-node node-" + kind + (selected ? " selected-root" : ""),
      transform: "translate(" + position.x + " " + position.y + ")",
      role: target.identifier ? "button" : "img",
      "aria-label": heading + (target.identifier ? ". Activate to edit user annotation." : ". Trace boundary.")
    });
    if (target.identifier) {
      group.setAttribute("tabindex", "0");
      group.addEventListener("click", function () {
        if (!state.dragged) {
          openAnnotation(target.kind, target.identifier, group);
        }
      });
      group.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          openAnnotation(target.kind, target.identifier, group);
        }
      });
    }
    group.appendChild(svgElement("rect", {
      "class": "node-outline", width: position.width, height: position.height, rx: 10
    }));
    svgText(group, "node-kicker", 12, 18, kicker);
    svgText(group, "node-heading", 12, 40, heading);
    let identifier = target.identifier;
    if (!identifier) {
      identifier = text(node.outpoint) || text(node.txid) || text(node.id);
    }
    svgText(group, "node-text", 12, 61, shortIdentifier(secret(identifier)));
    let factLine = kind === "terminal" ? text(node.terminal_detail, "Traversal stopped here") : text(node.script_type);
    if (Number.isSafeInteger(node.value_sats)) {
      factLine = formatSats(node.value_sats) + (factLine ? " · " + factLine : "");
    }
    svgText(group, factLine.startsWith("[redacted") ? "node-text" : "node-text", 12, 79, factLine);
    const purchases = api.uniqueStrings(node.purchase_ids);
    let evidenceLine = "No user annotation · no purchase evidence";
    const saved = target.identifier ? getAnnotation(target.kind, target.identifier) : { label: "", note: "" };
    if (state.redacted && (saved.label || saved.note)) {
      evidenceLine = "Redacted user annotation";
    } else if (saved.label || saved.note) {
      evidenceLine = "User label/note";
    } else if (purchases.length > 0) {
      evidenceLine = purchases.length + " linked purchase record(s)";
    } else if (target.identifier) {
      evidenceLine = "Activate to add user label/note";
    }
    svgText(group, "node-evidence", 12, 98, evidenceLine);
    return group;
  }

  function renderGraphTable(layout) {
    const body = byId("graph-table-body");
    body.replaceChildren();
    if (layout.nodes.length === 0) {
      const row = make("tr");
      const cell = make("td", "empty-state", "No graph nodes are selected.");
      cell.colSpan = 5;
      row.appendChild(cell);
      body.appendChild(row);
      return;
    }
    for (const node of layout.nodes) {
      const target = graphTarget(node);
      const identifier = target.identifier || text(node.outpoint) || text(node.txid) || text(node.id);
      const purchases = api.uniqueStrings(node.purchase_ids);
      const row = make("tr");
      append(row,
        make("td", "", graphKind(node).replaceAll("_", " ")),
        make("td", "", shortIdentifier(secret(identifier))),
        make("td", "", Number.isSafeInteger(node.value_sats) ? formatSats(node.value_sats) : "—"),
        make("td", "", annotationLabel(target.kind, target.identifier) || "None"),
        make("td", "", purchases.length
          ? purchases.map(function (id) { return secret(id, "[redacted purchase]"); }).join(", ")
          : "None"));
      body.appendChild(row);
    }
  }

  function updateGraphTransform() {
    const viewport = byId("sof-svg").querySelector(".graph-viewport");
    if (viewport) {
      viewport.setAttribute("transform", "translate(" + state.panX + " " + state.panY + ") scale(" + state.zoom + ")");
    }
  }

  function renderGraph(layout) {
    const svg = byId("sof-svg");
    const empty = layout.nodes.length === 0;
    state.viewportWidth = empty ? 900 : Math.max(900, layout.width);
    state.viewportHeight = empty ? 520 : Math.max(520, layout.height);
    svg.setAttribute("viewBox", "0 0 " + state.viewportWidth + " " + state.viewportHeight);
    svg.replaceChildren(
      svgElement("title", { id: "svg-title" }, "Bitcoin UTXO source-of-funds graph"),
      svgElement("desc", { id: "svg-description" },
        "Selected current outputs are right; exact funding transactions and previous outputs extend left. Blockchain facts and user annotations are identified separately.")
    );
    const defs = svgElement("defs");
    const pattern = svgElement("pattern", { id: "graph-grid-pattern", width: 20, height: 20, patternUnits: "userSpaceOnUse" });
    pattern.appendChild(svgElement("circle", { "class": "graph-grid", cx: 1, cy: 1, r: 1 }));
    defs.appendChild(pattern);
    for (const item of [["funded_by", "#7550a7"], ["input", "#168a9c"]]) {
      const marker = svgElement("marker", {
        id: "arrow-" + item[0], viewBox: "0 0 10 10", refX: 9, refY: 5,
        markerWidth: 6, markerHeight: 6, orient: "auto"
      });
      marker.appendChild(svgElement("path", { d: "M 0 0 L 10 5 L 0 10 z", fill: item[1] }));
      defs.appendChild(marker);
    }
    svg.appendChild(defs);
    svg.appendChild(svgElement("rect", {
      "class": "graph-background", width: state.viewportWidth, height: state.viewportHeight
    }));
    svg.appendChild(svgElement("rect", {
      width: state.viewportWidth, height: state.viewportHeight, fill: "url(#graph-grid-pattern)", opacity: 0.55
    }));
    if (empty) {
      svg.appendChild(svgElement("text", { x: 450, y: 250, "text-anchor": "middle", "class": "node-heading" },
        "No current UTXOs match the filters."));
      svg.appendChild(svgElement("text", { x: 450, y: 278, "text-anchor": "middle", "class": "node-text" },
        "Clear the filters to restore graph ancestry."));
      renderGraphTable(layout);
      return;
    }
    const viewport = svgElement("g", { "class": "graph-viewport" });
    const positions = new Map(layout.positions.map(function (position) { return [position.id, position]; }));
    for (const edge of layout.edges) {
      const from = positions.get(edge.from);
      const to = positions.get(edge.to);
      const kind = edge.kind === "funded_by" ? "funded_by" : "input";
      const startX = from.x;
      const startY = from.y + from.height / 2;
      const endX = to.x + to.width;
      const endY = to.y + to.height / 2;
      const distance = Math.max(50, Math.abs(startX - endX) / 2);
      const path = "M " + startX + " " + startY + " C " + (startX - distance) + " " + startY + ", " +
        (endX + distance) + " " + endY + ", " + endX + " " + endY;
      viewport.appendChild(svgElement("path", {
        "class": "edge edge-" + kind, d: path, "marker-end": "url(#arrow-" + kind + ")",
        "aria-label": kind === "funded_by" ? "Output created by funding transaction" : "Transaction spends exact previous output"
      }));
      const label = kind === "funded_by" ? "created output" : "input / prevout";
      viewport.appendChild(svgElement("text", {
        "class": "edge-label", x: (startX + endX) / 2, y: (startY + endY) / 2 - 6
      }, label));
    }
    layout.nodes.forEach(function (node, index) {
      viewport.appendChild(graphNode(node, layout.positions[index]));
    });
    svg.appendChild(viewport);
    updateGraphTransform();
    renderGraphTable(layout);
  }

  function evidenceForTransaction(txid) {
    if (!txid) {
      return [];
    }
    return allPurchases.filter(function (purchase) {
      return isRecord(purchase) && asArray(purchase.txids).includes(txid);
    });
  }

  function renderTimeline(layout) {
    const container = byId("transaction-timeline");
    container.replaceChildren();
    const positions = new Map(layout.positions.map(function (position) { return [position.id, position]; }));
    const transactions = layout.nodes.map(function (node, index) {
      return { node: node, index: index };
    }).filter(function (entry) {
      return entry.node.kind === "transaction";
    }).sort(function (left, right) {
      const depthDifference = positions.get(right.node.id).depth - positions.get(left.node.id).depth;
      return depthDifference || left.index - right.index;
    });
    if (transactions.length === 0) {
      const empty = make("li", "empty-state", "No transactions are visible for the current selection.");
      container.appendChild(empty);
      return;
    }
    for (const entry of transactions) {
      const txid = text(entry.node.txid) || text(entry.node.id);
      const annotation = getAnnotation("transaction", txid);
      const item = make("li", "timeline-item");
      const card = make("div", "timeline-card");
      const heading = state.redacted && (annotation.label || annotation.note)
        ? "Redacted transaction annotation"
        : (annotation.label || "Transaction");
      append(card,
        make("h3", "", heading),
        make("code", "identifier", shortIdentifier(secret(txid))),
        make("p", "muted", "Blockchain transaction · topological position " + (positions.get(entry.node.id).depth + 1)));
      const evidence = evidenceForTransaction(txid);
      if (evidence.length === 0) {
        card.appendChild(make("p", "muted", "No purchase-evidence date linked by exact txid."));
      } else {
        for (const purchase of evidence) {
          card.appendChild(make("p", "timeline-date", "User evidence: " + secret(purchase.date, "[redacted date]") +
            " · " + secret(purchase.id, "[redacted purchase]")));
        }
      }
      card.appendChild(annotationButton("transaction", txid, true));
      item.appendChild(card);
      container.appendChild(item);
    }
  }

  function evidenceValues(title, values, redactedLabel) {
    const wrapper = make("div");
    wrapper.appendChild(make("h4", "", title));
    const list = make("ul", "evidence-values");
    const strings = api.uniqueStrings(values);
    if (strings.length === 0) {
      list.appendChild(make("li", "empty-cell", "None"));
    } else {
      for (const value of strings) {
        list.appendChild(make("li", "", shortIdentifier(secret(value, redactedLabel || "[redacted]"))));
      }
    }
    wrapper.appendChild(list);
    return wrapper;
  }

  function renderPurchases() {
    const container = byId("purchase-list");
    container.replaceChildren();
    const purchases = allPurchases.filter(function (purchase) {
      return isRecord(purchase) && (state.purchaseId === "all" || purchase.id === state.purchaseId);
    });
    if (purchases.length === 0) {
      container.appendChild(make("p", "empty-state", state.purchaseId === "all"
        ? "No user purchase evidence was supplied."
        : "No purchase evidence matches the selected purchase ID."));
      return;
    }
    for (const purchase of purchases) {
      const card = make("article", "purchase-card");
      card.appendChild(make("h3", "", secret(purchase.id, "[redacted purchase]")));
      const meta = make("div", "purchase-meta");
      append(meta,
        make("span", "", "Date: " + secret(purchase.date, "[redacted date]")),
        make("span", "", "Provider: " + secret(purchase.provider, "[redacted provider]")),
        make("span", "", "Reported amount: " + formatSats(purchase.amount_sats)));
      card.appendChild(meta);
      const columns = make("div", "evidence-columns");
      columns.appendChild(evidenceValues("Exact transaction IDs", purchase.txids, "[redacted txid]"));
      columns.appendChild(evidenceValues("Addresses", purchase.addresses, "[redacted address]"));
      columns.appendChild(evidenceValues("Outpoints", purchase.outpoints, "[redacted outpoint]"));
      card.appendChild(columns);
      const purchaseNotes = notes(purchase.notes);
      if (purchaseNotes.length > 0) {
        card.appendChild(make("h4", "", "User notes"));
        for (const note of purchaseNotes) {
          card.appendChild(make("div", "note-box", secret(note, "[redacted note]")));
        }
      }
      const artifacts = asArray(purchase.artifacts);
      card.appendChild(make("h4", "", "Artifacts (local paths; no links are fetched)"));
      if (artifacts.length === 0) {
        card.appendChild(make("p", "empty-cell", "No artifacts listed."));
      } else {
        const list = make("ul", "artifact-list");
        for (const artifact of artifacts) {
          const item = make("li");
          if (!isRecord(artifact)) {
            item.textContent = "Malformed artifact entry.";
          } else {
            append(item,
              make("code", "identifier", secret(artifact.path, "[redacted path]")),
              make("span", "identifier", "SHA-256: " + secret(artifact.sha256, "[redacted hash]")),
              make("span", "", secret(artifact.description, "[redacted description]")));
          }
          list.appendChild(item);
        }
        card.appendChild(list);
      }
      container.appendChild(card);
    }
  }

  function renderEvidenceDiagnostics() {
    const container = byId("purchase-list");
    const diagnostics = make("section", "evidence-diagnostics");
    diagnostics.appendChild(make("h3", "", "Purchase-evidence match diagnostics"));
    const evidence = asArray(report.evidence);
    if (evidence.length === 0) {
      diagnostics.appendChild(make("p", "empty-state", "No purchase evidence identifiers were supplied."));
      container.appendChild(diagnostics);
      return;
    }
    const unmatched = evidence.filter(function (item) { return isRecord(item) && item.matched !== true; }).length;
    diagnostics.appendChild(make("p", "muted", unmatched + " of " + evidence.length + " supplied evidence identifiers were not observed in the traced graph."));
    const table = make("table", "evidence-diagnostics-table");
    const head = make("thead");
    const headings = make("tr");
    ["Type", "Value", "Purchase ID", "Observed", "Boundary"].forEach(function (label) { headings.appendChild(make("th", "", label)); });
    head.appendChild(headings);
    const body = make("tbody");
    for (const item of evidence) {
      if (!isRecord(item)) {
        continue;
      }
      const row = make("tr", item.matched === true ? "evidence-matched" : "evidence-unmatched");
      append(row,
        make("td", "", text(item.kind, "—")),
        make("td", "", shortIdentifier(secret(item.value, "[redacted evidence]"))),
        make("td", "", asArray(item.purchase_ids).map(function (id) { return secret(id, "[redacted purchase]"); }).join(", ") || "—"),
        make("td", "", item.matched === true ? "Matched" : "Not observed"),
        make("td", "", text(item.matched_node_id, "—")));
      body.appendChild(row);
    }
    table.appendChild(head);
    table.appendChild(body);
    const wrapper = make("div", "table-wrap");
    wrapper.appendChild(table);
    diagnostics.appendChild(wrapper);
    container.appendChild(diagnostics);
  }

  function renderResults() {
    const visible = api.filterUtxos(allUtxos, state);
    const roots = api.uniqueStrings(visible.map(function (utxo) { return "out:" + utxo.outpoint; }));
    state.selectedRoots = new Set(roots);
    const selectedGraph = api.selectGraph(baseGraph, roots);
    state.layout = api.layoutGraph(selectedGraph);
    renderSummary();
    renderUtxos(visible);
    renderGraph(state.layout);
    renderTimeline(state.layout);
    renderPurchases();
    renderEvidenceDiagnostics();
  }

  function renderPrivacy() {
    const button = byId("privacy-toggle");
    button.setAttribute("aria-pressed", String(state.redacted));
    button.textContent = state.redacted ? "Redaction: on" : "Redaction: off";
    renderPurchaseOptions();
    renderMetadata();
    renderResults();
  }

  function openAnnotation(kind, identifier, returnFocus) {
    if (!state.annotations[kind] || !identifier || state.saving) {
      return;
    }
    const dialog = byId("annotation-dialog");
    if (dialog.open) {
      return;
    }
    state.activeAnnotation = { kind: kind, identifier: identifier };
    state.lastFocus = returnFocus || document.activeElement;
    const annotation = getAnnotation(kind, identifier);
    byId("annotation-kind").value = kind;
    byId("annotation-target").value = identifier;
    byId("annotation-label").value = annotation.label;
    byId("annotation-note").value = annotation.note;
    byId("annotation-heading").textContent = kind === "transaction" ? "Edit transaction label and note" : "Edit address label and note";
    renderAnnotationStatus(config.server === true
      ? "This is user evidence, not a blockchain fact. Saving requires a successful same-origin server response."
      : "Offline mode: saving updates only this open report page.");
    if (typeof dialog.showModal === "function") {
      dialog.showModal();
    } else {
      dialog.setAttribute("open", "");
    }
    byId("annotation-label").focus();
  }

  function closeAnnotation() {
    if (state.saving) {
      return;
    }
    const dialog = byId("annotation-dialog");
    if (typeof dialog.close === "function") {
      dialog.close();
    } else {
      dialog.removeAttribute("open");
    }
    state.activeAnnotation = null;
    if (state.lastFocus && typeof state.lastFocus.focus === "function" && state.lastFocus.isConnected) {
      state.lastFocus.focus();
    }
  }

  function renderAnnotationStatus(message, isError) {
    const status = byId("annotation-status");
    status.textContent = message;
    status.classList.toggle("error", Boolean(isError));
  }

  async function saveAnnotation(event) {
    event.preventDefault();
    if (state.saving || !state.activeAnnotation) {
      return;
    }
    const active = state.activeAnnotation;
    const payload = { label: byId("annotation-label").value, note: byId("annotation-note").value };
    if (active.kind === "transaction") {
      payload.transaction = active.identifier;
    } else {
      payload.address = active.identifier;
    }
    if (config.server === true && config.save_url !== "/api/annotations") {
      renderAnnotationStatus("Saving blocked: server save_url must be exactly /api/annotations.", true);
      return;
    }
    state.saving = true;
    byId("annotation-save").disabled = true;
    renderAnnotationStatus(config.server === true ? "Saving to the local annotation server…" : "Applying annotation to this page…");
    try {
      if (config.server === true) {
        const response = await fetch(config.save_url, {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", "Accept": "application/json" },
          body: JSON.stringify(payload)
        });
        if (!response.ok) {
          throw new Error("The annotation server returned HTTP " + response.status + ". Nothing was changed locally.");
        }
      }
      state.annotations[active.kind].set(active.identifier, { label: payload.label, note: payload.note });
      state.saving = false;
      byId("annotation-save").disabled = false;
      closeAnnotation();
      renderResults();
      renderStorageStatus(config.server === true
        ? "Annotation saved to the local server and applied to this report."
        : "Annotation applied to this page only. Print now or keep this page open; the embedded HTML was not modified.");
    } catch (error) {
      state.saving = false;
      byId("annotation-save").disabled = false;
      renderAnnotationStatus(error instanceof Error ? error.message : "The annotation could not be saved.", true);
    }
  }

  function changeZoom(factor) {
    state.zoom = Math.min(4, Math.max(0.25, state.zoom * factor));
    updateGraphTransform();
  }

  function resetView() {
    state.zoom = 1;
    state.panX = 0;
    state.panY = 0;
    updateGraphTransform();
  }

  function pan(x, y) {
    state.panX += x;
    state.panY += y;
    updateGraphTransform();
  }

  function graphSvgCss() {
    return ".sof-svg .graph-background{fill:#f7fafc}.sof-svg .graph-grid{fill:#c5d0db}" +
      ".sof-svg .edge{fill:none;stroke-width:2.1}.sof-svg .edge-funded_by{stroke:#7550a7}" +
      ".sof-svg .edge-input{stroke:#168a9c;stroke-dasharray:7 4}.sof-svg .edge-label{fill:#5a6474;font-size:10px;text-anchor:middle;paint-order:stroke;stroke:#f7fafc;stroke-width:3px}" +
      ".sof-svg .node-outline{stroke:#7890a6;stroke-width:1.5}.sof-svg .node-transaction .node-outline{fill:#f2eef9;stroke:#7550a7}" +
      ".sof-svg .node-output .node-outline{fill:#edf7fb;stroke:#168a9c}.sof-svg .node-terminal .node-outline{fill:#f2f3f5;stroke:#6f7884;stroke-dasharray:5 3}" +
      ".sof-svg .selected-root .node-outline{fill:#fff8e9;stroke:#a86709;stroke-width:3}.sof-svg .node-kicker{fill:#5a6474;font-size:9px;font-weight:750;letter-spacing:.07em}" +
      ".sof-svg .node-heading{fill:#17243a;font-size:14px;font-weight:750}.sof-svg .node-text{fill:#334155;font-family:monospace;font-size:10px}" +
      ".sof-svg .node-evidence{fill:#8a570c;font-size:9px;font-weight:650}.sof-svg text{font-family:system-ui,sans-serif}";
  }

  function downloadSvg() {
    if (!state.layout) {
      return;
    }
    try {
      const clone = byId("sof-svg").cloneNode(true);
      const viewport = clone.querySelector(".graph-viewport");
      if (viewport) {
        viewport.removeAttribute("transform");
      }
      clone.classList.remove("is-dragging");
      clone.setAttribute("xmlns", "http://www.w3.org/2000/svg");
      clone.setAttribute("width", String(state.viewportWidth));
      clone.setAttribute("height", String(state.viewportHeight));
      clone.querySelectorAll("[tabindex]").forEach(function (element) { element.removeAttribute("tabindex"); });
      const style = document.createElementNS("http://www.w3.org/2000/svg", "style");
      style.textContent = graphSvgCss();
      clone.insertBefore(style, clone.firstChild);
      const serialized = new XMLSerializer().serializeToString(clone);
      const source = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- TRACEY.diy presentation inspiration; MIT License. -->\n" + serialized;
      const blob = new Blob([source], { type: "image/svg+xml;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = state.redacted ? "tracey-sof-graph-redacted.svg" : "tracey-sof-graph.svg";
      link.hidden = true;
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
      renderStorageStatus("SVG downloaded with the current filters, labels and redaction setting.");
    } catch (error) {
      renderStorageStatus("SVG export failed: " + (error instanceof Error ? error.message : "unknown browser error"));
    }
  }

  function attachEvents() {
    byId("privacy-toggle").addEventListener("click", function () {
      state.redacted = !state.redacted;
      renderPrivacy();
    });
    byId("status-filter").addEventListener("change", function (event) {
      state.status = event.target.value;
      renderResults();
    });
    byId("purchase-filter").addEventListener("change", function (event) {
      state.purchaseId = event.target.value;
      renderResults();
    });
    byId("clear-filters").addEventListener("click", function () {
      state.status = "all";
      state.purchaseId = "all";
      byId("status-filter").value = "all";
      byId("purchase-filter").value = "all";
      renderResults();
    });
    byId("zoom-in").addEventListener("click", function () { changeZoom(1.25); });
    byId("zoom-out").addEventListener("click", function () { changeZoom(0.8); });
    byId("zoom-reset").addEventListener("click", resetView);
    byId("pan-left").addEventListener("click", function () { pan(120, 0); });
    byId("pan-right").addEventListener("click", function () { pan(-120, 0); });
    byId("print-report").addEventListener("click", function () { globalThis.print(); });
    byId("download-svg").addEventListener("click", downloadSvg);
    byId("annotation-form").addEventListener("submit", saveAnnotation);
    byId("annotation-close").addEventListener("click", closeAnnotation);
    byId("annotation-cancel").addEventListener("click", closeAnnotation);
    byId("annotation-dialog").addEventListener("cancel", function (event) {
      if (state.saving) {
        event.preventDefault();
      }
    });

    const svg = byId("sof-svg");
    svg.addEventListener("wheel", function (event) {
      event.preventDefault();
      changeZoom(event.deltaY < 0 ? 1.12 : 0.89);
    }, { passive: false });
    svg.addEventListener("pointerdown", function (event) {
      if (event.button !== 0 || !state.layout || state.layout.nodes.length === 0) {
        return;
      }
      state.drag = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, panX: state.panX, panY: state.panY };
      state.dragged = false;
      svg.classList.add("is-dragging");
      svg.setPointerCapture(event.pointerId);
    });
    svg.addEventListener("pointermove", function (event) {
      if (!state.drag || state.drag.pointerId !== event.pointerId) {
        return;
      }
      const bounds = svg.getBoundingClientRect();
      const scaleX = bounds.width ? state.viewportWidth / bounds.width : 1;
      const scaleY = bounds.height ? state.viewportHeight / bounds.height : 1;
      const deltaX = (event.clientX - state.drag.x) * scaleX;
      const deltaY = (event.clientY - state.drag.y) * scaleY;
      if (Math.abs(deltaX) + Math.abs(deltaY) > 3) {
        state.dragged = true;
      }
      state.panX = state.drag.panX + deltaX;
      state.panY = state.drag.panY + deltaY;
      updateGraphTransform();
    });
    const finishDrag = function (event) {
      if (!state.drag || state.drag.pointerId !== event.pointerId) {
        return;
      }
      state.drag = null;
      svg.classList.remove("is-dragging");
      if (svg.hasPointerCapture(event.pointerId)) {
        svg.releasePointerCapture(event.pointerId);
      }
      requestAnimationFrame(function () { state.dragged = false; });
    };
    svg.addEventListener("pointerup", finishDrag);
    svg.addEventListener("pointercancel", finishDrag);
    globalThis.addEventListener("beforeprint", function () {
      const viewport = svg.querySelector(".graph-viewport");
      if (viewport) {
        state.printTransform = viewport.getAttribute("transform");
        viewport.removeAttribute("transform");
      }
    });
    globalThis.addEventListener("afterprint", function () {
      const viewport = svg.querySelector(".graph-viewport");
      if (viewport && state.printTransform) {
        viewport.setAttribute("transform", state.printTransform);
      } else {
        updateGraphTransform();
      }
    });
  }

  renderNotices();
  renderStorageStatus();
  renderPurchaseOptions();
  renderMetadata();
  renderWarnings();
  renderResults();
  attachEvents();
}());
