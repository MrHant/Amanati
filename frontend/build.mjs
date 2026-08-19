// Copies the static shell and the two vendored libraries into dist/.
// Tailwind writes dist/assets/app.css alongside them; Wails embeds the folder.
// There is no bundler on purpose: HTMX and Alpine are used from <script> tags.

import { copyFile, mkdir, rm, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(fileURLToPath(import.meta.url));
const dist = resolve(root, 'dist');

const copies = [
  ['src/index.html', 'dist/index.html'],
  ['node_modules/htmx.org/dist/htmx.min.js', 'dist/assets/htmx.min.js'],
  ['node_modules/alpinejs/dist/cdn.min.js', 'dist/assets/alpine.min.js'],
];

await rm(dist, { recursive: true, force: true });
await mkdir(resolve(dist, 'assets'), { recursive: true });

// main.go embeds this directory, so it has to exist in a fresh checkout too.
await writeFile(resolve(dist, '.gitkeep'), '');

for (const [from, to] of copies) {
  const target = resolve(root, to);
  await mkdir(dirname(target), { recursive: true });
  await copyFile(resolve(root, from), target);
}

console.log(`copied ${copies.length} files into frontend/dist`);
