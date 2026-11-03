// SPDX-License-Identifier: Apache-2.0
//
// Minimal stdlib static server for the portal's web/static/ directory,
// used as the Playwright `webServer`. The portal normally serves these
// files from the Go binary; for e2e we only need them served with the
// right MIME types (app.js / topics.js are ES modules and must be
// `text/javascript` or the browser refuses to execute them). No backend
// — the specs mock /api/* via page.route.
//
// No dependencies (Node stdlib only) so CI doesn't fetch a static-server
// package at runtime.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { extname, normalize, join } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = join(fileURLToPath(new URL(".", import.meta.url)), "..", "static");
const PORT = Number(process.env.PORT) || 8788;

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".ico": "image/x-icon",
  ".png": "image/png",
  ".woff2": "font/woff2",
};

const server = createServer(async (req, res) => {
  // Strip query string; default "/" to the topics page under test.
  let path = decodeURIComponent((req.url || "/").split("?")[0]);
  if (path === "/") path = "/topics.html";
  // Contain to ROOT — reject path traversal.
  const full = normalize(join(ROOT, path));
  if (!full.startsWith(ROOT)) {
    res.writeHead(403).end("forbidden");
    return;
  }
  try {
    const body = await readFile(full);
    res.writeHead(200, { "Content-Type": MIME[extname(full)] || "application/octet-stream" });
    res.end(body);
  } catch {
    res.writeHead(404).end("not found");
  }
});

server.listen(PORT, "127.0.0.1", () => {
  // eslint-disable-next-line no-console
  console.log(`portal-web-e2e static server: http://127.0.0.1:${PORT} (root ${ROOT})`);
});
