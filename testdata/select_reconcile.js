'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

class Option {
  constructor(val, text) {
    this.value = val !== undefined ? String(val) : '';
    this.textContent = text !== undefined ? String(text) : '';
    this.parent = null;
  }
  remove() {
    if (this.parent) {
      const idx = this.parent.options.indexOf(this);
      if (idx >= 0) this.parent.options.splice(idx, 1);
      this.parent = null;
    }
  }
}

class Select {
  constructor() {
    this.options = [];
    this.value = '';
  }
  appendChild(opt) {
    opt.parent = this;
    this.options.push(opt);
  }
  insertBefore(opt, ref) {
    opt.parent = this;
    const oldIdx = this.options.indexOf(opt);
    if (oldIdx >= 0) this.options.splice(oldIdx, 1);
    const refIdx = ref ? this.options.indexOf(ref) : -1;
    if (refIdx >= 0) this.options.splice(refIdx, 0, opt);
    else this.options.push(opt);
  }
}

const workerSelect = new Select();
const projectSelect = new Select();

const els = {
  'filter-worker': workerSelect,
  'filter-project': projectSelect,
  'form-project-list': { options: [], appendChild() {}, innerHTML: '' },
};

const document = {
  activeElement: null,
  getElementById: id => els[id] || null,
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
  createElement: () => new Option(),
};

let workerData = ['w1', 'w2'];
let projectData = ['p1', 'p2'];

const fetchStub = async (url) => {
  const isWorkers = url.includes('/workers');
  const d = isWorkers ? workerData : projectData;
  return {
    ok: true,
    status: 200,
    headers: { get: () => 'application/json' },
    text: async () => JSON.stringify(d),
    json: async () => d,
  };
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\nreturn { syncSelectOptions, loadWorkers, loadProjects };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  fetchStub, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

(async () => {
  await api.loadWorkers();
  if (workerSelect.options.length !== 3) throw new Error('workerSelect init failed');
  const w1Node = workerSelect.options[1];

  await api.loadWorkers();
  const workerNodePreservedOnSame = (workerSelect.options[1] === w1Node);

  workerData = ['w1', 'w3'];
  await api.loadWorkers();
  const workerNodePreservedOnChange = (workerSelect.options[1] === w1Node && workerSelect.options[2].value === 'w3');

  await api.loadProjects();
  if (projectSelect.options.length !== 3) throw new Error('projectSelect init failed');
  const p1Node = projectSelect.options[1];

  await api.loadProjects();
  const projectNodePreservedOnSame = (projectSelect.options[1] === p1Node);

  projectData = ['p1', 'p4'];
  await api.loadProjects();
  const projectNodePreservedOnChange = (projectSelect.options[1] === p1Node && projectSelect.options[2].value === 'p4');

  process.stdout.write(JSON.stringify({
    workerNodePreservedOnSame,
    workerNodePreservedOnChange,
    projectNodePreservedOnSame,
    projectNodePreservedOnChange,
  }));
})();
