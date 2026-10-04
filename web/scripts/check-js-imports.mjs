import { readdir, readFile, access } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = resolve(fileURLToPath(new URL('.', import.meta.url)));
const moduleDir = resolve(scriptDir, '../../internal/web/static/js');
const cssInput = await readFile(resolve(scriptDir, '../input.css'), 'utf8');
const cssBundle = await readFile(resolve(moduleDir, '../css/paratrack.css'), 'utf8');
const baseTemplate = await readFile(resolve(moduleDir, '../../templates/base.html'), 'utf8');
const offlineSource = await readFile(resolve(moduleDir, 'app-offline.js'), 'utf8');
const templatesDir = resolve(moduleDir, '../../templates');
const files = (await readdir(moduleDir)).filter((name) => /^app.*\.js$/.test(name));
const templateFiles = (await readdir(templatesDir)).filter((name) => name.endsWith('.html'));
const importPatterns = [
  /\b(?:import|export)\s+(?:[^'";]*?\s+from\s+)?(['"])([^'"\n]+)\1/g,
  /\bimport\(\s*(['"])([^'"\n]+)\1\s*\)/g,
];
const errors = [];
const graph = new Map();
const alpineComponents = new Set();
const entrypoints = new Set(
  [...baseTemplate.matchAll(/<script\b[^>]*\bsrc="\{\{asset "js\/(app[^"]*\.js)"\}\}"[^>]*>/g)]
    .map((match) => match[1]),
);

if (!entrypoints.has('app.js')) {
  errors.push('base.html must load app.js as the browser module entrypoint');
}

for (const policyCall of [
  'isOfflineQueueableMutation(verb, new URL(url, window.location.href).pathname)',
  'isOfflineQueueableMutation(item.verb, pathname)',
]) {
  if (!offlineSource.includes(policyCall)) {
    errors.push(`app-offline.js must enforce offline mutation policy at enqueue and replay (${policyCall})`);
  }
}

for (const source of [
  '@source "../internal/web/static/js/app.js";',
  '@source "../internal/web/static/js/app-*.js";',
]) {
  if (!cssInput.includes(source)) errors.push(`Tailwind input is missing browser module source ${source}`);
}

for (const utility of ['opacity-0', 'transition-opacity', 'duration-200']) {
  if (!cssBundle.includes(`.${utility}`)) errors.push(`compiled CSS is missing JavaScript utility .${utility}`);
}

for (const file of files) {
  const source = await readFile(resolve(moduleDir, file), 'utf8');
  if (/(?:window|globalThis)\s*(?:\.\s*[A-Za-z_$][\w$]*|\[\s*["'][^"']+["']\s*\])\s*=(?!=)/.test(source)) {
    errors.push(`${file}: browser modules must not publish mutable state on window or globalThis`);
  }
  for (const match of source.matchAll(/Alpine\.data\(\s*['"]([^'"]+)['"]/g)) {
    alpineComponents.add(match[1]);
  }
  const imports = [];
  for (const pattern of importPatterns) {
    for (const match of source.matchAll(pattern)) {
      const specifier = match[2];
      if (!specifier.startsWith('/static/js/')) {
        errors.push(`${file}: unsupported browser module URL ${specifier}`);
        continue;
      }
      const target = resolve(moduleDir, specifier.slice('/static/js/'.length));
      if (!target.startsWith(moduleDir + '/') || !target.endsWith('.js')) {
        errors.push(`${file}: invalid browser module URL ${specifier}`);
        continue;
      }
      try {
        await access(target);
        imports.push(target.slice(moduleDir.length + 1));
      } catch {
        errors.push(`${file}: missing imported module ${specifier}`);
      }
    }
  }
  graph.set(file, imports);
}

for (const file of templateFiles) {
  const template = await readFile(resolve(templatesDir, file), 'utf8');
  for (const match of template.matchAll(/\bx-data="([^"]+)"/g)) {
    const component = match[1].match(/^\s*([A-Za-z_$][\w$]*)/);
    if (component && !alpineComponents.has(component[1])) {
      errors.push(`${file}: x-data references unregistered Alpine component ${component[1]}`);
    }
  }
}

const visiting = new Set();
const visited = new Set();
function visit(file, path = []) {
  if (visiting.has(file)) {
    errors.push(`browser module import cycle: ${[...path, file].join(' -> ')}`);
    return;
  }
  if (visited.has(file)) return;
  visiting.add(file);
  for (const dependency of graph.get(file) || []) visit(dependency, [...path, file]);
  visiting.delete(file);
  visited.add(file);
}
for (const file of graph.keys()) visit(file);

// Browser modules are shipped as individual static assets, so an orphaned
// app module otherwise remains syntactically valid and silently ships unused.
// HTML script tags define module entrypoints; every other app module must be
// reachable from one of those roots through the import graph.
const reachable = new Set();
function markReachable(file) {
  if (reachable.has(file)) return;
  reachable.add(file);
  for (const dependency of graph.get(file) || []) markReachable(dependency);
}
for (const entrypoint of entrypoints) {
  if (!graph.has(entrypoint)) {
    errors.push(`base.html references missing browser module ${entrypoint}`);
  } else {
    markReachable(entrypoint);
  }
}
for (const file of graph.keys()) {
  if (!reachable.has(file)) {
    errors.push(`${file}: app module is unreachable from app.js`);
  }
}

if (errors.length) {
  console.error(errors.join('\n'));
  process.exitCode = 1;
} else {
  console.log(`checked imports, cycles, reachability, Alpine registrations and global state in ${files.length} browser modules`);
}
