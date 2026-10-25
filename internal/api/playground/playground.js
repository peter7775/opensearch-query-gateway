"use strict";

const form = document.getElementById("query-form");
const query = document.getElementById("query");
const submit = document.getElementById("translate");
const status = document.getElementById("status");
const output = document.getElementById("dsl");
const normalized = document.getElementById("normalized");
const copy = document.getElementById("copy");
let currentDSL = "";
let activeRequest;
let revision = 0;

function message(text, state = "") {
  status.textContent = text;
  status.dataset.state = state;
}

function aliases(items, fallback) {
  normalized.replaceChildren();
  const lines = items.length ? items.map(item => `${item.from} → ${item.to}`) : [fallback];
  for (const text of lines) {
    const li = document.createElement("li");
    li.textContent = text;
    normalized.append(li);
  }
}

function invalidate() {
  revision++;
  activeRequest?.abort();
  submit.disabled = false;
  submit.textContent = "Translate query →";
  currentDSL = "";
  copy.disabled = true;
  output.textContent = "Query changed. Translate to update the DSL.";
  aliases([], "Translate to inspect field aliases.");
  message("Ready to translate.");
}

async function translate() {
  activeRequest?.abort();
  const request = new AbortController();
  activeRequest = request;
  const version = ++revision;
  const timer = setTimeout(() => request.abort(), 10000);
  currentDSL = "";
  copy.disabled = true;
  submit.disabled = true;
  submit.textContent = "Translating…";
  output.textContent = "Parsing and validating your query…";
  aliases([], "Waiting for validation.");
  message("Translating…");
  try {
    const response = await fetch("/v1/translate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: query.value }),
      signal: request.signal
    });
    const data = await response.json();
    if (version !== revision) return;
    if (!response.ok) {
      const error = data.error || {};
      message(`${error.code || response.status}: ${error.message || "Translation failed."}`, "error");
      output.textContent = JSON.stringify(data, null, 2);
      aliases([], "No translation: query was not accepted.");
      return;
    }
    currentDSL = JSON.stringify(data.dsl, null, 2);
    output.textContent = currentDSL;
    aliases(data.normalized_fields || [], "No aliases applied.");
    message(`Translated successfully · index: ${data.index} · no search executed`, "success");
    copy.disabled = false;
  } catch (error) {
    if (version !== revision) return;
    message(error.name === "AbortError" ? "Request timed out. Try again." : "Cannot reach the translator. Check your connection and try again.", "error");
    output.textContent = "No DSL available.";
    aliases([], "No translation available.");
  } finally {
    clearTimeout(timer);
    if (version === revision) {
      submit.disabled = false;
      submit.textContent = "Translate query →";
    }
  }
}

form.addEventListener("submit", event => {
  event.preventDefault();
  translate();
});
query.addEventListener("input", invalidate);
query.addEventListener("keydown", event => {
  if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
    event.preventDefault();
    form.requestSubmit();
  }
});
document.getElementById("examples").addEventListener("click", event => {
  const button = event.target.closest("button[data-query]");
  if (!button) return;
  query.value = button.dataset.query;
  invalidate();
  query.focus();
  translate();
});
copy.addEventListener("click", async () => {
  const value = currentDSL;
  const version = revision;
  try {
    await navigator.clipboard.writeText(value);
    if (version === revision) message("DSL copied to clipboard.", "success");
  } catch {
    if (version === revision) message("Clipboard unavailable. Select and copy the JSON manually.", "error");
  }
});
translate();
