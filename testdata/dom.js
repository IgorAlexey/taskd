'use strict';

const VOID_TAGS = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr',
]);

const TOKEN = /<!--[\s\S]*?-->|<\/([a-zA-Z][\w-]*)\s*>|<([a-zA-Z][\w-]*)((?:\s+[^\s=>/]+(?:="[^"]*")?)*)\s*(\/?)>|([^<]+)/g;
const ATTR = /([^\s=]+)(?:="([^"]*)")?/g;
const COMPOUND = /^([a-zA-Z][\w-]*)?((?:\.[\w-]+|\[[^\]]*\]|:not\(\[[^\]]*\]\))*)$/;
const PREDICATE = /\.[\w-]+|\[[^\]]*\]|:not\(\[[^\]]*\]\)/g;
const ATTR_PREDICATE = /^\[([\w-]+)(?:="([^"]*)")?\]$/;

function unescapeHTML(s) {
  return s.replace(/&lt;/g, '<').replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'")
    .replace(/&amp;/g, '&');
}

function rejectAliased(name) {
  if (name === 'class' || name.startsWith('data-')) {
    throw new Error('dom.js: ' + name + ' lives in className/dataset, not the attribute map');
  }
}

function camelCase(name) {
  return name.replace(/-([a-z])/g, (_, c) => c.toUpperCase());
}

function attrValue(el, name) {
  if (name.startsWith('data-')) {
    const key = camelCase(name.slice(5));
    return el.dataset && key in el.dataset ? el.dataset[key] : null;
  }
  if (name === 'class') return el.className || null;
  return el.attrs && name in el.attrs ? el.attrs[name] : null;
}

function hasAttr(el, predicate) {
  const m = ATTR_PREDICATE.exec(predicate);
  if (!m) throw new Error('dom.js: unsupported attribute selector ' + predicate);
  const v = attrValue(el, m[1]);
  if (m[2] === undefined) return v !== null && v !== undefined;
  return String(v) === m[2];
}

function matchesCompound(el, compound) {
  const m = COMPOUND.exec(compound);
  if (!m || (!m[1] && !m[2])) throw new Error('dom.js: unsupported selector ' + compound);
  if (m[1] && el.tagName !== m[1].toUpperCase()) return false;
  for (const p of m[2].match(PREDICATE) || []) {
    if (p[0] === '.') {
      if (!String(el.className || '').split(/\s+/).includes(p.slice(1))) return false;
    } else if (p[0] === ':') {
      if (hasAttr(el, p.slice(5, -1))) return false;
    } else if (!hasAttr(el, p)) {
      return false;
    }
  }
  return true;
}

function descendants(el, out) {
  for (const c of el.children) {
    out.push(c);
    descendants(c, out);
  }
  return out;
}

function queryAll(root, selector) {
  const parts = String(selector).trim().split(/\s+/);
  if (parts.length > 2) {
    throw new Error('dom.js: at most one descendant combinator is supported: ' + selector);
  }
  let matched = descendants(root, []).filter(el => matchesCompound(el, parts[0]));
  if (parts.length === 2) {
    const out = [];
    for (const el of matched) {
      for (const d of descendants(el, [])) {
        if (matchesCompound(d, parts[1])) out.push(d);
      }
    }
    matched = out;
  }
  return matched;
}

function parseHTML(html, owner) {
  const stack = [];
  let pos = 0;
  TOKEN.lastIndex = 0;
  let m;
  while ((m = TOKEN.exec(html)) !== null) {
    if (m.index !== pos) {
      throw new Error('dom.js: cannot parse markup at offset ' + pos + ': ' + html.slice(pos, m.index + 20));
    }
    pos = TOKEN.lastIndex;
    const parent = stack.length > 0 ? stack[stack.length - 1] : owner;
    if (m[5] !== undefined) {
      parent.appendText(unescapeHTML(m[5]));
    } else if (m[1] !== undefined) {
      const open = stack.pop();
      if (!open || open.tagName !== m[1].toUpperCase()) {
        throw new Error('dom.js: unbalanced </' + m[1] + '> in ' + html);
      }
    } else if (m[2] !== undefined) {
      const el = createElement(m[2]);
      for (const [, name, value] of (m[3] || '').matchAll(ATTR)) {
        const v = value === undefined ? '' : unescapeHTML(value);
        if (name === 'class') el.className = v;
        else if (name.startsWith('data-')) el.dataset[camelCase(name.slice(5))] = v;
        else el.setAttribute(name, v);
      }
      parent.appendChild(el);
      if (!m[4] && !VOID_TAGS.has(m[2].toLowerCase())) stack.push(el);
    }
  }
  if (pos !== html.length) {
    throw new Error('dom.js: cannot parse markup at offset ' + pos + ': ' + html.slice(pos));
  }
  if (stack.length > 0) {
    throw new Error('dom.js: unclosed <' + stack[stack.length - 1].tagName.toLowerCase() + '> in ' + html);
  }
}

function textOf(nodes) {
  let out = '';
  for (const n of nodes) out += typeof n === 'string' ? n : textOf(n.childNodes);
  return out;
}

function createElement(tag) {
  let html = '';
  const nodes = [];
  const detach = () => {
    for (const n of nodes.splice(0)) {
      if (typeof n !== 'string') n.parentNode = null;
    }
  };
  const el = {
    tagName: tag.toUpperCase(),
    value: '',
    disabled: false,
    className: '',
    style: {},
    dataset: {},
    attrs: {},
    childNodes: nodes,
    options: [{ value: '', textContent: '(all)' }],
    onclick: null,
    parentNode: null,
    insertBefore(child, before) {
      if (child.parentNode) child.parentNode.removeChild(child);
      child.parentNode = this;
      const idx = before ? nodes.indexOf(before) : -1;
      if (idx === -1) {
        nodes.push(child);
      } else {
        nodes.splice(idx, 0, child);
      }
    },
    appendChild(child) { this.insertBefore(child, null); },
    appendText(text) { nodes.push(String(text)); },
    removeChild(child) {
      const idx = nodes.indexOf(child);
      if (idx !== -1) {
        nodes.splice(idx, 1);
        child.parentNode = null;
      }
    },
    remove() { if (this.parentNode) this.parentNode.removeChild(this); },
    addEventListener() {},
    querySelector(sel) {
      const all = queryAll(this, sel);
      return all.length > 0 ? all[0] : null;
    },
    querySelectorAll(sel) { return queryAll(this, sel); },
    setAttribute(k, v) { rejectAliased(k); this.attrs[k] = String(v); },
    getAttribute(k) { rejectAliased(k); return k in this.attrs ? this.attrs[k] : null; },
    hasAttribute(k) { rejectAliased(k); return k in this.attrs; },
    focus() {},
  };
  Object.defineProperty(el, 'children', {
    get() { return nodes.filter(n => typeof n !== 'string'); },
  });
  Object.defineProperty(el, 'textContent', {
    get() { return textOf(nodes); },
    set(v) {
      detach();
      const text = v === null || v === undefined ? '' : String(v);
      if (text !== '') nodes.push(text);
    },
  });
  Object.defineProperty(el, 'innerHTML', {
    get() { return html; },
    set(v) {
      html = String(v);
      detach();
      parseHTML(html, el);
    },
  });
  return el;
}

function response(status, data, headers = {}) {
  const allHeaders = { 'content-type': 'application/json', ...headers };
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: k => allHeaders[k.toLowerCase()] || allHeaders[k] || null },
    text: async () => (typeof data === 'string' ? data : JSON.stringify(data)),
    json: async () => (typeof data === 'string' ? JSON.parse(data) : data),
  };
}

module.exports = { createElement, response };
