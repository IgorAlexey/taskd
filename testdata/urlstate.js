'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];
const statusOptions = [...src
  .match(/<select[^>]*id="filter-status"[\s\S]*?<\/select>/)[0]
  .matchAll(/value="([^"]*)"/g)].map(m => m[1]);

function element(options) {
  const el = {
    value: '',
    textContent: '',
    style: {},
    dataset: {},
    children: [],
    options: options || [],
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
  let html = '';
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) {
      html = v;
      if (this.options.length) this.options = [{ value: '', textContent: '(all)' }];
    },
  });
  return el;
}

function response(status, data) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: () => 'application/json' },
    text: async () => JSON.stringify(data),
    json: async () => data,
  };
}

function boot(search, world) {
  const els = {
    'filter-project': element([{ value: '', textContent: '(all)' }]),
    'filter-status': element(statusOptions.map(v => ({ value: v }))),
    'task-details-content': element(),
    'error-banner': element(),
    'task-table-body': element(),
    'queue-count': element(),
    'stat-pending': element(),
    'stat-leased': element(),
    'stat-done': element(),
    'stat-total': element(),
  };
  const document = {
    getElementById: id => els[id] || null,
    createElement: () => element(),
  };
  const location = { pathname: '/ui', search, hash: '' };
  const entries = [location.pathname + search];
  const history = {
    log: [],
    pushState(s, _t, url) { this.log.push(['push', url]); entries.push(url); load(url); },
    replaceState(s, _t, url) { this.log.push(['replace', url]); entries[entries.length - 1] = url; load(url); },
  };
  function load(url) {
    const i = url.indexOf('?');
    location.search = i < 0 ? '' : url.slice(i);
  }
  let onpopstate = null;
  const window = {
    addEventListener(name, fn) { if (name === 'popstate') onpopstate = fn; },
  };
  const listFetches = [];
  const statsFetches = [];
  const fetchStub = async url => {
    if (url.startsWith('/projects')) {
      return world.projectsFail
        ? response(500, 'projects unavailable')
        : response(200, world.projects);
    }
    if (url.startsWith('/stats')) {
      statsFetches.push(url);
      const q = url.includes('?') ? new URLSearchParams(url.slice(url.indexOf('?'))) : null;
      const proj = q ? q.get('project') : '';
      const stats = (proj && world.projectStats && world.projectStats[proj]) ||
        world.stats || { pending: 0, leased: 0, done: 0, total: 0 };
      return response(200, stats);
    }
    if (url.startsWith('/tasks/')) {
      const id = decodeURIComponent(url.slice('/tasks/'.length));
      const t = world.tasks.find(x => x.id === id);
      return t ? response(200, t) : response(404, 'task not found');
    }
    if (url.startsWith('/tasks')) {
      listFetches.push(url);
      return response(200, world.page);
    }
    return response(404, 'no route');
  };
  const api = new Function(
    'document', 'location', 'history', 'window', 'fetch', 'console',
    'setInterval', 'clearInterval',
    script + '\nreturn {loadTasks, loadProjects, selectTask, onFilterChange, filterByStatus,' +
    ' currentURLState, get selected() { return selectedTaskId; }};'
  )(document, location, history, window, fetchStub, console, () => 0, () => {});
  return {
    api, els, history, location, listFetches, statsFetches,
    url: () => location.pathname + location.search,
    pane: () => {
      const h = els['task-details-content'].innerHTML;
      if (h.includes('Select a task')) return 'idle';
      if (h.includes('Task not found')) return 'notfound';
      return h.includes('field-label') ? 'details' : 'empty';
    },
    back: async () => {
      if (entries.length > 1) entries.pop();
      load(entries[entries.length - 1]);
      onpopstate();
      await settle();
    },
  };
}

const settle = () => new Promise(r => setTimeout(r, 0));

const t1 = { id: 't1', project: 'p1', status: 'pending', priority: 1, body: 'one' };
const t2 = { id: 't2', project: 'p1', status: 'done', priority: 1, body: 'two' };
const world = { projects: ['p1'], tasks: [t1, t2], page: [t1, t2] };

(async () => {
  const out = {};

  let w = boot('?project=p1&status=pending&task=t1', world);
  await settle();
  out.load = {
    state: w.api.currentURLState(),
    url: w.url(),
    pane: w.pane(),
    list: w.listFetches[0],
  };
  w = boot('?status=bogus&project=ghost&task=t1', world);
  await settle();
  out.noise = {
    status: w.els['filter-status'].value,
    project: w.els['filter-project'].value,
    options: w.els['filter-project'].options.map(o => o.value),
    url: w.url(),
  };

  w = boot('', world);
  await settle();
  w.els['filter-status'].value = 'done';
  w.api.onFilterChange();
  await settle();
  out.filter = { entry: w.history.log[w.history.log.length - 1][0], url: w.url() };

  w.api.selectTask('t2');
  await settle();
  out.select = { entry: w.history.log[w.history.log.length - 1][0], url: w.url(), pane: w.pane() };
  w.api.selectTask('t2');
  await settle();
  out.reselect = w.history.log[w.history.log.length - 1][0];

  await w.back();
  out.back = { url: w.url(), task: w.api.selected, pane: w.pane() };
  w = boot('?status=pending&task=t2', { ...world, page: [t1] });
  await settle();
  await w.api.loadTasks();
  await settle();
  out.offPage = { url: w.url(), task: w.api.selected, pane: w.pane() };
  w = boot('?task=gone', world);
  await settle();
  await w.api.loadTasks();
  await settle();
  out.missing = { url: w.url(), task: w.api.selected, pane: w.pane() };

  w = boot('?project=ghost&status=pending&task=t1', { ...world, projectsFail: true });
  await settle();
  w.api.selectTask('t2');
  await settle();
  out.projectsDown = {
    project: w.els['filter-project'].value,
    url: w.url(),
    list: w.listFetches[0],
  };


  w = boot('', {
    ...world,
    projectStats: {
      p1: { pending: 4, leased: 2, done: 1, total: 7 },
    },
  });
  await settle();
  w.els['filter-project'].value = 'p1';
  w.api.onFilterChange();
  await settle();
  out.projectFilterStats = {
    url: w.statsFetches[w.statsFetches.length - 1],
    pending: w.els['stat-pending'].textContent,
    leased: w.els['stat-leased'].textContent,
    done: w.els['stat-done'].textContent,
    total: w.els['stat-total'].textContent,
  };
  w.api.filterByStatus('pending');
  await settle();
  out.cardFilterStatus = {
    status: w.els['filter-status'].value,
    url: w.url(),
    list: w.listFetches[w.listFetches.length - 1],
  };

  w.api.filterByStatus('');
  await settle();
  out.cardFilterTotal = {
    status: w.els['filter-status'].value,
    url: w.url(),
    list: w.listFetches[w.listFetches.length - 1],
  };
  process.stdout.write(JSON.stringify(out, null, 1));
})();
