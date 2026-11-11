#!/usr/bin/env node
// Checks relative markdown links. Optional: --expect-fail <file> must exit 1.
import { readdirSync, readFileSync, statSync, existsSync } from "node:fs";
import { dirname, join, resolve, normalize } from "node:path";

const root = resolve(new URL("..", import.meta.url).pathname);
const expectFail = process.argv[2] === "--expect-fail" ? process.argv[3] : "";

function walk(dir, acc = []) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".git" || name === "broken-link.md") continue;
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) walk(p, acc);
    else if (name.endsWith(".md")) acc.push(p);
  }
  return acc;
}

function linksOf(text) {
  const out = [];
  const re = /\[[^\]]+\]\(([^)]+)\)/g;
  let m;
  while ((m = re.exec(text))) {
    const raw = m[1].trim();
    if (!raw || raw.startsWith("http:") || raw.startsWith("https:") || raw.startsWith("mailto:")) continue;
    out.push(raw.split("#")[0]);
  }
  return out.filter(Boolean);
}

function checkFile(file) {
  const text = readFileSync(file, "utf8");
  const missing = [];
  for (const rel of linksOf(text)) {
    const target = normalize(resolve(dirname(file), rel));
    if (!existsSync(target)) missing.push(rel);
  }
  return missing;
}

if (expectFail) {
  const file = resolve(root, expectFail);
  const missing = checkFile(file);
  if (missing.length === 0) {
    console.error("expected broken links in", expectFail);
    process.exit(1);
  }
  console.log("expect-fail ok", missing.join(", "));
  process.exit(0);
}

const files = walk(root);
let bad = 0;
for (const file of files) {
  const missing = checkFile(file);
  if (missing.length) {
    bad++;
    console.error(file, "missing", missing.join(", "));
  }
}
const openapi = join(root, "docs/architecture/openapi.yaml");
const spec = readFileSync(openapi, "utf8");
if (!spec.includes("openapi: 3.1.0") || !spec.includes('pattern: "^[0-9]+$"')) {
  console.error("openapi money/version contract missing");
  bad++;
}
if (bad) process.exit(1);
console.log("docs-check ok", files.length, "files");
