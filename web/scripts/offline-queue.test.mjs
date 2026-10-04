import assert from 'node:assert/strict';
import test from 'node:test';

import {
  encodeOfflineRequestPayload,
  isValidOfflineQueueItem,
  readOfflineQueue,
  sameOriginPath,
  writeOfflineQueue,
} from '../../internal/web/static/js/app-offline-queue.js';

test('queue storage treats corrupt or unavailable data as empty and reports write failure', () => {
  const corruptStorage = { getItem: () => '{broken' };
  assert.deepEqual(readOfflineQueue(corruptStorage, 'queue'), []);

  const storage = new Map();
  const adapter = {
    getItem: key => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
  };
  const queue = [{ verb: 'POST', url: '/api/start', encoding: 'form', body: [] }];
  assert.equal(writeOfflineQueue(adapter, 'queue', queue), true);
  assert.deepEqual(readOfflineQueue(adapter, 'queue'), queue);
  assert.equal(writeOfflineQueue({ setItem() { throw new Error('quota'); } }, 'queue', queue), false);
});

test('queue items require a supported body shape', () => {
  assert.equal(isValidOfflineQueueItem({
    verb: 'POST', url: '/api/start', encoding: 'form', body: [['activity', 'work']], ts: 1,
  }), true);
  assert.equal(isValidOfflineQueueItem({
    verb: 'POST', url: '/api/start', encoding: 'json', body: { activity: 'work' },
  }), true);
  assert.equal(isValidOfflineQueueItem({
    verb: 'POST', url: '/api/start', encoding: 'json', body: ['work'],
  }), false);
  assert.equal(isValidOfflineQueueItem({ verb: 'POST', url: '/api/start', encoding: 'form', body: [['x', 1]] }), false);
});

test('offline requests are restricted to the page origin', () => {
  assert.equal(sameOriginPath('/api/start', 'https://app.example/stats'), '/api/start');
  assert.equal(sameOriginPath('https://app.example/api/start?q=1', 'https://app.example/'), '/api/start?q=1');
  assert.equal(sameOriginPath('https://evil.example/api/start', 'https://app.example/'), null);
});

test('request payload encoding preserves forms and accepts JSON objects only', () => {
  assert.deepEqual(
    encodeOfflineRequestPayload(new URLSearchParams([['tag', 'one'], ['tag', 'two']]), 'application/x-www-form-urlencoded'),
    { body: [['tag', 'one'], ['tag', 'two']], encoding: 'form' },
  );
  assert.deepEqual(
    encodeOfflineRequestPayload('{"enabled":true}', 'application/problem+json'),
    { body: { enabled: true }, encoding: 'json' },
  );
  assert.equal(encodeOfflineRequestPayload('[1,2]', 'application/json'), null);
});
