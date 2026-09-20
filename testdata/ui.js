'use strict';
const fs = require('fs');
const testName = process.argv[2];
const htmlSrc = fs.readFileSync(process.argv[3] || 'web/index.html', 'utf8');
const scriptSrc = htmlSrc.slice(htmlSrc.indexOf('<script>') + 8, htmlSrc.lastIndexOf('</script>'));

class DOMElement {
  constructor(tag, attrs = {}) {
    this.tagName = tag.toUpperCase();
    this.attrs = { ...attrs };
    this.children = [];
    this.parentNode = null;
    this.listeners = {};
    this.value = attrs.value || '';
    this.dataset = {};
    for (const [k, v] of Object.entries(attrs)) if (k.startsWith('data-')) this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = v;
  }
  get id() { return this.attrs.id || ''; } set id(v) { this.attrs.id = v; }
  get className() { return this.attrs.class || ''; } set className(v) { this.attrs.class = v; }
  get hidden() { return 'hidden' in this.attrs; } set hidden(v) { if (v) this.attrs.hidden = ''; else delete this.attrs.hidden; }
  get parentElement() { return this.parentNode; }
  get classList() {
    const s = new Set((this.className || '').split(/\s+/).filter(Boolean));
    return {
      toggle: (c, f) => { if (f === undefined ? !s.has(c) : f) s.add(c); else s.delete(c); this.className = [...s].join(' '); },
      contains: c => s.has(c),
    };
  }
  getAttribute(k) { return this.attrs[k] ?? null; }
  setAttribute(k, v) {
    this.attrs[k] = String(v);
    if (k.startsWith('data-')) this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = String(v);
  }
  appendChild(c) { if (c) { c.parentNode = this; this.children.push(c); } return c; }
  prepend(c) { if (c) { c.parentNode = this; this.children.unshift(c); } return c; }
  remove() { const i = this.parentNode ? this.parentNode.children.indexOf(this) : -1; if (i >= 0) this.parentNode.children.splice(i, 1); }
  replaceWith(el) { const i = this.parentNode ? this.parentNode.children.indexOf(this) : -1; if (i >= 0) { this.parentNode.children[i] = el; el.parentNode = this.parentNode; } }
  focus() { document.activeElement = this; } blur() { if (document.activeElement === this) document.activeElement = null; }
  scrollIntoView() {}
  reset() { for (const c of this.querySelectorAll('input, textarea, select')) c.value = ''; }
  requestSubmit() { this.dispatchEvent({ type: 'submit', target: this }); }
  addEventListener(t, fn) { (this.listeners[t] ||= []).push(fn); }
  dispatchEvent(e) {
    e.target ||= this; e.preventDefault ||= () => {};
    for (let c = this; c; c = c.parentNode) for (const fn of c.listeners[e.type] || []) fn.call(c, e);
  }
  closest(s) { for (let c = this; c?.tagName; c = c.parentNode) if (matches(c, s)) return c; return null; }
  matches(s) { return matches(this, s); }
  querySelector(s) { return this.querySelectorAll(s)[0] || null; }
  querySelectorAll(s) {
    const parts = s.split(',').map(x => x.trim()), res = [];
    const walk = n => { for (const c of n.children) { if (c.tagName !== '#TEXT' && parts.some(p => matches(c, p))) res.push(c); walk(c); } };
    walk(this);
    return res;
  }
  get textContent() { return this.tagName === '#TEXT' ? this.value : this.children.map(c => c.textContent).join(''); }
  set textContent(v) { this.children = []; const t = new DOMElement('#text'); t.value = String(v); this.appendChild(t); }
  set innerHTML(html) {
    parseHTML(html, this);
    for (const c of this.querySelectorAll('form')) for (const n of c.querySelectorAll('[name]')) c[n.getAttribute('name')] = n;
    if (this.tagName === 'FORM') for (const n of this.querySelectorAll('[name]')) this[n.getAttribute('name')] = n;
  }
}

function parseHTML(html, root) {
  root.children = [];
  const tagRe = /<(\/)?([a-zA-Z0-9]+)([^>]*)>|([^<]+)/g, stack = [root];
  let m;
  while ((m = tagRe.exec(html)) !== null) {
    const [, close, tag, rawAttrs, text] = m;
    if (text) {
      const t = new DOMElement('#text');
      t.value = text.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"');
      (t.parentNode = stack[stack.length - 1]).children.push(t);
    } else if (close) {
      if (stack.length > 1 && stack[stack.length - 1].tagName.toLowerCase() === tag.toLowerCase()) stack.pop();
    } else {
      const attrs = {};
      for (const [, k, v1, v2, v3] of rawAttrs.matchAll(/([a-zA-Z0-9_-]+)(?:=(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g)) attrs[k] = v1 ?? v2 ?? v3 ?? '';
      const el = new DOMElement(tag, attrs);
      (el.parentNode = stack[stack.length - 1]).children.push(el);
      if (!/^(input|br|hr|img|link|meta)$/i.test(tag)) stack.push(el);
    }
  }
}

function matchesSimple(el, sel) {
  if (!el || el.tagName === '#TEXT') return false;
  const m = sel.match(/^([a-z0-9_-]*)(\.([a-z0-9_-]+)|#([a-z0-9_-]+)|\[([a-z0-9_-]+)([\^=]?)=?["']?([^"']*)["']?\])?$/i);
  if (!m) return el.tagName.toLowerCase() === sel.toLowerCase();
  const [, tag, , cls, id, attr, op, val] = m;
  if (tag && el.tagName.toLowerCase() !== tag.toLowerCase()) return false;
  if (cls && !el.classList.contains(cls)) return false;
  if (id && el.id !== id) return false;
  const v = attr ? el.getAttribute(attr) : null;
  return !attr || (v !== null && (op === '^' ? v.startsWith(val) : op === '=' ? v === val : true));
}

function matches(el, selector) {
  const parts = selector.trim().split(/\s+/);
  if (!matchesSimple(el, parts[parts.length - 1])) return false;
  let cur = el.parentNode, i = parts.length - 2;
  for (; cur && i >= 0; cur = cur.parentNode) if (matchesSimple(cur, parts[i])) i--;
  return i < 0;
}

class FakeFormData {
  constructor(form) {
    this.data = new Map();
    for (const el of form?.querySelectorAll('input, select, textarea') || []) if (el.attrs.name) this.data.set(el.attrs.name, el.value ?? '');
  }
  get(k) { return this.data.get(k) ?? null; }
}

class FakeEventSource {
  static instances = [];
  constructor() { FakeEventSource.instances.push(this); this.listeners = {}; }
  addEventListener(t, fn) { (this.listeners[t] ||= []).push(fn); }
  dispatch(t, data) { for (const fn of this.listeners[t] || []) fn({ type: t, data }); }
}

const document = new DOMElement('document'), window = { addEventListener: (t, fn) => document.addEventListener(t, fn) };
document.documentElement = new DOMElement('html');
document.documentElement.appendChild(document.body = new DOMElement('body'));
document.activeElement = null;
document.createElement = tag => new DOMElement(tag);
document.getElementById = id => document.querySelector('#' + id);
document.querySelector = sel => document.documentElement.querySelector(sel);
document.querySelectorAll = sel => document.documentElement.querySelectorAll(sel);
document.body.innerHTML = (htmlSrc.match(/<body>([\s\S]*?)<script>/) || [])[1] || '';
const location = { pathname: '/ui', search: '', hash: '' }, history = { replaceState: (_, __, u) => { location.hash = u.includes('#') ? u.slice(u.indexOf('#')) : ''; } };

let timers = [], nextTimerId = 1;
const fakeSetTimeout = (fn, ms) => { const id = nextTimerId++; timers.push({ id, fn }); return id; };
const fakeClearTimeout = id => { timers = timers.filter(t => t.id !== id); };
async function flushTimers() { while (timers.length) { for (const t of timers.splice(0)) await t.fn(); } }
const sleep = ms => new Promise(r => setTimeout(r, ms));

const mkTask = (id, status, priority, body, extra = {}) => ({ id, status, priority, body, project: 'taskd', created_at: 1000, after: [], ...extra });
const fixtures = {
  renders_sections: [
    mkTask(1, 'buried', 3, 'Stuck Task\nDetails', { claim_count: 1 }),
    mkTask(2, 'leased', 2, 'Claimed Task', { worker: 'worker1', lease_expires: Math.floor((Date.now() + 60000) / 1000), claim_count: 1 }),
    mkTask(3, 'pending', 1, 'Queued Task'),
    mkTask(4, 'done', 3, 'Done Task', { worker: 'worker2', claim_count: 1 }),
  ],
  event_reload: [mkTask(1, 'pending', 3, 'Queued')],
  reply_kick: [mkTask(42, 'buried', 3, 'Stuck Task', { claim_count: 1 })],
  blocked_not_claimable: [mkTask(10, 'pending', 3, 'Prereq task'), mkTask(20, 'pending', 3, 'Blocked task', { created_at: 2000, after: [10] })],
};

const requests = [];
let fetches = 0, lastPost = null;
const fetchStub = async (url, opts = {}) => {
  const method = opts.method || 'GET', body = opts.body ? JSON.parse(opts.body) : null;
  requests.push({ method, url, body });
  if (url.startsWith('/tasks?limit=1000')) { fetches++; return { ok: true, status: 200, json: async () => fixtures[testName] || [] }; }
  if (method === 'POST' && url === '/tasks') { lastPost = body; return { ok: true, status: 201, json: async () => ({ id: 10, ...body }) }; }
  if (url === '/tasks/42') return { ok: true, status: 200, json: async () => ({ id: 42, notes: [] }) };
  return { ok: true, status: 200, json: async () => ({}) };
};

(async () => {
  new Function('document', 'location', 'history', 'window', 'fetch', 'EventSource', 'FormData', 'confirm', 'prompt', 'console', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'addEventListener', scriptSrc)(
    document, location, history, window, fetchStub, FakeEventSource, FakeFormData, () => true, () => 'test answer', console, fakeSetTimeout, fakeClearTimeout, () => {}, () => {}, Date, (t, f) => window.addEventListener(t, f)
  );

  await sleep(20);

  if (testName === 'renders_sections') {
    const sections = ['stuck', 'claimed', 'queued', 'done'].filter(s => !!document.querySelector('#' + s));
    process.stdout.write(JSON.stringify({ sections, statusText: document.querySelector('#status')?.textContent || '' }));
  } else if (testName === 'event_reload') {
    const initialFetches = fetches;
    FakeEventSource.instances[0]?.dispatch('change');
    FakeEventSource.instances[0]?.dispatch('change');
    const beforeFlushFetches = fetches;
    await flushTimers();
    process.stdout.write(JSON.stringify({ initialFetches, beforeFlushFetches, totalFetches: fetches }));
  } else if (testName === 'add_task') {
    const f = document.querySelector('form.add');
    if (f) { f.body.value = 'New Task Title\nDetailed instructions'; f.project.value = 'taskd'; f.pri.value = '2'; f.after.value = ''; f.requestSubmit(); }
    await sleep(20);
    process.stdout.write(JSON.stringify({ posted: lastPost }));
  } else if (testName === 'reply_kick') {
    const row = document.querySelector('tr[data-id="42"]');
    (row?.querySelector('td.task a') || row)?.dispatchEvent({ type: 'click', preventDefault() {} });
    await sleep(20);
    const form = document.querySelector('form.reply');
    if (form) { form.reply.value = 'I fixed the issue'; form.requestSubmit(); }
    await sleep(20);
    process.stdout.write(JSON.stringify({ requests }));
  } else if (testName === 'blocked_not_claimable') {
    const row = document.querySelector('tr[data-id="20"]');
    process.stdout.write(JSON.stringify({ position: row?.querySelector('td.n')?.textContent.trim() || '', why: row?.querySelector('span.why')?.textContent.trim() || '' }));
  }
})();
