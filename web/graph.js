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
    const nodeWidth = 240;
    const nodeHeight = 108;
    for (const item of sorted) {
      const row = columnRows[item.depth];
      positions[item.index] = {
        id: nodes[item.index].id,
        depth: item.depth,
        row: row,
        x: 32 + (maxDepth - item.depth) * 320,
        y: 32 + row * 158,
        width: nodeWidth,
        height: nodeHeight
      };
      columnRows[item.depth] += 1;
    }
    return {
      nodes: nodes,
      edges: edges,
      positions: positions,
      width: 64 + (maxDepth + 1) * 320,
      height: Math.max(180, 64 + Math.max.apply(null, columnRows) * 158),
      maxDepth: maxDepth
    };
  }

  function formatSats(value) {
    if (!Number.isSafeInteger(value)) {
      return "—";
    }
    return value.toLocaleString("en-US") + " sats";
  }

  return Object.freeze({
    uniqueStrings: uniqueStrings,
    utxoMatches: utxoMatches,
    filterUtxos: filterUtxos,
    normalizeGraph: normalizeGraph,
    selectGraph: selectGraph,
    layoutGraph: layoutGraph,
    formatSats: formatSats
  });
}));
