'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function element() {
  let html = '';
  const el = {
    value: '',
    textContent: '',
    style: {},
    dataset: {},
    children: [],
    options: [{ value: '', textContent: '(all)' }],
    onclick: null,
    insertBefore() {},
    remove() {},
    addEventListener() {},
    appendChild(o) { this.options.push(o); },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    setAttribute() {},
    getAttribute() { return null; },
    focus() {},
  };
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; },
  });
  return el;
}

function response(status, data) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => 'application/json' },
    text: async () => (typeof data === 'string' ? data : JSON.stringify(data)),
    json: async () => (typeof data === 'string' ? JSON.parse(data) : data),
  };
}

const calls = [];
const tasks = [
  { id: 't-pending', project: 'p1', status: 'pending', priority: 1, body: 'pending task' },
  { id: 't-leased', project: 'p1', status: 'leased', worker: 'w-1', lease_expires: 1999999999, priority: 1, body: 'leased task' },
  { id: 't-done', project: 'p1', status: 'done', priority: 1, body: 'done task' },
];

let confirmAnswer = true;
let confirmAsked = 0;
global.confirm = () => {
  confirmAsked++;
  return confirmAnswer;
};

let promptAnswer = 'op-1';
let promptAsked = 0;
let promptDefault = null;
global.prompt = (_msg, dflt) => {
  promptAsked++;
  promptDefault = dflt;
  return promptAnswer;
};

let claimStatus = 200;

const bannerText = element();
const els = {
  'filter-project': element(),
  'filter-status': element(),
  'task-details-content': element(),
  'error-banner': element(),
  'error-banner-text': bannerText,
  'task-table-body': element(),
  'queue-count': element(),
  'stat-pending': element(),
  'stat-leased': element(),
  'stat-done': element(),
  'stat-total': element(),
};

Object.defineProperty(els['error-banner'], 'textContent', {
  get() { return bannerText.textContent; },
  set(v) { bannerText.textContent = v; },
});

const document = {
  getElementById: id => {
    if (!els[id]) els[id] = element();
    return els[id];
  },
  createElement: () => element(),
  querySelector: sel => null,
};

const location = { pathname: '/ui', search: '', hash: '' };
const history = {
  pushState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
  replaceState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
};
const window = { addEventListener() {} };

const fetchStub = async (url, opts = {}) => {
  opts = opts || {}; calls.push({ url, method: opts.method || 'GET', body: opts.body ? JSON.parse(opts.body) : null });
  if (url.startsWith('/projects')) return response(200, ['p1']);
  if (url.startsWith('/stats')) return response(200, { pending: 1, leased: 1, done: 1, total: 3 });
  if (url.endsWith('/done')) return response(204, null);
  if (url.endsWith('/release')) return response(204, null);
  if (url.endsWith('/close')) return response(204, null);
  if (url.endsWith('/claim')) {
    const id = decodeURIComponent(url.slice('/tasks/'.length, -'/claim'.length));
    const t = tasks.find(x => x.id === id);
    if (!t) return response(404, 'task not found');
    if (claimStatus !== 200) return response(claimStatus, 'task is leased by w-9');
    return response(200, Object.assign({}, t, { status: 'leased', worker: promptAnswer }));
  }
  if (url.startsWith('/tasks/')) {
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('?')[0]);
    const t = tasks.find(x => x.id === id);
    if (!t) return response(404, 'task not found');
    if (opts.method === 'DELETE') {
      if (t.status === 'leased') return response(409, 'task is leased');
      return response(204, null);
    }
    return response(200, t);
  }
  if (url.startsWith('/tasks')) return response(200, tasks);
  return response(404, 'not found');
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn {loadTasks, selectTask, deleteTask, completeTask, releaseTask, claimTask,' +
  ' closeTask, finishTaskAction, get selected() { return selectedTaskId; }};'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

(async () => {
  const results = {};

  api.selectTask('t-pending');
  await new Promise(r => setTimeout(r, 10));
  const pendingHTML = els['task-details-content'].innerHTML;
  results.pendingHasDelete = pendingHTML.includes('id="delete-task-btn"');
  results.pendingHasComplete = pendingHTML.includes('id="complete-task-btn"');
  results.pendingHasRelease = pendingHTML.includes('id="release-task-btn"');
  results.pendingHasClaim = pendingHTML.includes('id="claim-task-btn"');
  results.pendingHasClose = pendingHTML.includes('id="close-task-btn"');
  let clipboardText = '';
  Object.defineProperty(global.navigator, 'clipboard', {
    value: { writeText: async (txt) => { clipboardText = txt; } },
    configurable: true,
    writable: true,
  });
  results.pendingHasCopy = pendingHTML.includes('id="copy-id-btn"');
  if (els['copy-id-btn'].onclick) {
    await els['copy-id-btn'].onclick();
    results.copySuccess = (clipboardText === 't-pending' && els['copy-id-btn'].textContent === 'Copied!');
  }
  delete global.navigator.clipboard;
  els['copy-id-btn'].textContent = 'Copy';
  if (els['copy-id-btn'].onclick) {
    await els['copy-id-btn'].onclick();
    results.copyFailure = (els['copy-id-btn'].textContent === 'Copy' && els['error-banner'].textContent.includes('Failed to copy ID'));
  }

  confirmAnswer = false;
  confirmAsked = 0;
  calls.length = 0;
  await els['delete-task-btn'].onclick();
  results.cancelDeleteAsked = confirmAsked === 1;
  results.cancelDeleteCalls = calls.length;

  confirmAnswer = true;
  confirmAsked = 0;
  calls.length = 0;
  await els['delete-task-btn'].onclick();
  results.confirmDeletePending = calls.find(c => c.method === 'DELETE');
  results.pendingDeletedPaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  api.selectTask('t-leased');
  await new Promise(r => setTimeout(r, 10));
  const leasedHTML = els['task-details-content'].innerHTML;
  results.leasedHasDelete = leasedHTML.includes('id="delete-task-btn"');
  results.leasedDeleteDisabled = /id="delete-task-btn"[^>]*disabled/.test(leasedHTML) &&
    /id="delete-task-btn"[^>]*aria-disabled="true"/.test(leasedHTML);
  results.leasedDeleteTitle = /id="delete-task-btn"[^>]*title="[^"]+"/.test(leasedHTML);
  results.leasedHasComplete = leasedHTML.includes('id="complete-task-btn"');
  results.leasedHasRelease = leasedHTML.includes('id="release-task-btn"');
  results.leasedHasClaim = leasedHTML.includes('id="claim-task-btn"');
  results.leasedHasClose = leasedHTML.includes('id="close-task-btn"');

  confirmAnswer = true;
  confirmAsked = 0;
  calls.length = 0;
  await els['delete-task-btn'].onclick();
  results.leasedDeleteAsked = confirmAsked > 0;
  results.leasedDeleteCalls = calls.filter(c => c.method === 'DELETE').length;
  results.leasedDeletePaneKept = !els['task-details-content'].innerHTML.includes('Select a task');

  calls.length = 0;
  await els['release-task-btn'].onclick();
  results.releaseCall = calls.find(c => c.url.includes('/release'));
  results.releasePaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  api.selectTask('t-leased');
  await new Promise(r => setTimeout(r, 10));
  calls.length = 0;
  await els['complete-task-btn'].onclick();
  results.completeCall = calls.find(c => c.url.includes('/done'));
  results.completePaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  api.selectTask('t-pending');
  await new Promise(r => setTimeout(r, 10));
  calls.length = 0;
  promptAnswer = null;
  promptAsked = 0;
  await els['claim-task-btn'].onclick();
  results.cancelClaimAsked = promptAsked === 1;
  results.cancelClaimCalls = calls.length;

  calls.length = 0;
  promptAnswer = '  op-1  ';
  claimStatus = 409;
  els['error-banner'].textContent = '';
  await els['claim-task-btn'].onclick();
  results.claimConflictBanner = els['error-banner'].textContent.includes('task is leased by w-9');
  results.claimConflictPaneKept = !els['task-details-content'].innerHTML.includes('Select a task');

  calls.length = 0;
  claimStatus = 200;
  await els['claim-task-btn'].onclick();
  await new Promise(r => setTimeout(r, 10));
  results.claimCall = calls.find(c => c.url.endsWith('/claim'));
  results.claimPaneKept = els['task-details-content'].innerHTML.includes('id="complete-task-btn"') &&
    !els['task-details-content'].innerHTML.includes('id="claim-task-btn"');
  results.claimRefreshedList = calls.some(c => c.url.startsWith('/tasks?')) && calls.some(c => c.url.startsWith('/stats'));

  api.selectTask('t-pending');
  await new Promise(r => setTimeout(r, 10));
  calls.length = 0;
  promptAnswer = 'op-2';
  promptDefault = null;
  await els['claim-task-btn'].onclick();
  results.claimWorkerRemembered = promptDefault === 'op-1';

  api.selectTask('t-pending');
  await new Promise(r => setTimeout(r, 10));
  calls.length = 0;
  confirmAnswer = false;
  confirmAsked = 0;
  await els['close-task-btn'].onclick();
  results.cancelCloseAsked = confirmAsked === 1;
  results.cancelCloseCalls = calls.length;

  calls.length = 0;
  confirmAnswer = true;
  await els['close-task-btn'].onclick();
  results.closeCall = calls.find(c => c.url.endsWith('/close'));
  results.closePaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  api.selectTask('t-done');
  await new Promise(r => setTimeout(r, 10));
  calls.length = 0;
  confirmAnswer = true;
  await els['delete-task-btn'].onclick();
  results.doneDeleteCall = calls.find(c => c.method === 'DELETE');
  results.doneDeletedPaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  els['error-banner'].textContent = 'error persistence test';
  els['error-banner'].style.display = 'flex';
  await api.loadTasks();
  results.errorBannerSurvivesPoll = els['error-banner'].textContent.includes('error persistence test') && els['error-banner'].style.display !== 'none';

  process.stdout.write(JSON.stringify(results, null, 2));
})();
