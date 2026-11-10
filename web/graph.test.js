"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const graphPath = path.join(__dirname, "graph.js");
const graph = require(graphPath);

function node(id, extra) {
  return Object.assign({ id: id, kind: "output" }, extra);
}

function edge(from, to, kind) {
  return { from: from, to: to, kind: kind || "input" };
}

test("CommonJS export is dependency-free, complete, and immutable", function () {
  assert.equal(Object.isFrozen(graph), true);
  assert.deepEqual(Object.keys(graph).sort(), [
    "filterUtxos",
    "formatSats",
    "layoutGraph",
    "normalizeGraph",
    "selectGraph",
    "uniqueStrings",
    "utxoMatches"
  ]);
});

test("UMD installs SOFGraph on globalThis when CommonJS is unavailable", function () {
  const source = fs.readFileSync(graphPath, "utf8");
  const browser = vm.createContext({ console: console });
  new vm.Script(source, { filename: graphPath }).runInContext(browser);
  assert.equal(typeof browser.SOFGraph.layoutGraph, "function");
  assert.deepEqual(
    Array.from(browser.SOFGraph.uniqueStrings(["a", "a", "b"])),
    ["a", "b"]
  );

  const exportsAbsent = vm.createContext({ module: {} });
  new vm.Script(source, { filename: graphPath }).runInContext(exportsAbsent);
  assert.equal(typeof exportsAbsent.SOFGraph.selectGraph, "function");
});

test("uniqueStrings rejects malformed containers and values and de-duplicates in order", function () {
  for (const value of [undefined, null, false, "abc", 7, {}, new Set(["a"])]) {
    assert.deepEqual(graph.uniqueStrings(value), []);
  }
  assert.deepEqual(graph.uniqueStrings([]), []);
  assert.deepEqual(graph.uniqueStrings([
    "current:0", "", "current:0", 42, null, undefined, {}, ["nested"],
    "txid", "current:0", "txid", "last"
  ]), ["current:0", "txid", "last"]);
  assert.deepEqual(graph.uniqueStrings(["__proto__", "constructor", "__proto__"]),
    ["__proto__", "constructor"]);
});

test("utxoMatches rejects non-record UTXOs", function () {
  for (const value of [undefined, null, false, 0, "utxo", [], ["documented"]]) {
    assert.equal(graph.utxoMatches(value, {}), false);
  }
});

test("utxoMatches defaults malformed filters to all and requires both predicates", function () {
  const documented = {
    documentation_status: "documented",
    purchase_ids: ["purchase-a", "purchase-b"]
  };
  const partial = { documentation_status: "partially_documented", purchase_ids: ["purchase-b"] };
  for (const filters of [undefined, null, false, "documented", [], {}, { status: 3, purchaseId: null }]) {
    assert.equal(graph.utxoMatches(documented, filters), true);
  }
  assert.equal(graph.utxoMatches(documented, { status: "all", purchaseId: "all" }), true);
  assert.equal(graph.utxoMatches(documented, { status: "documented" }), true);
  assert.equal(graph.utxoMatches(documented, { status: "undocumented" }), false);
  assert.equal(graph.utxoMatches(documented, { purchaseId: "purchase-a" }), true);
  assert.equal(graph.utxoMatches(documented, { purchaseId: "missing" }), false);
  assert.equal(graph.utxoMatches({ documentation_status: "documented" }, { purchaseId: "purchase-a" }), false);
  assert.equal(graph.utxoMatches({ documentation_status: "documented", purchase_ids: null },
    { purchaseId: "purchase-a" }), false);
  assert.equal(graph.utxoMatches(partial, { status: "partially_documented", purchaseId: "purchase-b" }), true);
  assert.equal(graph.utxoMatches(partial, { status: "documented", purchaseId: "purchase-b" }), false);
  assert.equal(graph.utxoMatches(partial, { status: "partially_documented", purchaseId: "purchase-a" }), false);
});

test("filterUtxos handles malformed lists and combines documentation and purchase filters", function () {
  for (const value of [undefined, null, false, "utxos", {}, 4]) {
    assert.deepEqual(graph.filterUtxos(value, {}), []);
  }
  const utxos = [
    { outpoint: "a:0", documentation_status: "documented", purchase_ids: ["one"] },
    { outpoint: "b:0", documentation_status: "partially_documented", purchase_ids: ["one", "two"] },
    { outpoint: "c:0", documentation_status: "undocumented", purchase_ids: [] },
    null
  ];
  assert.deepEqual(graph.filterUtxos(utxos, {}).map(function (item) { return item.outpoint; }),
    ["a:0", "b:0", "c:0"]);
  assert.deepEqual(graph.filterUtxos(utxos, { status: "documented", purchaseId: "one" }),
    [utxos[0]]);
  assert.deepEqual(graph.filterUtxos(utxos, { status: "partially_documented", purchaseId: "two" }),
    [utxos[1]]);
  assert.deepEqual(graph.filterUtxos([], {}), []);
});

test("normalizeGraph accepts only uniquely identified node records and connected edge records", function () {
  for (const value of [undefined, null, false, "graph", 0, [], {}]) {
    assert.deepEqual(graph.normalizeGraph(value), { nodes: [], edges: [] });
  }
  assert.deepEqual(graph.normalizeGraph({ nodes: "bad", edges: "bad" }), { nodes: [], edges: [] });

  const first = node("a");
  const second = node("b");
  const proto = node("__proto__");
  const validEdge = edge("a", "b", "funded_by");
  const normalized = graph.normalizeGraph({
    nodes: [
      null, "node", 2, [], { id: 4 }, { id: "" }, {}, first, second, proto,
      node("a", { label: "duplicate is ignored" })
    ],
    edges: [
      null, "edge", 2, [], {}, { from: 4, to: "a" }, { from: "a", to: 4 },
      edge("missing", "a"), edge("a", "missing"), validEdge, edge("__proto__", "a")
    ]
  });
  assert.deepEqual(normalized.nodes, [first, second, proto]);
  assert.deepEqual(normalized.edges, [validEdge, edge("__proto__", "a")]);
  assert.strictEqual(normalized.nodes[0], first);
  assert.notEqual(normalized, graph.normalizeGraph({ nodes: [first], edges: [validEdge] }));
});

test("selectGraph returns an empty, safe subgraph for absent or unknown roots", function () {
  const source = { nodes: [node("a"), node("b")], edges: [edge("a", "b")] };
  for (const roots of [undefined, null, "a", {}, [], ["unknown"], [""], [null, 4]]) {
    assert.deepEqual(graph.selectGraph(source, roots), { nodes: [], edges: [] });
  }
});

test("selectGraph follows exact ancestry through shared paths without duplicates", function () {
  const currentA = node("a:0", { kind: "output", owned: true });
  const currentB = node("b:0", { kind: "output", owned: true });
  const txA = node("tx-a", { kind: "transaction" });
  const txB = node("tx-b", { kind: "transaction" });
  const previous = node("old:0", { kind: "output" });
  const origin = node("origin", { kind: "terminal" });
  const disconnected = node("other");
  const source = {
    nodes: [currentA, currentB, txA, txB, previous, origin, disconnected],
    edges: [
      edge("a:0", "tx-a", "funded_by"),
      edge("b:0", "tx-b", "funded_by"),
      edge("tx-a", "old:0"),
      edge("tx-b", "old:0"),
      edge("old:0", "origin"),
      edge("other", "origin")
    ]
  };
  assert.deepEqual(graph.selectGraph(source, ["a:0", "a:0"]), {
    nodes: [currentA, txA, previous, origin],
    edges: [source.edges[0], source.edges[2], source.edges[4]]
  });
  assert.deepEqual(graph.selectGraph(source, ["a:0", "b:0"]), {
    nodes: [currentA, currentB, txA, txB, previous, origin],
    edges: source.edges.slice(0, 5)
  });
  assert.deepEqual(graph.selectGraph(source, ["__proto__"]), { nodes: [], edges: [] });
});

test("layoutGraph represents empty and malformed graphs without phantom dimensions", function () {
  const expected = { nodes: [], edges: [], positions: [], width: 0, height: 0, maxDepth: -1 };
  for (const value of [undefined, null, "graph", {}, { nodes: [], edges: [] }, { nodes: null }]) {
    assert.deepEqual(graph.layoutGraph(value), expected);
  }
});

test("layoutGraph stacks disconnected roots deterministically in the current-UTXO column", function () {
  const source = { nodes: [node("current-b"), node("current-a")], edges: [] };
  const layout = graph.layoutGraph(source);
  assert.equal(layout.maxDepth, 0);
  assert.equal(layout.width, 384);
  assert.equal(layout.height, 380);
  assert.deepEqual(layout.positions.map(function (position) {
    return { id: position.id, depth: position.depth, row: position.row, x: position.x, y: position.y };
  }), [
    { id: "current-b", depth: 0, row: 0, x: 32, y: 32 },
    { id: "current-a", depth: 0, row: 1, x: 32, y: 190 }
  ]);
  assert.equal(layout.positions[0].width, 240);
  assert.equal(layout.positions[0].height, 108);
});

test("layoutGraph puts current outputs right and progressively older ancestry left", function () {
  const source = {
    nodes: [
      node("current:0", { owned: true }),
      node("funding-new", { kind: "transaction" }),
      node("previous:0"),
      node("funding-old", { kind: "transaction" }),
      node("coinbase", { kind: "terminal" })
    ],
    edges: [
      edge("current:0", "funding-new", "funded_by"),
      edge("funding-new", "previous:0"),
      edge("previous:0", "funding-old", "funded_by"),
      edge("funding-old", "coinbase")
    ]
  };
  const layout = graph.layoutGraph(source);
  assert.equal(layout.maxDepth, 4);
  assert.equal(layout.width, 1664);
  assert.equal(layout.height, 222);
  assert.deepEqual(layout.positions.map(function (position) { return position.depth; }), [0, 1, 2, 3, 4]);
  for (let index = 1; index < layout.positions.length; index += 1) {
    assert.ok(layout.positions[index].x < layout.positions[index - 1].x);
  }
  assert.equal(layout.positions[0].x, 32 + 4 * 320);
  assert.equal(layout.positions[4].x, 32);
});

test("layoutGraph uses longest-path layers for shared ancestry and stacks each column", function () {
  const source = {
    nodes: [
      node("left-root"), node("right-root"), node("join-short"), node("join-long"),
      node("shared"), node("origin")
    ],
    edges: [
      edge("left-root", "join-short"),
      edge("right-root", "join-long"),
      edge("join-long", "shared"),
      edge("join-short", "shared"),
      edge("shared", "origin")
    ]
  };
  const layout = graph.layoutGraph(source);
  const positions = new Map(layout.positions.map(function (position) { return [position.id, position]; }));
  assert.equal(positions.get("left-root").depth, 0);
  assert.equal(positions.get("right-root").depth, 0);
  assert.equal(positions.get("join-short").depth, 1);
  assert.equal(positions.get("join-long").depth, 1);
  assert.equal(positions.get("shared").depth, 2);
  assert.equal(positions.get("origin").depth, 3);
  assert.equal(positions.get("left-root").row, 0);
  assert.equal(positions.get("right-root").row, 1);
  assert.equal(positions.get("join-short").row, 0);
  assert.equal(positions.get("join-long").row, 1);
});

test("layoutGraph terminates on cycles, self-loops, and mixed cyclic acyclic components", function () {
  const source = {
    nodes: [node("cycle-a"), node("cycle-b"), node("self"), node("root"), node("child")],
    edges: [
      edge("cycle-a", "cycle-b"), edge("cycle-b", "cycle-a"),
      edge("self", "self"), edge("root", "child")
    ]
  };
  const layout = graph.layoutGraph(source);
  assert.equal(layout.nodes.length, 5);
  assert.equal(layout.edges.length, 4);
  assert.equal(layout.maxDepth, 1);
  const positions = new Map(layout.positions.map(function (position) { return [position.id, position]; }));
  assert.equal(positions.get("cycle-a").depth, 0);
  assert.equal(positions.get("cycle-b").depth, 0);
  assert.equal(positions.get("self").depth, 0);
  assert.equal(positions.get("root").depth, 0);
  assert.equal(positions.get("child").depth, 1);
  assert.equal(new Set(layout.positions.map(function (position) {
    return position.x + "," + position.y;
  })).size, 5);
});

test("normalizeGraph and selection preserve duplicate edges without causing duplicate traversal", function () {
  const source = {
    nodes: [node("a"), node("b")],
    edges: [edge("a", "b"), edge("a", "b")]
  };
  const selected = graph.selectGraph(source, ["a"]);
  assert.equal(selected.nodes.length, 2);
  assert.equal(selected.edges.length, 2);
  const layout = graph.layoutGraph(selected);
  assert.equal(layout.maxDepth, 1);
  assert.equal(layout.positions[1].depth, 1);
});

test("formatSats handles integer, negative, zero, and malformed numeric values", function () {
  assert.equal(graph.formatSats(0), "0 sats");
  assert.equal(graph.formatSats(123456789), "123,456,789 sats");
  assert.equal(graph.formatSats(-1234567), "-1,234,567 sats");
  assert.equal(graph.formatSats(Number.MAX_SAFE_INTEGER), "9,007,199,254,740,991 sats");
  for (const value of [undefined, null, "100", 1.25, NaN, Infinity, -Infinity,
    Number.MAX_SAFE_INTEGER + 1, Number.MIN_SAFE_INTEGER - 1, 100n]) {
    assert.equal(graph.formatSats(value), "—");
  }
});
