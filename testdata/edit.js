'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function element(onSetHTML) {
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
    querySelector(sel) {
      if (sel && sel.includes('badge')) return element();
      const m = sel ? sel.match(/\[data-field="([^"]+)"\]/) : null;
      if (m && this.dataset && this.dataset[m[1]] !== undefined) {
        const sub = element();
        sub.textContent = this.dataset[m[1]];
        return sub;
      }
      return element();
    },
    querySelectorAll() { return []; },
    setAttribute() {},
    getAttribute() { return null; },
    focus() {},
  };
  Object.defineProperty(el, 'innerHTML', {
    configurable: true,
    get() { return html; },
    set(v) {
      html = v;
      if (onSetHTML) onSetHTML(v);
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
  { id: 't-pending', project: 'orig-proj', status: 'pending', priority: 2, body: 'orig body', asset_path: 'orig/asset.glb' },
];

const bannerText = element();
const els = {
  'filter-project': element(),
  'filter-status': element(),
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
  configurable: true,
  get() { return bannerText.textContent; },
  set(v) { bannerText.textContent = v; },
});

const pane = element(v => {
  const inputRe = /<input[^>]*id="([^"]+)"[^>]*>/g;
  let m;
  while ((m = inputRe.exec(v)) !== null) {
    const id = m[1];
    const valMatch = m[0].match(/value="([^"]*)"/);
    const val = valMatch ? valMatch[1] : '';
    const el = document.getElementById(id);
    el.value = val;
  }
  const taRe = /<textarea[^>]*id="([^"]+)"[^>]*>([\s\S]*?)<\/textarea>/g;
  while ((m = taRe.exec(v)) !== null) {
    const id = m[1];
    const el = document.getElementById(id);
    el.value = m[2];
  }
});
els['task-details-content'] = pane;

const document = {
  getElementById: id => {
    if (!els[id]) els[id] = element();
    return els[id];
  },
  createElement: () => element(),
  querySelector: () => null,
};

const location = { pathname: '/ui', search: '', hash: '' };
const history = {
  pushState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
  replaceState(_s, _t, url) { location.search = url.includes('?') ? url.slice(url.indexOf('?')) : ''; },
};
const window = { addEventListener() {} };

const fetchStub = async (url, opts = {}) => {
  opts = opts || {};
  const method = opts.method || 'GET';
  calls.push({ url, method, body: opts.body ? JSON.parse(opts.body) : null });
  if (url.startsWith('/projects')) return response(200, ['orig-proj', 'new-proj']);
  if (url.startsWith('/stats')) return response(200, { pending: 1, leased: 0, done: 0, total: 1 });
  if (url.startsWith('/tasks/')) {
    const raw = url.slice('/tasks/'.length);
    const id = decodeURIComponent(raw.split('?')[0]);
    const t = tasks.find(x => x.id === id);
    if (!t) return response(404, 'task not found');
    if (method === 'PATCH') {
      const patch = JSON.parse(opts.body);
      if (patch.project !== undefined) t.project = patch.project;
      if (patch.asset_path !== undefined) t.asset_path = patch.asset_path;
      if (patch.priority !== undefined) t.priority = patch.priority;
      if (patch.body !== undefined) t.body = patch.body;
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
  script + '\nreturn {loadTasks, selectTask, toggleTaskEdit, cancelTaskEdit, saveTaskEdit,' +
  ' get currentTask() { return currentTask; }, get isEditing() { return isEditingTask; }};'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

(async () => {
  const results = {};

  api.selectTask('t-pending');
  await new Promise(r => setTimeout(r, 10));

  const initialViewHTML = pane.innerHTML;
  results.initialHasProject = initialViewHTML.includes('orig-proj');
  results.initialHasAsset = initialViewHTML.includes('orig/asset.glb');

  api.toggleTaskEdit();
  const editHTML = pane.innerHTML;
  results.editHasProjectInput = editHTML.includes('id="edit-task-project"');
  results.editHasAssetInput = editHTML.includes('id="edit-task-asset-path"');
  results.projectPrefilled = els['edit-task-project'] ? els['edit-task-project'].value : '';
  const assetEl = els['edit-task-asset-path'];
  results.assetPrefilled = assetEl ? assetEl.value : '';

  calls.length = 0;
  if (els['edit-task-project']) els['edit-task-project'].value = 'invalid project name!';
  await api.saveTaskEdit();
  results.invalidProjectRejected = calls.filter(c => c.method === 'PATCH').length === 0;
  results.invalidProjectError = bannerText.textContent.includes('Project must be 1-64 characters');

  calls.length = 0;
  if (els['edit-task-project']) els['edit-task-project'].value = 'new-proj';
  if (assetEl) assetEl.value = '';
  if (els['edit-task-body']) els['edit-task-body'].value = '   ';
  await api.saveTaskEdit();
  results.whitespaceBodyNoAssetRejected = calls.filter(c => c.method === 'PATCH').length === 0;

  calls.length = 0;
  if (els['edit-task-project']) els['edit-task-project'].value = 'new-proj';
  if (assetEl) assetEl.value = 'models/updated.glb';
  if (els['edit-task-priority']) els['edit-task-priority'].value = '5';
  if (els['edit-task-body']) els['edit-task-body'].value = 'updated body';

  await api.saveTaskEdit();
  await new Promise(r => setTimeout(r, 10));

  const patchCall = calls.find(c => c.method === 'PATCH');
  results.patchSent = !!patchCall;
  results.patchPayload = patchCall ? patchCall.body : null;

  results.isEditingAfterSave = api.isEditing;
  const postSaveViewHTML = pane.innerHTML;
  results.detailsHasUpdatedProject = postSaveViewHTML.includes('new-proj');
  results.detailsHasUpdatedAsset = postSaveViewHTML.includes('models/updated.glb');

  const row = api.loadTasks ? tasks[0] : null;
  results.taskUpdated = row && row.project === 'new-proj' && row.asset_path === 'models/updated.glb';
  results.projectsReloaded = calls.some(c => c.url.startsWith('/projects'));

  process.stdout.write(JSON.stringify(results, null, 2));
})();
