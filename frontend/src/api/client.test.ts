import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ApiError, createClient, withToken } from './client';

test('authenticated JSON requests retain caller headers and cancellation', async () => {
  const controller = new AbortController();
  const api = createClient({ token: () => 'private-token', fetch: async (url, init) => {
    assert.equal(url, '/api/tasks');
    const headers = new Headers(init?.headers);
    assert.equal(headers.get('Authorization'), 'Bearer private-token');
    assert.equal(headers.get('X-Request-ID'), 'one');
    assert.equal(headers.get('Content-Type'), 'application/json');
    assert.equal(init?.signal, controller.signal);
    assert.equal(init?.body, '{"priority":0}');
    return Response.json({ id: 7 });
  }});
  assert.deepEqual(await api('/tasks', { method: 'POST', body: { priority: 0 }, headers: { 'X-Request-ID': 'one' }, signal: controller.signal }), { id: 7 });
});

test('multipart attachment upload leaves boundary generation to the browser', async () => {
  const form = new FormData();
  form.append('file', new Blob(['hello']), 'note.txt');
  const api = createClient({ token: () => '', fetch: async (_, init) => {
    assert.equal(init?.body, form);
    assert.equal(new Headers(init?.headers).has('Content-Type'), false);
    return new Response(null, { status: 204 });
  }});
  assert.equal(await api('/attachments', { method: 'POST', body: form }), null);
});

test('401 notifies the UI and keeps the server error without retrying a mutation', async () => {
  let calls = 0, unauthorized = 0;
  const api = createClient({ token: () => '', onUnauthorized: () => unauthorized++, fetch: async () => {
    calls++;
    return Response.json({ detail: 'Token expired' }, { status: 401 });
  }});
  await assert.rejects(api('/sessions', { method: 'POST', body: {} }), (error: unknown) => error instanceof ApiError && error.status === 401 && error.message === 'Token expired');
  assert.equal(calls, 1);
  assert.equal(unauthorized, 1);
});

test('non-JSON proxy failures retain their HTTP status', async () => {
  const api = createClient({ token: () => '', fetch: async () => new Response('<html>Unavailable</html>', { status: 502, statusText: 'Bad Gateway' }) });
  await assert.rejects(api('/tasks'), (error: unknown) => error instanceof ApiError && error.status === 502 && error.message === 'Bad Gateway');
});

test('EventSource token is encoded before fragments and preserves existing queries', () => {
  assert.equal(withToken('/api/stream?after=2#view', 'a&b'), '/api/stream?after=2&token=a%26b#view');
  assert.equal(withToken('/api/stream', ''), '/api/stream');
});
