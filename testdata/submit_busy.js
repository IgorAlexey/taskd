'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const attrs = { 'aria-busy': 'false' };
const submitBtn = {
  disabled: false,
  setAttribute(k, v) { attrs[k] = String(v); },
  getAttribute(k) { return attrs[k] || null; },
  removeAttribute(k) { delete attrs[k]; },
};

const els = {
  'submit-task-btn': submitBtn,
  'form-project': { value: 'default', focus() {} },
  'form-body': { value: 'test body', focus() {} },
  'form-priority': { value: '' },
  'form-id': { value: '' },
  'form-error-summary': { hidden: true, setAttribute() {}, removeAttribute() {} },
  'form-success-summary': { hidden: true, setAttribute() {}, removeAttribute() {} },
  'form-error-summary-list': { replaceChildren() {} },
  'form-success-summary-body': { textContent: '' },
  'connection-status': { hidden: true, textContent: '' },
};

const document = {
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

let fetchCount = 0;
let resolveFetch;
const fetchPromise = new Promise(r => { resolveFetch = r; });

const fetchStub = async (url, opts) => {
  if (url === '/tasks' && opts && opts.method === 'POST') {
    fetchCount++;
    await fetchPromise;
    return {
      ok: true,
      status: 200,
      headers: { get: () => 'application/json' },
      json: async () => ({ id: 'new-id' }),
    };
  }
  return { ok: true, status: 200, json: async () => ({}) };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nloadAll = async () => {};\nreturn { submitTask };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  const p1 = api.submitTask();
  const inFlightDisabled = submitBtn.disabled;
  const inFlightBusy = submitBtn.getAttribute('aria-busy');

  const p2 = api.submitTask();

  resolveFetch();
  await Promise.all([p1, p2]);

  const settledDisabled = submitBtn.disabled;
  const settledBusy = submitBtn.getAttribute('aria-busy');

  process.stdout.write(JSON.stringify({
    inFlightDisabled,
    inFlightBusy,
    settledDisabled,
    settledBusy,
    fetchCount,
  }));
})();
