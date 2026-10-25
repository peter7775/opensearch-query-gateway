const { test } = require("node:test");
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const vm = require("node:vm");

const source = readFileSync(join(__dirname, "../internal/api/playground/playground.js"), "utf8");
const tick = () => new Promise(resolve => setImmediate(resolve));

function setup(fetch, clipboard = async () => {}) {
  const elements = {};
  for (const id of ["query-form", "query", "translate", "status", "dsl", "normalized", "copy", "examples"]) {
    elements[id] = {
      value: "author:petr AND views:>=100", dataset: {}, children: [], disabled: false,
      textContent: "", listeners: {},
      addEventListener(name, fn) { this.listeners[name] = fn; },
      replaceChildren() { this.children = []; },
      append(item) { this.children.push(item); },
      focus() {},
      requestSubmit() { this.listeners.submit({ preventDefault() {} }); }
    };
  }
  vm.runInNewContext(source, {
    document: { getElementById: id => elements[id], createElement: () => ({ textContent: "" }) },
    navigator: { clipboard: { writeText: clipboard } },
    fetch, AbortController, setTimeout, clearTimeout
  });
  return elements;
}

const success = {
  index: "documents",
  dsl: { query: { match: { created_by: "petr" } }, size: 10 },
  normalized_fields: [{ from: "author", to: "created_by" }]
};
const response = (data = success, ok = true) => ({ ok, json: async () => data });

test("initial query renders DSL, aliases and can be copied", async () => {
  let copied;
  const ui = setup(async (url, options) => {
    assert.equal(url, "/v1/translate");
    assert.deepEqual(JSON.parse(options.body), { query: "author:petr AND views:>=100" });
    return response();
  }, async value => { copied = value; });
  await tick();
  assert.deepEqual(JSON.parse(ui.dsl.textContent), success.dsl);
  assert.equal(ui.normalized.children[0].textContent, "author → created_by");
  assert.equal(ui.copy.disabled, false);
  await ui.copy.listeners.click();
  assert.equal(copied, JSON.stringify(success.dsl, null, 2));
});

test("validation and rate-limit errors clear copyable DSL", async () => {
  for (const code of ["clause_rejected", "rate_limited"]) {
    const ui = setup(async () => response({ error: { code, message: "Try another query." } }, false));
    await tick();
    assert.equal(ui.status.dataset.state, "error");
    assert.ok(ui.status.textContent.includes(code));
    assert.equal(ui.copy.disabled, true);
    assert.equal(ui.translate.disabled, false);
  }
});

test("editing discards results and ignores an older in-flight response", async () => {
  let resolve;
  const ui = setup(() => new Promise(done => { resolve = done; }));
  ui.query.listeners.input();
  resolve(response());
  await tick();
  assert.equal(ui.dsl.textContent, "Query changed. Translate to update the DSL.");
  assert.equal(ui.copy.disabled, true);
});

test("example buttons submit their query and keyboard shortcut submits", async () => {
  const queries = [];
  const ui = setup(async (_, options) => {
    queries.push(JSON.parse(options.body).query);
    return response();
  });
  await tick();
  ui.examples.listeners.click({
    target: { closest: () => ({ dataset: { query: "tag:go* AND last:15m" } }) }
  });
  await tick();
  assert.equal(ui.query.value, "tag:go* AND last:15m");
  assert.equal(queries[1], ui.query.value);
  let prevented = false;
  ui.query.listeners.keydown({
    ctrlKey: true, key: "Enter", preventDefault() { prevented = true; }
  });
  await tick();
  assert.equal(prevented, true);
  assert.equal(queries.length, 3);
});

test("network errors and timeouts restore controls", async () => {
  for (const name of ["TypeError", "AbortError"]) {
    const ui = setup(async () => { throw Object.assign(new Error("offline"), { name }); });
    await tick();
    assert.equal(ui.status.dataset.state, "error");
    assert.equal(ui.translate.disabled, false);
    assert.equal(ui.copy.disabled, true);
    assert.equal(ui.dsl.textContent, "No DSL available.");
  }
});

test("clipboard failures explain manual copying", async () => {
  const ui = setup(async () => response(), async () => { throw new Error("denied"); });
  await tick();
  await ui.copy.listeners.click();
  assert.ok(ui.status.textContent.includes("copy the JSON manually"));
});

test("server strings render as text rather than HTML", async () => {
  const unsafe = "<img src=x onerror=alert(1)>";
  const ui = setup(async () => response({
    ...success, normalized_fields: [{ from: unsafe, to: "tags" }]
  }));
  await tick();
  assert.equal(ui.normalized.children[0].textContent, `${unsafe} → tags`);
  assert.equal(ui.normalized.children[0].innerHTML, undefined);
});
