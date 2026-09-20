'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function element() {
  let html = '';
  const attrs = {};
  const children = [];
  const el = {
    value: '',
    textContent: '',
    style: {},
    dataset: {},
    children: children,
    options: [{ value: '', textContent: '(all)' }],
    onclick: null,
    insertBefore() {},
    remove() {},
    addEventListener() {},
    appendChild(child) {
      children.push(child);
      this.options.push(child);
      if (child.textContent) {
        this.textContent = (this.textContent ? this.textContent + ' ' : '') + child.textContent;
      }
    },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    setAttribute(k, v) { attrs[k] = String(v); },
    getAttribute(k) { return attrs[k]; },
    hasAttribute(k) { return k in attrs; },
    focus() {},
  };
  Object.defineProperty(el, 'innerHTML', {
    get() {
      if (html) return html;
      if (children.length > 0) {
        return children.map(c => c.innerHTML || c.textContent || '').join('');
      }
      return '';
    },
    set(v) {
      html = v;
      children.length = 0;
      if (!v) el.textContent = '';
    },
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
  { id: 't-buried', project: 'p1', status: 'buried', priority: 1, body: 'buried task' },
];

const rows = tasks.map(t => {
  const r = element();
  r.dataset = { taskId: t.id };
  return r;
});

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
let failDoneStatus = null;
let failDoneMessage = null;
let failReleaseStatus = null;
let failReleaseMessage = null;
let failDeleteStatus = null;
let failDeleteMessage = null;
let failListStatus = null;
let failStatsStatus = null;
let failProjectsStatus = null;

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
  'stat-buried': element(),
  'stat-total': element(),
  'form-project': element(),
  'form-priority': element(),
  'form-body': element(),
  'form-asset': element(),
  'form-id': element(),
  'form-id-error': element(),
  'form-project-error': element(),
  'form-priority-error': element(),
  'form-body-error': element(),
  'form-asset-error': element(),
  'form-error-summary': element(),
  'form-error-summary-list': element(),
  'submit-task-form': element(),
  'submit-task-btn': element(),
};

els['task-table-body'].querySelectorAll = sel => sel.includes('data-task-id') ? rows : [];

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
  if (url.startsWith('/projects')) {
    if (failProjectsStatus) return response(failProjectsStatus, { error: 'projects query failed' });
    return response(200, ['p1']);
  }
  if (url.startsWith('/stats')) {
    if (failStatsStatus) return response(failStatsStatus, { error: 'stats query failed' });
    return response(200, { pending: 1, leased: 1, done: 1, total: 3 });
  }
  if (url.endsWith('/done')) {
    if (failDoneStatus) return response(failDoneStatus, { error: failDoneMessage });
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('/done')[0]);
    const t = tasks.find(x => x.id === id);
    if (t) {
      t.status = 'done';
      delete t.worker;
      delete t.lease_expires;
    }
    return response(204, null);
  }
  if (url.endsWith('/release')) {
    if (failReleaseStatus) return response(failReleaseStatus, { error: failReleaseMessage });
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('/release')[0]);
    const t = tasks.find(x => x.id === id);
    if (t) {
      t.status = 'pending';
      delete t.worker;
      delete t.lease_expires;
    }
    return response(204, null);
  }
  if (url.endsWith('/kick')) {
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('/kick')[0]);
    const t = tasks.find(x => x.id === id);
    if (!t) return response(404, 'task not found');
    if (t.status !== 'buried') return response(409, 'task is ' + t.status);
    t.status = 'pending';
    delete t.worker;
    delete t.lease_expires;
    return response(204, null);
  }
  if (url.endsWith('/close')) {
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('/close')[0]);
    const t = tasks.find(x => x.id === id);
    if (t) {
      t.status = 'done';
      delete t.worker;
      delete t.lease_expires;
    }
    return response(204, null);
  }
  if (url.endsWith('/touch')) {
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('/touch')[0]);
    const t = tasks.find(x => x.id === id);
    if (!t) return response(404, 'task not found');
    if (t.status !== 'leased') return response(409, 'task is ' + t.status);
    const body = opts.body ? JSON.parse(opts.body) : {};
    if (!body.worker) return response(400, 'missing worker');
    if (body.worker !== t.worker) return response(409, 'worker mismatch');
    const newExpires = 2000000000;
    t.lease_expires = newExpires;
    return response(200, { id: t.id, lease_expires: newExpires, status: t.status });
  }
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
      if (failDeleteStatus) return response(failDeleteStatus, { error: failDeleteMessage });
      if (t.status === 'leased') return response(409, { error: 'task is leased' });
      return response(204, null);
    }
    return response(200, t);
  }
  if (url === '/tasks' && opts.method === 'POST') {
    const b = opts.body ? JSON.parse(opts.body) : {};
    if (b.id === 'dup-id') return response(409, { error: 'duplicate id' });
    return response(201, { id: b.id || 'new-id' });
  }
  if (url.startsWith('/tasks')) {
    if (failListStatus) return response(failListStatus, { error: 'task list query failed' });
    return response(200, tasks);
  }
  return response(404, 'not found');
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn {loadTasks, selectTask, deleteTask, completeTask, touchTask, releaseTask, claimTask, kickTask,' +
  ' closeTask, clearSelectedTask, submitTask, loadStats, loadProjects, finishTaskAction: clearSelectedTask, get selected() { return selectedTaskId; }};'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

(async () => {
  const results = {};

  await api.selectTask('t-pending');
  const pendingHTML = els['task-details-content'].innerHTML;
  results.pendingHasDelete = pendingHTML.includes('id="delete-task-btn"');
  results.pendingHasComplete = pendingHTML.includes('id="complete-task-btn"');
  results.pendingHasRelease = pendingHTML.includes('id="release-task-btn"');
  results.pendingHasClaim = pendingHTML.includes('id="claim-task-btn"');
  results.pendingHasClose = pendingHTML.includes('id="close-task-btn"');
  results.pendingHasTouch = pendingHTML.includes('id="touch-task-btn"');
  results.pendingHasKick = pendingHTML.includes('id="kick-task-btn"');
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
  results.pendingHasCopyBody = pendingHTML.includes('id="copy-body-btn"');
  Object.defineProperty(global.navigator, 'clipboard', {
    value: { writeText: async (txt) => { clipboardText = txt; } },
    configurable: true,
    writable: true,
  });
  if (els['copy-body-btn'] && els['copy-body-btn'].onclick) {
    await els['copy-body-btn'].onclick();
    results.copyBodySuccess = (clipboardText === 'pending task' && els['copy-body-btn'].textContent === 'Copied!');
  }
  delete global.navigator.clipboard;
  if (els['copy-body-btn']) els['copy-body-btn'].textContent = 'Copy';
  if (els['copy-body-btn'] && els['copy-body-btn'].onclick) {
    await els['copy-body-btn'].onclick();
    results.copyBodyFailure = (els['copy-body-btn'].textContent === 'Copy' && els['error-banner'].textContent.includes('Failed to copy body'));
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

  await api.selectTask('t-leased');
  const leasedHTML = els['task-details-content'].innerHTML;
  results.leasedHasDelete = leasedHTML.includes('id="delete-task-btn"');
  results.leasedDeleteDisabled = /id="delete-task-btn"[^>]*disabled/.test(leasedHTML) &&
    /id="delete-task-btn"[^>]*aria-disabled="true"/.test(leasedHTML);
  results.leasedDeleteTitle = /id="delete-task-btn"[^>]*title="[^"]+"/.test(leasedHTML);
  results.leasedHasComplete = leasedHTML.includes('id="complete-task-btn"');
  results.leasedHasTouch = leasedHTML.includes('id="touch-task-btn"');
  results.leasedHasRelease = leasedHTML.includes('id="release-task-btn"');
  results.leasedHasClaim = leasedHTML.includes('id="claim-task-btn"');
  results.leasedHasClose = leasedHTML.includes('id="close-task-btn"');
  results.leasedHasKick = leasedHTML.includes('id="kick-task-btn"');

  confirmAnswer = true;
  confirmAsked = 0;
  calls.length = 0;
  await els['delete-task-btn'].onclick();
  results.leasedDeleteAsked = confirmAsked > 0;
  results.leasedDeleteCalls = calls.filter(c => c.method === 'DELETE').length;
  results.leasedDeletePaneKept = !els['task-details-content'].innerHTML.includes('Select a task');

  calls.length = 0;
  if (els['touch-task-btn'] && els['touch-task-btn'].onclick) {
    await els['touch-task-btn'].onclick();
  }
  results.touchCall = calls.find(c => c.url.includes('/touch'));
  results.touchSelected = (api.selected === 't-leased');
  const touchPaneHTML = els['task-details-content'].innerHTML;
  results.touchPaneHasExpires = touchPaneHTML.includes('Lease Expires');
  results.touchUpdatedExpires = touchPaneHTML.includes(new Date(2000000000 * 1000).toLocaleString());
  results.touchPaneNotReset = !touchPaneHTML.includes('Select a task');

  calls.length = 0;
  await els['release-task-btn'].onclick();
  results.releaseCall = calls.find(c => c.url.includes('/release'));
  results.releaseSelected = (api.selected === 't-leased');
  results.releaseURLPreserved = location.search.includes('task=t-leased');
  const releasePaneHTML = els['task-details-content'].innerHTML;
  results.releasePaneHasBadge = releasePaneHTML.includes('badge badge-pending') && releasePaneHTML.includes('pending');
  results.releasePaneNotReset = !releasePaneHTML.includes('Select a task');
  const leasedRowAfterRelease = rows.find(r => r.dataset.taskId === 't-leased');
  results.releaseRowSelected = !!leasedRowAfterRelease && leasedRowAfterRelease.getAttribute('aria-selected') === 'true';

  const tLeased = tasks.find(x => x.id === 't-leased');
  tLeased.status = 'leased';
  tLeased.worker = 'w-1';
  tLeased.lease_expires = 1999999999;
  await api.selectTask('t-leased');
  calls.length = 0;
  await els['complete-task-btn'].onclick();
  results.completeCall = calls.find(c => c.url.includes('/done'));
  results.completeSelected = (api.selected === 't-leased');
  results.completeURLPreserved = location.search.includes('task=t-leased');
  const completePaneHTML = els['task-details-content'].innerHTML;
  results.completePaneHasBadge = completePaneHTML.includes('badge badge-done') && completePaneHTML.includes('done');
  results.completePaneNotReset = !completePaneHTML.includes('Select a task');
  const leasedRowAfterComplete = rows.find(r => r.dataset.taskId === 't-leased');
  results.completeRowSelected = !!leasedRowAfterComplete && leasedRowAfterComplete.getAttribute('aria-selected') === 'true';

  await api.selectTask('t-pending');
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
  results.claimCall = calls.find(c => c.url.endsWith('/claim'));
  results.claimPaneKept = els['task-details-content'].innerHTML.includes('id="complete-task-btn"') &&
    !els['task-details-content'].innerHTML.includes('id="claim-task-btn"');
  results.claimRefreshedList = calls.some(c => c.url.startsWith('/tasks?')) && calls.some(c => c.url.startsWith('/stats'));

  await api.selectTask('t-pending');
  calls.length = 0;
  promptAnswer = 'op-2';
  promptDefault = null;
  await els['claim-task-btn'].onclick();
  results.claimWorkerRemembered = promptDefault === 'op-1';

  await api.selectTask('t-pending');
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
  results.closePaneKept = els['task-details-content'].innerHTML.includes('badge badge-done') &&
    !els['task-details-content'].innerHTML.includes('Select a task');

  await api.selectTask('t-buried');
  const buriedHTML = els['task-details-content'].innerHTML;
  results.buriedHasKick = buriedHTML.includes('id="kick-task-btn"');
  results.buriedHasDelete = buriedHTML.includes('id="delete-task-btn"');
  results.buriedDeleteDisabled = /id="delete-task-btn"[^>]*disabled/.test(buriedHTML);
  results.buriedHasComplete = buriedHTML.includes('id="complete-task-btn"');
  results.buriedHasTouch = buriedHTML.includes('id="touch-task-btn"');
  results.buriedHasRelease = buriedHTML.includes('id="release-task-btn"');
  results.buriedHasClaim = buriedHTML.includes('id="claim-task-btn"');
  results.buriedHasClose = buriedHTML.includes('id="close-task-btn"');

  calls.length = 0;
  if (els['kick-task-btn'] && els['kick-task-btn'].onclick) {
    await els['kick-task-btn'].onclick();
  }
  results.kickCall = calls.find(c => c.url.endsWith('/kick'));
  results.kickSelected = (api.selected === 't-buried');
  results.kickURLPreserved = location.search.includes('task=t-buried');
  const kickPaneHTML = els['task-details-content'].innerHTML;
  results.kickPaneHasBadge = kickPaneHTML.includes('badge badge-pending') && kickPaneHTML.includes('pending');
  results.kickPaneNotReset = !kickPaneHTML.includes('Select a task');
  const buriedRowAfterKick = rows.find(r => r.dataset.taskId === 't-buried');
  results.kickRowSelected = !!buriedRowAfterKick && buriedRowAfterKick.getAttribute('aria-selected') === 'true';

  await api.selectTask('t-done');
  calls.length = 0;
  confirmAnswer = true;
  await els['delete-task-btn'].onclick();
  results.doneDeleteCall = calls.find(c => c.method === 'DELETE');
  results.doneDeletedPaneReset = els['task-details-content'].innerHTML.includes('Select a task');

  els['error-banner'].textContent = 'error persistence test';
  els['error-banner'].style.display = 'flex';
  await api.loadTasks();
  results.errorBannerSurvivesPoll = els['error-banner'].textContent.includes('error persistence test') && els['error-banner'].style.display !== 'none';

  const resetLeased = () => {
    const t = tasks.find(x => x.id === 't-leased');
    t.status = 'leased';
    t.worker = 'w-1';
    t.lease_expires = 1999999999;
    return t;
  };

  resetLeased();
  failDoneStatus = 409;
  failDoneMessage = 'lease has expired';
  els['error-banner'].textContent = '';
  await api.selectTask('t-leased');
  await els['complete-task-btn'].onclick();
  results.completeExpiredBanner = els['error-banner'].textContent.includes('lease has expired');
  await api.loadTasks();
  results.completeExpiredBannerSurvivesPoll = els['error-banner'].textContent.includes('lease has expired');
  failDoneStatus = null;

  resetLeased();
  failReleaseStatus = 409;
  failReleaseMessage = 'task not leased by worker';
  els['error-banner'].textContent = '';
  await api.selectTask('t-leased');
  await els['release-task-btn'].onclick();
  results.releaseWrongWorkerBanner = els['error-banner'].textContent.includes('task not leased by worker');
  await api.loadTasks();
  results.releaseWrongWorkerBannerSurvivesPoll = els['error-banner'].textContent.includes('task not leased by worker');
  failReleaseStatus = null;

  await api.selectTask('t-pending');
  confirmAnswer = true;
  confirmAsked = 0;
  calls.length = 0;
  failDeleteStatus = 409;
  failDeleteMessage = 'task is leased';
  els['error-banner'].textContent = '';
  await els['delete-task-btn'].onclick();
  results.deleteLeasedBanner = els['error-banner'].textContent.includes('task is leased');
  await api.loadTasks();
  results.deleteLeasedBannerSurvivesPoll = els['error-banner'].textContent.includes('task is leased');
  failDeleteStatus = null;

  resetLeased();
  failDoneStatus = 409;
  failDoneMessage = 'lease has expired';
  els['error-banner'].textContent = '';
  await api.selectTask('t-leased');
  await els['complete-task-btn'].onclick();
  const bannerBeforeSelect = els['error-banner'].textContent.includes('lease has expired');
  await api.selectTask('t-pending');
  results.errorDismissedOnSelect = bannerBeforeSelect && (els['error-banner'].textContent === '' || els['error-banner'].style.display === 'none');
  failDoneStatus = null;

  els['error-banner'].textContent = '';
  els['error-banner'].style.display = 'none';
  els['form-project'].value = 'p1';
  els['form-id'].value = 'dup-id';
  els['form-body'].value = 'test body';
  await api.submitTask({ preventDefault() {} });
  results.submitDuplicateSummary = els['form-error-summary-list'].innerHTML.includes('duplicate id') &&
    els['form-error-summary'].style.display !== 'none';
  results.submitDuplicateFieldError = els['form-id-error'].textContent.includes('duplicate id') &&
    els['form-id-error'].style.display !== 'none';
  results.submitDuplicateAriaInvalid = els['form-id'].getAttribute('aria-invalid') === 'true';
  results.submitDuplicateNoBanner = (els['error-banner'].textContent === '' || !els['error-banner'].textContent) &&
    (els['error-banner'].style.display === 'none' || !els['error-banner'].style.display);

  failListStatus = 500;
  els['error-banner'].textContent = '';
  await api.loadTasks();
  results.listFailureBanner = els['error-banner'].textContent.includes('task list query failed');
  failListStatus = null;

  failStatsStatus = 500;
  els['error-banner'].textContent = '';
  await api.loadStats();
  results.statsFailureBanner = els['error-banner'].textContent.includes('stats query failed');
  failStatsStatus = null;

  failProjectsStatus = 500;
  els['error-banner'].textContent = '';
  await api.loadProjects();
  results.projectsFailureBanner = els['error-banner'].textContent.includes('projects query failed');
  failProjectsStatus = null;

  process.stdout.write(JSON.stringify(results, null, 2));
})();
