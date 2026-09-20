'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function boot(search) {
  const prioEl = { value: '', focus() {}, addEventListener() {} };
  const countEl = { textContent: '' };
  const els = {
    'filter-priority': prioEl,
    'queue-count': countEl,
  };
  const location = { pathname: '/ui', search, hash: '' };
  const history = {
    replaceState(s, t, u) {
      const i = u.indexOf('?');
      location.search = i < 0 ? '' : u.slice(i);
    },
    pushState(s, t, u) { this.replaceState(s, t, u); },
  };
  const document = {
    getElementById: id => els[id] || null,
    querySelector: () => null,
    querySelectorAll: () => [],
    createElement: () => ({ appendChild() {}, setAttribute() {}, style: {} }),
  };
  const listFetches = [];
  const fetchStub = async url => {
    if (url.startsWith('/tasks')) {
      listFetches.push(url);
      const u = new URL(url, 'http://localhost');
      const p = u.searchParams.get('priority');
      const tasks = p === '6' ? [{ id: 't6', priority: 6, project: 'p1' }] : [];
      return {
        ok: true,
        status: 200,
        headers: { get: name => name.toLowerCase() === 'x-total-count' ? String(tasks.length) : 'application/json' },
        text: async () => JSON.stringify(tasks),
        json: async () => tasks,
      };
    }
    return { ok: true, status: 200, headers: { get: () => 'application/json' }, json: async () => [] };
  };

  const api = new Function(
    'document', 'location', 'history', 'window', 'fetch', 'console', 'setInterval', 'clearInterval',
    script + '\nreturn { onFilterChange, readURLState, applyURLState, currentURLState, loadTasks };'
  )(document, location, history, { addEventListener() {} }, fetchStub, console, () => 0, () => {});

  return { api, els, location, listFetches };
}

(async () => {
  const out = {};

  let w = boot('?priority=6');
  w.api.applyURLState();
  await w.api.loadTasks();
  out.priority = {
    search: w.location.search,
    controlValue: w.els['filter-priority'].value,
    queueCount: w.els['queue-count'].textContent,
    list: w.listFetches[w.listFetches.length - 1],
  };

  w = boot('?priority=-1');
  w.api.applyURLState();
  await w.api.loadTasks();
  out.priorityNegative = {
    search: w.location.search,
    controlValue: w.els['filter-priority'].value,
  };

  w = boot('');
  w.els['filter-priority'].value = '6';
  w.api.onFilterChange();
  await w.api.loadTasks();
  out.priorityChange = {
    search: w.location.search,
    controlValue: w.els['filter-priority'].value,
    list: w.listFetches[w.listFetches.length - 1],
  };

  w.els['filter-priority'].value = '-1';
  w.api.onFilterChange();
  out.priorityRefuse = {
    search: w.location.search,
    controlValue: w.els['filter-priority'].value,
  };

  process.stdout.write(JSON.stringify(out));
})();
