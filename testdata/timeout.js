'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const pill = { textContent: '', hidden: true };
const countEl = { textContent: '' };
const rows = [];
const tbody = {
  rows,
  children: rows,
  querySelectorAll: () => rows,
  querySelector: () => null,
  insertBefore(tr) { if (!rows.includes(tr)) rows.push(tr); },
  addEventListener() {},
};

const els = {
  'connection-status': pill,
  'queue-count': countEl,
  'task-table-body': tbody,
  'auto-refresh': { checked: true },
};

const document = {
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
  createElement: () => ({
    dataset: {},
    style: {},
    appendChild() {},
    remove() { const i = rows.indexOf(this); if (i >= 0) rows.splice(i, 1); },
    setAttribute() {},
    getAttribute: () => null,
    hasAttribute: () => false,
    querySelector: () => null,
  }),
};

let now = 1000;
let tasks = [{ id: '41764ca4' }, { id: '2e7afac5' }, { id: 'be3024c2' }];
let hang = false;
let fetches = 0;
let abortHang = null;

const fetchStub = (url, opts) => {
  fetches++;
  if (hang && url.startsWith('/tasks')) {
    return new Promise((_, reject) => {
      abortHang = () => reject(new DOMException('TimeoutError', 'TimeoutError'));
      if (opts && opts.signal) opts.signal.addEventListener('abort', abortHang);
    });
  }
  return Promise.resolve({
    ok: true, status: 200,
    headers: { get: () => 'application/json' },
    json: async () => tasks,
    text: async () => JSON.stringify(tasks),
  });
};

let activeSignal = null;
const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { loadAll, refreshTick };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  class extends Date { static now() { return now; } },
  {
    timeout(ms) {
      const c = new AbortController();
      activeSignal = c;
      return c.signal;
    }
  },
);

(async () => {
  await api.loadAll();

  const getIds = () => rows.map(r => r.dataset.taskId);
  const out = { bootRows: getIds(), bootCount: countEl.textContent };

  hang = true;
  tasks = [{ id: '41764ca4' }, { id: 'be3024c2' }, { id: '3642f64f' }];

  const pending = api.loadAll();
  const hangingFetches = fetches;

  api.refreshTick();
  out.windowRequests = fetches - hangingFetches;
  out.staleRows = getIds();

  if (activeSignal) activeSignal.abort();
  else if (abortHang) abortHang();
  await pending;

  out.offlineStatus = pill.textContent;
  out.offlineHidden = pill.hidden;

  hang = false;
  now += 5000;
  const beforeRetry = fetches;
  await api.refreshTick();

  out.retryRequests = fetches - beforeRetry;
  out.recoveredRows = getIds();
  out.recoveredHidden = pill.hidden;

  process.stdout.write(JSON.stringify(out, null, 2));
})();
