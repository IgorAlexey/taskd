'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.slice(src.indexOf('<script>') + 8, src.lastIndexOf('</script>'));

const els = {};
function getOrCreate(id) {
  if (!els[id]) {
    els[id] = {
      id,
      value: '',
      textContent: '',
      options: [],
      hidden: false,
      disabled: false,
      attributes: {},
      listeners: {},
      setAttribute(k, v) { this.attributes[k] = String(v); },
      getAttribute(k) { return this.attributes[k] || null; },
      addEventListener(evt, fn) { this.listeners[evt] = fn; },
      focus() {},
    };
  }
  return els[id];
}

const document = {
  getElementById: id => getOrCreate(id),
  querySelector: () => null,
  querySelectorAll: () => [],
  addEventListener() {},
};

const sampleTask = {
  id: 101,
  project: 'proj-alpha',
  status: 'pending',
  priority: 15,
  claim_count: 0,
  body: 'Fix the widget layout',
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'AbortSignal',
  script + '\ncurrentTask = ' + JSON.stringify(sampleTask) + ';\nreturn { renderTaskDetails, toggleTaskEdit, getEls: () => els, getEditing: () => isEditingTask };'
)(
  document, { pathname: '/ui', search: '', hash: '' },
  { pushState() {}, replaceState() {} }, { addEventListener() {} },
  () => {}, { log() {}, error() {} },
  () => 0, () => {}, () => 1, () => {},
  Date,
  { timeout: () => new AbortController().signal }
);

api.renderTaskDetails(sampleTask, false);
const editBtn = getOrCreate('edit-task-btn');
if (editBtn.onclick) {
  editBtn.onclick();
}

process.stdout.write(JSON.stringify({
  isEditing: api.getEditing(),
  project: getOrCreate('edit-task-project').value,
  priority: String(getOrCreate('edit-task-priority').value),
  body: getOrCreate('edit-task-body').value,
}));
