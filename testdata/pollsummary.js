'use strict';
const fs = require('fs');
const { createElement, response } = require('./dom.js');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

const tasks = [
  {
    id: 'aaaaaaaabbbbbbbbccccccccdddddddd',
    project: 'demo',
    status: 'pending',
    priority: 3,
    claim_count: 0,
    worker: '',
    asset_path: '',
    body: 'web: keep the Summary column populated\n\nWhy: it blanks.',
    summary: 'web: keep the Summary column populated',
  },
];

function project(task, fields) {
  if (!fields) return { ...task };
  const out = {};
  for (const f of fields) {
    if (f in task) out[f] = task[f];
  }
  return out;
}

const listFetches = [];
const fetchStub = async url => {
  if (url.startsWith('/projects')) return response(200, ['demo']);
  if (url.startsWith('/stats')) return response(200, { pending: 1, leased: 0, done: 0, total: 1 });
  if (url.startsWith('/tasks')) {
    listFetches.push(url);
    const u = new URL('http://localhost' + url);
    const raw = u.searchParams.get('fields');
    const fields = raw ? raw.split(',').map(s => s.trim()) : null;
    const rows = tasks.map(t => project(t, fields));
    return response(200, rows, { 'X-Total-Count': String(rows.length) });
  }
  return response(404, 'not found');
};

const els = {
  'filter-project': createElement('select'),
  'filter-status': createElement('select'),
  'task-details-content': createElement('div'),
  'error-banner': createElement('div'),
  'task-table-body': createElement('tbody'),
  'queue-count': createElement('span'),
  'prev-page-btn': createElement('button'),
  'next-page-btn': createElement('button'),
};

const document = {
  getElementById: id => els[id] || null,
  createElement: tag => createElement(tag),
};
const location = { pathname: '/ui', search: '', hash: '' };
const history = { pushState() {}, replaceState() {} };
const window = { addEventListener() {} };

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn { loadTasks, boot };'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

function summaries() {
  const out = [];
  for (const tr of els['task-table-body'].querySelectorAll('tr[data-task-id]')) {
    const cell = tr.querySelector('[data-field="summary"]');
    out.push(cell ? cell.textContent : null);
  }
  return out;
}

function statuses() {
  const out = [];
  for (const tr of els['task-table-body'].querySelectorAll('tr[data-task-id]')) {
    const cell = tr.querySelector('[data-field="status"]');
    out.push(cell ? cell.textContent : null);
  }
  return out;
}

const drain = () => new Promise(r => setImmediate(r));

(async () => {
  const out = {};
  await api.boot();
  await drain();
  out.firstLoad = summaries();
  out.firstStatuses = statuses();
  out.firstURL = listFetches[0];

  await api.loadTasks();
  out.afterPoll = summaries();
  out.pollURL = listFetches[listFetches.length - 1];

  tasks[0].status = 'leased';
  tasks[0].worker = 'w-1';
  tasks.push({
    id: 'eeeeeeeeffffffff00000000111111111',
    project: 'demo',
    status: 'pending',
    priority: 5,
    claim_count: 0,
    worker: '',
    asset_path: '',
    body: 'created while the page was open\nsecond line',
    summary: 'created while the page was open',
  });
  await api.loadTasks();
  await drain();
  out.afterCreatePoll = summaries();
  out.afterCreateStatuses = statuses();

  process.stdout.write(JSON.stringify(out, null, 2));
})();
