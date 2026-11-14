/*
 * TRACEY.diy presentation inspiration: layered, zoomable SVG graphs.
 * Upstream: https://github.com/L0laL33tz/TRACEY.diy
 * Copyright (c) 2026 tracey.diy. MIT License.
 * The graph algorithms and source-of-funds implementation here are independent.
 */
(function (root, factory) {
  "use strict";
  if (typeof module === "object" && module.exports) {
    module.exports = factory();
  } else {
    root.SOFGraph = factory();
  }
}(globalThis, function () {
  "use strict";

  function uniqueStrings(values) {
    const seen = new Set();
    if (!Array.isArray(values)) {
      return [];
    }
    for (const value of values) {
      if (typeof value === "string" && value.length > 0) {
        seen.add(value);
      }
    }
    return Array.from(seen);
  }

  function utxoMatches(utxo, filters) {
    if (!utxo || typeof utxo !== "object" || Array.isArray(utxo)) {
      return false;
    }
    const options = filters && typeof filters === "object" ? filters : {};
    const status = typeof options.status === "string" ? options.status : "all";
    const purchase = typeof options.purchaseId === "string" ? options.purchaseId : "all";
    const statusMatches = status === "all" || utxo.documentation_status === status;
    const purchaseMatches = purchase === "all" ||
      (Array.isArray(utxo.purchase_ids) && utxo.purchase_ids.includes(purchase));
    return statusMatches && purchaseMatches;
  }

  function filterUtxos(utxos, filters) {
    return (Array.isArray(utxos) ? utxos : []).filter(function (utxo) {
      return utxoMatches(utxo, filters);
    });
  }

  function normalizeGraph(graph) {
    const source = graph && typeof graph === "object" ? graph : {};
    const nodes = [];
    const ids = new Set();
    const inputNodes = Array.isArray(source.nodes) ? source.nodes : [];
    for (const node of inputNodes) {
      if (node && typeof node === "object" && !Array.isArray(node) &&
          typeof node.id === "string" && node.id.length > 0 && !ids.has(node.id)) {
        ids.add(node.id);
        nodes.push(node);
      }
    }
    const edges = [];
    const inputEdges = Array.isArray(source.edges) ? source.edges : [];
    for (const edge of inputEdges) {
      if (edge && typeof edge === "object" && !Array.isArray(edge) &&
          typeof edge.from === "string" && typeof edge.to === "string" &&
          ids.has(edge.from) && ids.has(edge.to)) {
        edges.push(edge);
      }
    }
    return { nodes: nodes, edges: edges };
  }

  function selectGraph(graph, rootOutpoints) {
    const normalized = normalizeGraph(graph);
    const roots = new Set(uniqueStrings(rootOutpoints));
    const included = new Set();
    const outgoing = new Map();
    for (const node of normalized.nodes) {
      outgoing.set(node.id, []);
    }
    for (const edge of normalized.edges) {
      outgoing.get(edge.from).push(edge.to);
    }
    const queue = normalized.nodes.filter(function (node) {
      return roots.has(node.id);
    }).map(function (node) {
      included.add(node.id);
      return node.id;
    });
    for (let cursor = 0; cursor < queue.length; cursor += 1) {
      const id = queue[cursor];
      for (const next of outgoing.get(id)) {
        if (!included.has(next)) {
          included.add(next);
          queue.push(next);
        }
      }
    }
    return {
      nodes: normalized.nodes.filter(function (node) { return included.has(node.id); }),
      edges: normalized.edges.filter(function (edge) {
        return included.has(edge.from);
      })
    };
  }

  function monthOf(value) {
    return typeof value === "string" && /^[0-9]{4}-[0-9]{2}/.test(value) ? value.slice(0, 7) : "";
  }

  function expandMonths(first, last) {
    const months = [];
    let year = Number(first.slice(0, 4));
    let month = Number(first.slice(5, 7));
    const endYear = Number(last.slice(0, 4));
    const endMonth = Number(last.slice(5, 7));
    while (year < endYear || (year === endYear && month <= endMonth)) {
      months.push(year + "-" + String(month).padStart(2, "0"));
      month += 1;
      if (month > 12) {
        month = 1;
        year += 1;
      }
    }
    return months;
  }

  // layoutGraphTime places one column per calendar month. Unlike the hop
  // layout, x is strictly chronological (oldest left, newest right); nodes with
  // no known date are grouped in a leading "undated" column.
  function layoutGraphTime(graph, dateOf) {
    const normalized = normalizeGraph(graph);
    const nodes = normalized.nodes;
    const edges = normalized.edges;
    if (nodes.length === 0) {
      return { nodes: [], edges: [], positions: [], headers: [], width: 0, height: 0, maxDepth: -1 };
    }
    const dateFor = typeof dateOf === "function" ? dateOf : function () { return ""; };
    const dates = new Map();
    const buckets = new Map();
    for (const node of nodes) {
      const value = typeof dateFor(node) === "string" ? dateFor(node) : "";
      const key = monthOf(value);
      dates.set(node.id, value);
      if (!buckets.has(key)) {
        buckets.set(key, []);
      }
      buckets.get(key).push(node);
    }
    const dated = Array.from(buckets.keys()).filter(function (key) { return key !== ""; }).sort();
    let keys = dated;
    if (dated.length > 0) {
      keys = expandMonths(dated[0], dated[dated.length - 1]);
    }
    if (buckets.has("")) {
      keys = [""].concat(keys);
    }
    const nodeWidth = 300;
    const nodeHeight = 150;
    const positions = new Array(nodes.length);
    const index = new Map(nodes.map(function (node, position) { return [node.id, position]; }));
    const headers = [];
    let maxRows = 0;
    keys.forEach(function (key, column) {
      const entries = (buckets.get(key) || []).slice().sort(function (left, right) {
        const leftDate = dates.get(left.id) || "";
        const rightDate = dates.get(right.id) || "";
        if (leftDate !== rightDate) {
          return leftDate.localeCompare(rightDate);
        }
        return left.id.localeCompare(right.id);
      });
      const x = 190 + column * 340;
      headers.push({ label: key || "undated", x: x - 140, y: 20 });
      entries.forEach(function (node, row) {
        positions[index.get(node.id)] = {
          id: node.id,
          depth: column,
          row: row,
          x: x,
          y: 110 + row * 210,
          width: nodeWidth,
          height: nodeHeight
        };
      });
      maxRows = Math.max(maxRows, entries.length);
    });
    return {
      nodes: nodes,
      edges: edges,
      positions: positions,
      headers: headers,
      width: 340 * keys.length + 40,
      height: Math.max(240, 20 + maxRows * 210),
      maxDepth: keys.length - 1
    };
  }

  function layoutGraph(graph) {
    const normalized = normalizeGraph(graph);
    const nodes = normalized.nodes;
    const edges = normalized.edges;
    const count = nodes.length;
    if (count === 0) {
      return { nodes: [], edges: [], positions: [], width: 0, height: 0, maxDepth: -1 };
    }
    const indices = new Map(nodes.map(function (node, index) { return [node.id, index]; }));
    const indegree = new Array(count).fill(0);
    const depth = new Array(count).fill(0);
    const outgoing = new Array(count);
    for (let index = 0; index < count; index += 1) {
      outgoing[index] = [];
    }
    for (const edge of edges) {
      const from = indices.get(edge.from);
      const to = indices.get(edge.to);
      outgoing[from].push(to);
      indegree[to] += 1;
    }
    const queue = [];
    for (let index = 0; index < count; index += 1) {
      if (indegree[index] === 0) {
        queue.push(index);
      }
    }
    for (let cursor = 0; cursor < queue.length; cursor += 1) {
      const from = queue[cursor];
      for (const to of outgoing[from]) {
        depth[to] = Math.max(depth[to], depth[from] + 1);
        indegree[to] -= 1;
        if (indegree[to] === 0) {
          queue.push(to);
        }
      }
    }
    const maxDepth = depth.reduce(function (maximum, value) {
      return Math.max(maximum, value);
    }, 0);
    const columnRows = new Array(maxDepth + 1).fill(0);
    const sorted = nodes.map(function (node, index) {
      return { index: index, depth: depth[index] };
    }).sort(function (left, right) {
      return left.depth - right.depth || left.index - right.index;
    });
    const positions = new Array(count);
    const nodeWidth = 300;
    const nodeHeight = 150;
    for (const item of sorted) {
      const row = columnRows[item.depth];
      positions[item.index] = {
        id: nodes[item.index].id,
        depth: item.depth,
        row: row,
        x: 32 + (maxDepth - item.depth) * 380,
        y: 32 + row * 210,
        width: nodeWidth,
        height: nodeHeight
      };
      columnRows[item.depth] += 1;
    }
    return {
      nodes: nodes,
      edges: edges,
      positions: positions,
      width: 64 + (maxDepth + 1) * 380,
      height: Math.max(260, 64 + Math.max.apply(null, columnRows) * 210),
      maxDepth: maxDepth
    };
  }

  function formatSats(value) {
    if (!Number.isSafeInteger(value)) {
      return "—";
    }
    return value.toLocaleString("en-US") + " sats";
  }

  function formatBtc(value) {
    if (!Number.isSafeInteger(value)) {
      return "—";
    }
    const negative = value < 0;
    const absolute = Math.abs(value);
    const whole = Math.floor(absolute / 100000000);
    const fraction = String(absolute % 100000000).padStart(8, "0");
    return (negative ? "-" : "") + whole + "." + fraction + " BTC";
  }

  return Object.freeze({
    uniqueStrings: uniqueStrings,
    utxoMatches: utxoMatches,
    filterUtxos: filterUtxos,
    normalizeGraph: normalizeGraph,
    selectGraph: selectGraph,
    layoutGraph: layoutGraph,
    layoutGraphTime: layoutGraphTime,
    monthOf: monthOf,
    formatSats: formatSats,
    formatBtc: formatBtc
  });
}));
