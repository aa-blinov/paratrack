import assert from 'node:assert/strict';
import test from 'node:test';

test('shared messages follow a language change in the same document', async () => {
  const previous = globalThis.document;
  const dataset = { i18nUndo: 'Отменить', i18nSendError: 'Ошибка отправки', i18nOffline: 'Нет сети' };
  globalThis.document = { body: { dataset } };
  try {
    const { paratrackT } = await import('../../internal/web/static/js/app-config.js');
    assert.equal(paratrackT.undo, 'Отменить');
    Object.assign(dataset, { i18nUndo: 'Undo', i18nSendError: 'Send failed', i18nOffline: 'Offline' });
    assert.equal(paratrackT.undo, 'Undo');
    assert.equal(paratrackT.sendError, 'Send failed');
    assert.equal(paratrackT.offline, 'Offline');
  } finally {
    globalThis.document = previous;
  }
});
