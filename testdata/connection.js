'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

const pill = { textContent: '', style: {} };
const banner = { style: {} };
const bannerText = { textContent: '' };
const autoRefresh = { checked: true };

function stub() {
  let html = '';
  const el = {
    value: '',
    textContent: '',
    disabled: false,
    style: {},
    dataset: {},
    options: [],
    appendChild() {},
    insertBefore() {},
    removeChild() {},
    remove() {},
    addEventListener() {},
    setAttribute() {},
    getAttribute() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    focus() {},
  };
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; },
  });
  return el;
}

const els = {
  'connection-status': pill,
  'error-banner': banner,
  'error-banner-text': bannerText,
  'auto-refresh': autoRefresh,
};

const document = {
  querySelector: () => null,
  getElementById(id) {
    if (!(id in els)) els[id] = stub();
    return els[id];
  },
  createElement: () => stub(),
};

let now = 1700000000000;
class FakeDate extends Date {
  constructor(...args) {
    super(...(args.length ? args : [now]));
  }
  static now() { return now; }
}

let ticker = null;
function fakeSetInterval(fn) {
  ticker = fn;
  return 1;
}
function fakeClearInterval() {
  ticker = null;
}
function fakeSetTimeout(fn) { return 0; }
function fakeClearTimeout() {}

let online = false;
let fetches = 0;
let hold = null;
const fetchStub = async url => {
  fetches++;
  if (hold) await hold;
  if (!online) throw new TypeError('Failed to fetch');
  const body = url.startsWith('/stats')
    ? '{"pending":1,"leased":0,"done":0,"total":1}'
    : '[]';
  return {
    ok: true,
    status: 200,
    text: async () => body,
    json: async () => JSON.parse(body),
    headers: { get: k => (k === 'X-Total-Count' ? '0' : 'application/json') },
  };
};

let logs = 0;
const consoleStub = { log() {}, error() { logs++; } };

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date',
  script + '\nreturn { loadAll, setupAutoRefresh, showError, showFetchError, fetchRaw };'
)(
  document,
  { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} },
  { addEventListener() {} },
  fetchStub,
  consoleStub,
  fakeSetTimeout, fakeClearTimeout, fakeSetInterval, fakeClearInterval,
  FakeDate,
);

async function drain() {
  for (let i = 0; i < 500; i++) await Promise.resolve();
}

async function ticksToRefresh(limit) {
  const before = fetches;
  for (let i = 1; i <= limit; i++) {
    if (!ticker) return -1;
    now += 1000;
    ticker();
    await drain();
    if (fetches > before) return i;
  }
  return -1;
}

function snapshot() {
  return {
    text: pill.textContent,
    display: pill.style.display,
    banner: banner.style.display || '',
    ticking: ticker !== null,
  };
}

(async () => {
  const out = {};

  await drain();
  out.boot = snapshot();

  out.backoff = [];
  for (let i = 0; i < 5; i++) out.backoff.push(await ticksToRefresh(40));
  out.countdown = pill.textContent;

  const transport = new Error('Failed to fetch');
  transport.transport = true;
  api.showFetchError(transport);
  out.mutationBanner = banner.style.display || '';
  api.showError('Priority must be a whole number, 0 or greater');
  out.validationBanner = banner.style.display || '';
  out.validationText = bannerText.textContent;
  api.showError(null);
  out.logsWhileOffline = logs;

  autoRefresh.checked = false;
  api.setupAutoRefresh();
  out.autoRefreshOff = snapshot();
  autoRefresh.checked = true;
  api.setupAutoRefresh();

  online = true;
  out.recoverTicks = await ticksToRefresh(40);
  out.recovered = snapshot();
  out.onlineTicks = await ticksToRefresh(40);

  let release;
  hold = new Promise(r => { release = r; });
  const before = fetches;
  await ticksToRefresh(10);
  const started = fetches - before;
  for (let i = 0; i < 5; i++) {
    now += 1000;
    ticker();
    await drain();
  }
  out.overlapFetches = fetches - before - started;
  hold = null;
  release();
  await drain();

  online = false;
  out.relapseTicks = await ticksToRefresh(40);
  out.relapse = snapshot();

  online = true;
  await ticksToRefresh(40);
  online = false;
  await api.loadAll();
  await api.loadAll();
  await api.loadAll();
  await drain();
  out.afterManualRefresh = await ticksToRefresh(40);

  await ticksToRefresh(40);
  online = true;
  await api.fetchRaw('/workers');
  await drain();
  out.afterOutOfBandSuccess = await ticksToRefresh(40);

  process.stdout.write(JSON.stringify(out, null, 2));
  process.exit(0);
})();
