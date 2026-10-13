#!/usr/bin/env bun
// Merge the pnpm workspace configuration into the Readest root package.json so
// Bun understands the monorepo layout and dependency overrides.
//
// The upstream Readest repo drives its monorepo with pnpm, so the workspace
// globs, dependency `overrides`, and `patchedDependencies` all live in
// `pnpm-workspace.yaml` rather than in `package.json`. Bun only reads those from
// `package.json`, so a plain `bun install` in the mirrored tree links no
// workspace packages and resolves nothing. This script copies the relevant
// fields across, producing the package.json that the build actually installs
// from. It is idempotent and only writes when the result changes.
//
// Usage: bun scripts/prepare-readest-package.js <readest-tree-dir>

import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const dir = process.argv[2];
if (!dir) {
  console.error('usage: prepare-readest-package.js <readest-tree-dir>');
  process.exit(2);
}

const pkgPath = join(dir, 'package.json');
const wsPath = join(dir, 'pnpm-workspace.yaml');
if (!existsSync(pkgPath) || !existsSync(wsPath)) {
  console.error(`missing package.json or pnpm-workspace.yaml in ${dir}`);
  process.exit(1);
}

const pkg = JSON.parse(readFileSync(pkgPath, 'utf8'));

// Pull the fields we need out of pnpm-workspace.yaml. The file is simple
// top-level YAML (lists and 1-level maps of string values), so parse it directly
// instead of pulling in a YAML dependency the Bun builder image may not have.
const yaml = readFileSync(wsPath, 'utf8');

function parseStringList(key) {
  const lines = yaml.split('\n');
  const out = [];
  let inKey = false;
  for (const raw of lines) {
    const line = raw.replace(/\s+$/, '');
    if (!inKey) {
      if (line.startsWith(`${key}:`)) inKey = true;
      continue;
    }
    if (line.trim() === '') continue;
    const m = line.match(/^\s*-\s+(.*)$/);
    if (!m) break; // next top-level key
    out.push(m[1].replace(/^['"]|['"]$/g, ''));
  }
  return out;
}

function parseStringMap(key) {
  const lines = yaml.split('\n');
  const out = {};
  let inKey = false;
  for (const raw of lines) {
    const line = raw.replace(/\s+$/, '');
    if (!inKey) {
      if (line.startsWith(`${key}:`)) inKey = true;
      continue;
    }
    if (line.trim() === '') continue;
    const m = line.match(/^\s+(.+?):\s*(.*)$/);
    if (!m) break;
    const k = m[1].replace(/^['"]|['"]$/g, '');
    out[k] = m[2].replace(/^['"]|['"]$/g, '');
  }
  return out;
}

pkg.workspaces = parseStringList('packages');
const overrides = parseStringMap('overrides');
if (Object.keys(overrides).length) pkg.overrides = overrides;
const patches = parseStringMap('patchedDependencies');
if (Object.keys(patches).length) pkg.patchedDependencies = patches;

const next = JSON.stringify(pkg, null, 2) + '\n';
const prev = readFileSync(pkgPath, 'utf8');
if (prev !== next) writeFileSync(pkgPath, next);
