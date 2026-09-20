'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

let keydownHandler = null;
const tbody = {
  querySelectorAll: () => [],
  querySelector: () => null,
  addEventListener(type, fn) {
    if (type === 'keydown') keydownHandler = fn;
  },
};

const document = {
  getElementById: id => (id === 'task-table-body' ? tbody : null),
  querySelectorAll: () => [],
  querySelector: () => null,
};

let selectedTaskId = null;
const win = {
  addEventListener() {},
  history: { pushState() {}, replaceState() {} },
  location: { pathname: '/ui', search: '', hash: '' },
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval',
  script + '\nreturn { setupRowActivation, selectTask: id => { selectedTaskId = id; }, getSelected: () => selectedTaskId };'
)(document, win.location, win.history, win, async () => ({ ok: true, json: async () => ({}) }), { log() {}, error() {} }, () => 0, () => {});

api.setupRowActivation();
if (!keydownHandler) {
  console.error('keydown handler was not registered on tbody');
  process.exit(1);
}

let active = null;
function makeRow(id) {
  return {
    dataset: { taskId: id },
    focus() { active = this; },
    closest(sel) { return sel.includes('tr') ? this : null; },
    nextElementSibling: null,
    previousElementSibling: null,
  };
}

const r1 = makeRow('task-1');
const r2 = makeRow('task-2');
const r3 = makeRow('task-3');
r1.nextElementSibling = r2;
r2.previousElementSibling = r1;
r2.nextElementSibling = r3;
r3.previousElementSibling = r2;

const b1 = {
  closest(sel) { return sel.includes('tr') ? r1 : null; },
};
const b2 = {
  closest(sel) { return sel.includes('tr') ? r2 : null; },
};

function press(target, key, extra = {}) {
  let prevented = false;
  const evt = Object.assign({
    key,
    target,
    preventDefault() { prevented = true; },
  }, extra);
  keydownHandler(evt);
  return prevented;
}

const out = {};

r1.focus();
press(r1, 'ArrowDown');
out.DownFromRow1Selected = api.getSelected();
out.DownFromRow1Focused = active ? active.dataset.taskId : null;

press(active, 'ArrowDown');
out.DownFromRow2Selected = api.getSelected();
out.DownFromRow2Focused = active ? active.dataset.taskId : null;

press(active, 'ArrowDown');
out.DownAtBottomSelected = api.getSelected();
out.DownAtBottomFocused = active ? active.dataset.taskId : null;

press(active, 'ArrowUp');
out.UpFromRow3Selected = api.getSelected();
out.UpFromRow3Focused = active ? active.dataset.taskId : null;

press(active, 'ArrowUp');
out.UpFromRow2Selected = api.getSelected();
out.UpFromRow2Focused = active ? active.dataset.taskId : null;

press(active, 'ArrowUp');
out.UpAtTopSelected = api.getSelected();
out.UpAtTopFocused = active ? active.dataset.taskId : null;

b1.closest('tr').focus();
press(b1, 'ArrowDown');
out.DownFromButton1Selected = api.getSelected();
out.DownFromButton1Focused = active ? active.dataset.taskId : null;

b2.closest('tr').focus();
press(b2, 'ArrowUp');
out.UpFromButton2Selected = api.getSelected();
out.UpFromButton2Focused = active ? active.dataset.taskId : null;

r1.focus();
api.selectTask('task-1');
press(r1, 'ArrowDown', { ctrlKey: true });
out.ModifierIgnored = (api.getSelected() === 'task-1' && active === r1);

process.stdout.write(JSON.stringify(out, null, 2));
