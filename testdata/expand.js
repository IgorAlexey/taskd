'use strict';
const fs = require('fs');
const src = fs.readFileSync(process.argv[2], 'utf8');
const script = src.match(/<script>([\s\S]*?)<\/script>/)[1];

function element(id) {
  let html = '';
  const attrs = {};
  const listeners = {};
  const el = {
    id: id || '',
    value: '',
    textContent: '',
    open: false,
    style: {},
    dataset: {},
    options: [],
    onclick: null,
    scrollTop: 0,
    focusCount: 0,
    remove() {},
    appendChild() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    setAttribute(k, v) { attrs[k] = String(v); },
    removeAttribute(k) { delete attrs[k]; },
    getAttribute(k) { return attrs[k]; },
    focus() { this.focusCount++; focused = this; },
    addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
    dispatch(type, evt) { for (const fn of listeners[type] || []) fn(evt); },
    showModal() { this.open = true; opener = focused; },
    close() {
      if (!this.open) return;
      this.open = false;
      this.dispatch('close', { target: this });
    },
  };
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) { html = v; },
  });
  return el;
}

let focused = null;
let opener = null;
let windowKeydown = false;
const els = {};

const document = {
  getElementById: id => {
    if (!els[id]) els[id] = element(id);
    return els[id];
  },
  createElement: () => element(),
  querySelector: () => null,
  querySelectorAll: () => [],
};

const location = { pathname: '/ui', search: '', hash: '' };
const history = { pushState() {}, replaceState() {} };
const window = {
  addEventListener(type) {
    if (type === 'keydown') windowKeydown = true;
  },
};

const api = new Function(
  'document', 'location', 'history', 'window', 'fetch', 'console',
  'setInterval', 'clearInterval', 'setTimeout', 'clearTimeout',
  script + '\nreturn { renderTaskDetails, setupBodyOverlay };'
)(document, location, history, window,
  async () => ({ ok: true, status: 200, text: async () => '{}', json: async () => ({}) }),
  { log() {}, error() {} }, () => 0, () => {}, () => 0, () => {});

const body = 'line one\n' + 'x'.repeat(400) + '\nlast line';
const task = { id: 't-1', project: 'p1', status: 'pending', priority: 1, body };

api.setupBodyOverlay();
api.renderTaskDetails(task, false);

const out = {};
const pane = document.getElementById('task-details-content');
out.ButtonRendered = pane.innerHTML.includes('id="expand-body-btn"');

const btn = document.getElementById('expand-body-btn');
const dlg = document.getElementById('body-overlay');
const pre = document.getElementById('body-overlay-pre');

out.ClosedBeforeExpand = dlg.open === false;
out.NoWindowKeydown = windowKeydown === false;
out.HasClickHandler = typeof btn.onclick === 'function';

btn.focus();
if (out.HasClickHandler) btn.onclick();
out.OpenedAsModal = dlg.open === true;
out.OverlayBody = pre.textContent;
out.ModalOpenerWasControl = opener === btn;

const beforeClose = btn.focusCount;
dlg.close();
out.ClosedAfterDismiss = dlg.open === false;
out.FocusReturned = btn.focusCount > beforeClose && focused === btn;

if (out.HasClickHandler) btn.onclick();
const closeBtn = document.getElementById('body-overlay-close');
out.HasCloseHandler = typeof closeBtn.onclick === 'function';
if (out.HasCloseHandler) closeBtn.onclick();
out.ClosedAfterCloseButton = dlg.open === false;

if (out.HasClickHandler) btn.onclick();
dlg.dispatch('click', { target: pre });
out.PanelClickKeepsOpen = dlg.open === true;
dlg.dispatch('click', { target: dlg });
out.BackdropClickCloses = dlg.open === false;

if (out.HasClickHandler) btn.onclick();
api.renderTaskDetails({ id: 't-1', status: 'pending', body: 'fresh body' }, true);
out.OverlayRefreshed = pre.textContent === 'fresh body';
dlg.close();

const idle = pre.textContent;
api.renderTaskDetails({ id: 't-1', status: 'pending', body: 'later body' }, true);
out.ClosedOverlayUntouched = pre.textContent === idle;

process.stdout.write(JSON.stringify(out, null, 2));
