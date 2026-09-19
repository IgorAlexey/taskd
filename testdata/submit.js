'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];
const corpus = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
const bodyCorpus = JSON.parse(fs.readFileSync(process.argv[4], 'utf8'));

function element() {
  let html = '';
  const el = {
    value: '',
    textContent: '',
    checked: false,
    style: {},
    dataset: {},
    attrs: {},
    options: [],
    onclick: null,
    insertBefore() {},
    remove() {},
    addEventListener() {},
    appendChild(o) { this.options.push(o); },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    setAttribute(k, v) { this.attrs[k] = v; },
    getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; },
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

const els = {};
const document = {
  getElementById: id => {
    if (!els[id]) els[id] = element();
    return els[id];
  },
  createElement: () => element(),
  querySelector: () => null,
};

const fields = ['form-project', 'form-priority', 'form-body', 'form-asset', 'form-id'];
for (const id of fields) {
  document.getElementById(id);
  document.getElementById(id + '-error');
}
const form = document.getElementById('submit-task-form');
form.querySelectorAll = sel => {
  if (sel === '[aria-invalid]') return fields.map(id => els[id]);
  if (sel === '.field-error') return fields.map(id => els[id + '-error']);
  return [];
};

const posted = [];
const fetchStub = async (url, opts = {}) => {
  opts = opts || {};
  const method = opts.method || 'GET';
  if (method === 'POST') posted.push({ url, body: JSON.parse(opts.body) });
  if (url.startsWith('/projects')) return response(200, ['p1']);
  if (url.startsWith('/workers')) return response(200, ['w1']);
  if (url.startsWith('/stats')) return response(200, { pending: 0, leased: 0, done: 0, total: 0 });
  if (url === '/tasks' && method === 'POST') return response(201, { id: 'new-id' });
  if (url.startsWith('/tasks')) return response(200, []);
  return response(404, 'not found');
};

const location = { pathname: '/ui', search: '', hash: '' };
const history = { pushState() {}, replaceState() {} };
const window = { addEventListener() {} };

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn {submitTask, projectError, customIdError, bodyError};'
)(document, location, history, window, fetchStub, console, () => 0, () => {});

async function attempt(project, customId, body, asset) {
  els['form-project'].value = project;
  els['form-id'].value = customId;
  els['form-body'].value = body;
  els['form-asset'].value = asset;
  els['form-priority'].value = '';
  posted.length = 0;
  await api.submitTask({ preventDefault() {} });
  return {
    posted: posted.length,
    projectError: els['form-project-error'].textContent,
    bodyError: els['form-body-error'].textContent,
    idError: els['form-id-error'].textContent,
    assetInvalid: els['form-asset'].getAttribute('aria-invalid'),
    projectInvalid: els['form-project'].getAttribute('aria-invalid'),
    idInvalid: els['form-id'].getAttribute('aria-invalid'),
  };
}

(async () => {
  const cases = {};
  cases.spacedProject = await attempt('my project', '', 'a body', '');
  cases.slashProject = await attempt('a/b', '', 'a body', '');
  cases.longProject = await attempt('p'.repeat(65), '', 'a body', '');
  cases.emptyProject = await attempt('', '', 'a body', '');
  cases.spacedId = await attempt('p1', 'my id', 'a body', '');
  cases.reservedId = await attempt('p1', 'Claim', 'a body', '');
  cases.dotId = await attempt('p1', '..', 'a body', '');
  cases.longId = await attempt('p1', 'i'.repeat(129), 'a body', '');
  cases.blankBody = await attempt('p1', '', '  \n ', '/tmp/asset');
  cases.noBodyNoAsset = await attempt('p1', '', '', '');
  cases.bothBad = await attempt('bad proj', 'bad id', 'a body', '');
  cases.valid = await attempt('proj.1_x-Y', 'task.1_x-Y', 'a body', '');

  const verdicts = corpus.map(s => ({
    project: api.projectError(s) === null,
    id: api.customIdError(s) === null,
  }));

  const bodyVerdicts = bodyCorpus.map(p => api.bodyError(p[0], p[1]) === null);

  process.stdout.write(JSON.stringify({ cases, verdicts, bodyVerdicts }, null, 2));
})();
