'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const banner = { hidden: true };
const bannerText = { textContent: '' };
const els = {
  'error-banner': banner,
  'error-banner-text': bannerText,
  'edit-task-project': { value: 'proj' },
  'edit-task-priority': { value: '1' },
  'edit-task-body': { value: 'task body' },
  'save-task-btn': { disabled: false },
  'cancel-task-btn': { disabled: false },
};

const document = {
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

let patchPayload = null;
const fetchStub = async (url, opts) => {
  if (opts && opts.method === 'PATCH') {
    patchPayload = JSON.parse(opts.body);
    return {
      ok: false,
      status: 409,
      statusText: 'Conflict',
      headers: { get: () => 'application/json' },
      text: async () => JSON.stringify({ error: 'version conflict' }),
      json: async () => ({ error: 'version conflict' }),
    };
  }
  return { ok: true, status: 200, json: async () => ({}) };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\ncurrentTask = { id: 1, version: 3, project: "proj", priority: 1, body: "task body" };\nreturn { saveTaskEdit };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  await api.saveTaskEdit();
  process.stdout.write(JSON.stringify({
    sentVersion: patchPayload ? patchPayload.if_version : null,
    bannerHidden: banner.hidden,
    bannerText: bannerText.textContent,
  }));
})();
