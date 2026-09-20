'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];
const radioTags = src.match(/<input[^>]*name="filter-status"[^>]*>/g) || [];
const statusOptions = radioTags.map(tag => {
  const valMatch = tag.match(/value="([^"]*)"/);
  const idMatch = tag.match(/id="([^"]*)"/);
  return {
    value: valMatch ? valMatch[1] : '',
    id: idMatch ? idMatch[1] : '',
  };
});

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
  const statusRadios = [];
  for (let i = 0; i < statusOptions.length; i++) {
    const opt = statusOptions[i];
    let isChecked = (i === 0);
    const r = {
      tagName: 'INPUT',
      type: 'radio',
      name: 'filter-status',
      id: opt.id,
      value: opt.value,
      get checked() { return isChecked; },
      set checked(v) {
        isChecked = Boolean(v);
        if (isChecked) {
          for (const other of statusRadios) {
            if (other !== r) other.uncheck();
          }
        }
      },
      uncheck() { isChecked = false; }
    };
    statusRadios.push(r);
  }

  const els = {
    'filter-project': element([{ value: '', textContent: '(all)' }]),
    'filter-search': element(),
    'form-project': element(),
    'task-details-content': element(),
    'error-banner': element(),
    'task-table-body': element(),
    'queue-count': element(),
    'stat-pending': element(),
    'stat-leased': element(),
    'stat-done': element(),
    'stat-total': element(),
  };
  for (const r of statusRadios) {
    els[r.id] = r;
  }
  const document = {
    getElementById: id => els[id] || null,
    createElement: () => element(),
    querySelector: sel => {
      if (sel === 'input[name="filter-status"]:checked') {
        return statusRadios.find(r => r.checked) || null;
      }
      const m = sel.match(/^input\[name="filter-status"\]\[value="([^"]*)"\]$/);
      if (m) {
        return statusRadios.find(r => r.value === m[1]) || null;
      }
      return null;
    },
    querySelectorAll: sel => {
      if (sel === 'input[name="filter-status"]') {
        return statusRadios;
      }
      return [];
    },
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
    ' onSearchInput, clearSearch, readURLState, applyURLState,' +
    ' currentURLState, get selected() { return selectedTaskId; }};'
  )(document, location, history, window, fetchStub, console, () => 0, () => {});
  return {
    api, els, history, location, listFetches, statsFetches,
    status: () => { const active = statusRadios.find(r => r.checked); return active ? active.value : ''; },
    setStatus: v => { const r = statusRadios.find(x => x.value === v); if (r) r.checked = true; },
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

const settle = (ms = 0) => new Promise(r => setTimeout(r, ms));

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
    formProject: w.els['form-project'] ? w.els['form-project'].value : '',
  };
  w = boot('?status=bogus&project=ghost&task=t1', world);
  await settle();
  out.noise = {
    status: w.status(),
    project: w.els['filter-project'].value,
    options: w.els['filter-project'].options.map(o => o.value),
    url: w.url(),
  };

  w = boot('', world);
  await settle();
  w.setStatus('done');
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
    status: w.status(),
    url: w.url(),
    list: w.listFetches[w.listFetches.length - 1],
  };

  w.api.filterByStatus('');
  await settle();
  out.cardFilterTotal = {
    status: w.status(),
    url: w.url(),
    list: w.listFetches[w.listFetches.length - 1],
  };
  out.projectPrefill = {
    formProject: w.els['form-project'] ? w.els['form-project'].value : '',
  };
  w = boot('', world);
  await settle();
  w.els['filter-search'].value = 'needle';
  w.api.onSearchInput();
  await settle(250);
  out.search = {
    entry: w.history.log.length ? w.history.log[w.history.log.length - 1][0] : '',
    url: w.url(),
    search: w.location.search,
    list: w.listFetches[w.listFetches.length - 1],
  };

  w.api.selectTask('t2');
  await settle();
  out.searchSelect = {
    entry: w.history.log[w.history.log.length - 1][0],
    url: w.url(),
    search: w.location.search,
  };

  await w.back();
  out.searchBack = {
    url: w.url(),
    task: w.api.selected || '',
    search: w.els['filter-search'].value,
  };

  w.api.clearSearch();
  await settle();
  out.searchClear = {
    url: w.url(),
    search: w.els['filter-search'].value,
  };

  w = boot('?q=prefilled', world);
  await settle();
  out.searchRestore = {
    parsedQ: w.api.readURLState().q,
    inputValue: w.els['filter-search'].value,
    url: w.url(),
    list: w.listFetches[0],
  };

  w = boot('?status=live', world);
  await settle();
  const liveBootStatus = w.status();
  const liveBootList = w.listFetches[w.listFetches.length - 1];
  w.api.selectTask('t1');
  await settle();
  out.live = {
    status: liveBootStatus,
    bootList: liveBootList,
    selectedURL: w.url(),
  };

  w = boot('?status=pending', world);
  await settle();
  w.setStatus('live');
  w.api.onFilterChange();
  await settle();
  out.liveChoose = {
    url: w.url(),
    list: w.listFetches[w.listFetches.length - 1],
  };

  process.stdout.write(JSON.stringify(out, null, 1));
})();
