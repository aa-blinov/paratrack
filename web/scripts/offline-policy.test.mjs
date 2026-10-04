import assert from 'node:assert/strict';
import test from 'node:test';

import { isOfflineQueueableMutation } from '../../internal/web/static/js/app-offline-policy.js';

test('allows supported timer, schedule, and ledger mutations', () => {
  for (const [method, path] of [
    ['POST', '/api/start'],
    ['POST', '/api/sessions/42/stop'],
    ['POST', '/api/timesheet/cell'],
    ['PATCH', '/api/sessions/42'],
    ['DELETE', '/api/sessions/42/tags'],
  ]) {
    assert.equal(isOfflineQueueableMutation(method, path), true, `${method} ${path}`);
  }
});

test('keeps credential and payment mutations online-only', () => {
  for (const [method, path] of [
    ['POST', '/api/login'],
    ['POST', '/api/profile/password'],
    ['POST', '/api/integrations'],
    ['POST', '/api/team/stripe'],
    ['POST', '/api/push/subscribe'],
    ['POST', '/api/invoices/12/paid'],
  ]) {
    assert.equal(isOfflineQueueableMutation(method, path), false, `${method} ${path}`);
  }
});

test('rejects non-mutating methods and malformed session paths', () => {
  assert.equal(isOfflineQueueableMutation('GET', '/api/start'), false);
  assert.equal(isOfflineQueueableMutation('POST', '/api/sessions/not-an-id/stop'), false);
  assert.equal(isOfflineQueueableMutation('PUT', '/api/start'), false);
});
