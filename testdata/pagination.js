'use strict';
const fs = require('fs');
const { createElement, response } = require('./dom.js');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

const tasks = [];
for (let i = 0; i < 250; i++) {
  tasks.push({
    id: 'task-' + String(i).padStart(3, '0'),
    project: 'test',
    status: 'pending',
    priority: 1,
    body: 'task body ' + i,
  });
}

function boot(initialSearch = '') {
  const listFetches = [];
  const fetchStub = async (url, opts = {}) => {
    if (url.startsWith('/projects')) return response(200, ['test']);
    if (url.startsWith('/stats')) return response(200, { pending: 250, leased: 0, done: 0, total: 250 });
    if (url.startsWith('/tasks')) {
      listFetches.push(url);
      const u = new URL('http://localhost' + url);
      const limit = parseInt(u.searchParams.get('limit') || '200', 10);
      const offset = parseInt(u.searchParams.get('offset') || '0', 10);
      const sliced = tasks.slice(offset, offset + limit);
      return response(200, sliced, { 'X-Total-Count': String(tasks.length) });
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
    'stat-pending': createElement('div'),
    'stat-leased': createElement('div'),
    'stat-done': createElement('div'),
    'stat-total': createElement('div'),
  };

  const document = {
    getElementById: id => els[id] || null,
    createElement: tag => createElement(tag),
  };

  const location = { pathname: '/ui', search: initialSearch, hash: '' };
  const history = {
    pushState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
    replaceState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
  };
  let onpopstate = null;
  const window = {
    addEventListener(name, fn) { if (name === 'popstate') onpopstate = fn; },
  };

  const api = new Function(
    'document', 'location', 'history', 'window', 'fetch', 'console',
    'setInterval', 'clearInterval',
    script + '\nreturn { loadTasks, prevPage, nextPage, applyURLState, currentURLState };'
  )(document, location, history, window, fetchStub, console, () => 0, () => {});

  return { api, els, location, listFetches };
}

(async () => {
  const out = {};
  const app = boot('');
  app.api.applyURLState();
  await app.api.loadTasks();

  out.initial = {
    countText: app.els['queue-count'].textContent,
    prevDisabled: app.els['prev-page-btn'].disabled,
    nextDisabled: app.els['next-page-btn'].disabled,
    rowCount: app.els['task-table-body'].children.length,
    firstTaskId: app.els['task-table-body'].children[0] ? app.els['task-table-body'].children[0].dataset.taskId : null,
    fetchURL: app.listFetches[app.listFetches.length - 1],
    url: app.location.pathname + app.location.search,
  };

  await app.api.nextPage();
  out.page2 = {
    countText: app.els['queue-count'].textContent,
    prevDisabled: app.els['prev-page-btn'].disabled,
    nextDisabled: app.els['next-page-btn'].disabled,
    rowCount: app.els['task-table-body'].children.length,
    firstTaskId: app.els['task-table-body'].children[0] ? app.els['task-table-body'].children[0].dataset.taskId : null,
    fetchURL: app.listFetches[app.listFetches.length - 1],
    url: app.location.pathname + app.location.search,
  };

  await app.api.prevPage();
  out.backToPage1 = {
    countText: app.els['queue-count'].textContent,
    prevDisabled: app.els['prev-page-btn'].disabled,
    nextDisabled: app.els['next-page-btn'].disabled,
    rowCount: app.els['task-table-body'].children.length,
    firstTaskId: app.els['task-table-body'].children[0] ? app.els['task-table-body'].children[0].dataset.taskId : null,
    fetchURL: app.listFetches[app.listFetches.length - 1],
    url: app.location.pathname + app.location.search,
  };

  const appPage2 = boot('?page=2');
  appPage2.api.applyURLState();
  await appPage2.api.loadTasks();
  out.directPage2 = {
    countText: appPage2.els['queue-count'].textContent,
    prevDisabled: appPage2.els['prev-page-btn'].disabled,
    nextDisabled: appPage2.els['next-page-btn'].disabled,
    rowCount: appPage2.els['task-table-body'].children.length,
    firstTaskId: appPage2.els['task-table-body'].children[0] ? appPage2.els['task-table-body'].children[0].dataset.taskId : null,
    fetchURL: appPage2.listFetches[appPage2.listFetches.length - 1],
    url: appPage2.location.pathname + appPage2.location.search,
  };

  process.stdout.write(JSON.stringify(out, null, 2));
})();
