import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

// Execute the shipped decoder/classifier; browser tests exercise its DOM callers.
const source = fs.readFileSync(new URL('../static/app.js', import.meta.url), 'utf8');
const decoder = source.slice(source.indexOf('export class ApiError'), source.indexOf('export async function api(')).replace(/^export /gm, '');
const { readResponse, isAuthError } = new Function(decoder + '\nreturn {readResponse, isAuthError};')();

for (const detail of ['vendor scope mismatch', 'operator-only endpoint']) {
  test(`403 preserves permission detail: ${detail}`, async () => {
    await assert.rejects(readResponse(new Response(JSON.stringify({error: {message: detail}}), {
      status: 403, headers: {'Content-Type': 'application/json'},
    })), error => error.status === 403 && error.category === 'permission' && error.message === detail && !isAuthError(error));
  });
}
test('401 retains the sign-in path without exposing server detail', async () => {
  await assert.rejects(readResponse(new Response(JSON.stringify({error: {message: 'internal authentication detail'}}), {
    status: 401, headers: {'Content-Type': 'application/json'},
  })), error => isAuthError(error) && error.category === 'auth' && error.message === 'Authentication failed. Check your API key.');
});
for (const body of ['', '{', '<html>upstream failure</html>', '{"error":{"message":{}}}']) {
  test(`403 fallback for unusable detail: ${JSON.stringify(body)}`, async () => {
    await assert.rejects(readResponse(new Response(body, {status: 403, headers: {'Content-Type': 'application/json'}})),
      error => error.message === 'Permission denied.' && !isAuthError(error));
  });
}
test('403 accepts a structured detail field', async () => {
  await assert.rejects(readResponse(new Response('{"detail":"permission required"}', {status: 403, headers: {'Content-Type': 'application/json'}})),
    error => error.message === 'permission required' && !isAuthError(error));
});
