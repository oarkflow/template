(function () {
  let core = null;
  let goRuntime = null;
  let root = null;
  let handlers = {};
  let signalCache = {};
  let bootOptions = null;
  let listenersAttached = false;
  let wasmReady = null;
  let debounceTimers = new WeakMap();

  function getCore() {
    if (!window.SPLWASMCore) {
      throw new Error('SPLWASMCore is not available');
    }
    return window.SPLWASMCore;
  }

  function assertLastError() {
    const err = core.getLastError();
    if (err) {
      throw new Error(err);
    }
  }

  function parseJSON(value, fallback) {
    if (!value) return fallback;
    try {
      return JSON.parse(value);
    } catch (_) {
      return fallback;
    }
  }

  function clone(value) {
    if (value == null) return value;
    return JSON.parse(JSON.stringify(value));
  }

  function readSignal(path) {
    return parseJSON(core.getSignal(path), null);
  }

  function syncSignals() {
    signalCache = parseJSON(core.getSignals(), {}) || {};
  }

  function syncHandlers() {
    handlers = parseJSON(core.getHandlers(), {}) || {};
  }

  function writeSignal(path, value) {
    core.setSignal(path, JSON.stringify(value));
    assertLastError();
    syncSignals();
  }

  function getPathValue(value, path) {
    if (!path.length) return value;
    let current = value;
    for (const part of path) {
      if (current == null) return undefined;
      current = current[part];
    }
    return current;
  }

  function setPathValue(target, path, value) {
    if (!path.length) return value;
    let current = target;
    for (let i = 0; i < path.length - 1; i += 1) {
      const key = path[i];
      const next = current[key];
      if (!next || typeof next !== 'object') {
        current[key] = {};
      }
      current = current[key];
    }
    current[path[path.length - 1]] = unwrapSignalValue(value);
    return target;
  }

  function resolveSignalName(target) {
    if (typeof target === 'string') return target;
    if (target && typeof target === 'object' && typeof target.__splSignalName === 'string') {
      return target.__splSignalName;
    }
    return '';
  }

  function unwrapSignalValue(value) {
    if (!value || typeof value !== 'object') return value;
    if (typeof value.__splSignalName === 'string') {
      return clone(readSignal(value.__splSignalName));
    }
    return value;
  }

  function signalPrimitive(name) {
    return {
      __splSignalName: name,
      valueOf() {
        return readSignal(name);
      },
      toString() {
        const value = readSignal(name);
        return value == null ? '' : String(value);
      },
    };
  }

  function signalProxy(name, path) {
    const currentValue = getPathValue(clone(readSignal(name)), path);
    if (currentValue == null || typeof currentValue !== 'object') {
      return signalPrimitive(name);
    }
    return new Proxy(currentValue, {
      get(_, prop) {
        if (prop === '__splSignalName') return name;
        if (prop === '__splSignalPath') return [name].concat(path).join('.');
        if (prop === Symbol.toPrimitive) {
          return function () {
            return getPathValue(clone(readSignal(name)), path);
          };
        }
        const nextValue = getPathValue(clone(readSignal(name)), path.concat(String(prop)));
        if (nextValue != null && typeof nextValue === 'object') {
          return signalProxy(name, path.concat(String(prop)));
        }
        return nextValue;
      },
      set(_, prop, value) {
        const nextRoot = clone(readSignal(name)) || {};
        setPathValue(nextRoot, path.concat(String(prop)), value);
        writeSignal(name, nextRoot);
        return true;
      },
      ownKeys() {
        const value = getPathValue(clone(readSignal(name)), path);
        return value && typeof value === 'object' ? Reflect.ownKeys(value) : [];
      },
      getOwnPropertyDescriptor() {
        return { configurable: true, enumerable: true };
      },
    });
  }

  function signal(name) {
    const value = readSignal(name);
    if (value != null && typeof value === 'object') {
      return signalProxy(name, []);
    }
    return signalPrimitive(name);
  }

  function setSignal(target, next) {
    const name = resolveSignalName(target);
    if (!name) return null;
    const prev = clone(readSignal(name));
    const value = typeof next === 'function' ? next(prev) : unwrapSignalValue(next);
    writeSignal(name, value);
    return value;
  }

  function toggle(target) {
    const name = resolveSignalName(target);
    if (!name) return false;
    const current = Boolean(readSignal(name));
    writeSignal(name, !current);
    return !current;
  }

  function ref(name) {
    return root ? root.querySelector(`[data-spl-ref="${name}"]`) : null;
  }

  function select(selector) {
    return root ? root.querySelector(selector) : null;
  }

  function selectAll(selector) {
    return root ? Array.from(root.querySelectorAll(selector)) : [];
  }

  function normalizeNumber(value) {
    const num = Number(value);
    return Number.isFinite(num) ? num : 0;
  }

  function cloneActionValue(value) {
    return clone(value);
  }

  function applyAction(action) {
    if (!action || typeof action !== 'object' || !action.kind || !action.target) {
      throw new Error('Invalid browser action payload');
    }
    if (action.kind === 'toggle') {
      toggle(action.target);
      return;
    }
    if (action.kind === 'set') {
      writeSignal(action.target, cloneActionValue(action.value));
      return;
    }
    if (action.kind === 'add') {
      writeSignal(action.target, normalizeNumber(readSignal(action.target)) + normalizeNumber(action.value));
      return;
    }
    if (action.kind === 'sub') {
      writeSignal(action.target, normalizeNumber(readSignal(action.target)) - normalizeNumber(action.value));
      return;
    }
    throw new Error(`Unsupported browser action kind: ${action.kind}`);
  }

  function runActions(actions) {
    if (!Array.isArray(actions)) {
      throw new Error('Invalid browser action list');
    }
    actions.forEach(applyAction);
  }

  function parseEventSpec(spec) {
    if (!spec) return null;
    const trimmed = String(spec).trim();
    if (!trimmed) return null;
    if ((trimmed.startsWith('[') && trimmed.endsWith(']')) || (trimmed.startsWith('{') && trimmed.endsWith('}'))) {
      return parseJSON(trimmed, null);
    }
    return trimmed;
  }

  function resolveEventActions(spec) {
    if (typeof spec === 'string') {
      const actions = handlers[spec];
      if (!Array.isArray(actions)) {
        throw new Error(`Unknown browser handler: ${spec}`);
      }
      return actions;
    }
    if (Array.isArray(spec)) {
      return spec;
    }
    if (spec && typeof spec === 'object') {
      if (spec.handler) {
        const actions = handlers[spec.handler];
        if (!Array.isArray(actions)) {
          throw new Error(`Unknown browser handler: ${spec.handler}`);
        }
        return actions;
      }
      if (Array.isArray(spec.actions)) {
        return spec.actions;
      }
    }
    throw new Error('Unsupported browser event spec');
  }

  function scheduleDebouncedEvent(element, key, delay, runner) {
    let timers = debounceTimers.get(element);
    if (!timers) {
      timers = new Map();
      debounceTimers.set(element, timers);
    }
    if (timers.has(key)) {
      clearTimeout(timers.get(key));
    }
    timers.set(key, setTimeout(() => {
      timers.delete(key);
      runner();
    }, delay));
  }

  function executeEventSpec(rawSpec, event, element) {
    const parsed = parseEventSpec(rawSpec);
    if (!parsed) return true;
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed) && parsed.delay > 0) {
      const debounceKey = `${event.type}:${rawSpec}`;
      scheduleDebouncedEvent(element, debounceKey, parsed.delay, () => {
        runActions(resolveEventActions(parsed));
        rerender().catch((err) => {
          console.error('[spl:browser]', err);
        });
      });
      return false;
    }
    runActions(resolveEventActions(parsed));
    return true;
  }

  function updateModels() {
    if (!root) return;
    root.querySelectorAll('[data-spl-model]').forEach((el) => {
      const path = el.getAttribute('data-spl-model');
      const value = readSignal(path);
      if (el.type === 'checkbox') {
        el.checked = Boolean(value);
        return;
      }
      if (el.type === 'radio') {
        el.checked = String(value) === String(el.value);
        return;
      }
      if (value == null) {
        if (el.tagName === 'SELECT') {
          el.value = '';
        }
        return;
      }
      const normalized = typeof value === 'object' ? JSON.stringify(value) : String(value);
      if (el.value !== normalized) {
        el.value = normalized;
      }
    });
  }

  function stringifyBindingValue(value) {
    if (value == null) return '';
    if (typeof value === 'object') {
      return JSON.stringify(value, null, 2);
    }
    return String(value);
  }

  function updateBindings() {
    if (!root) return;
    root.querySelectorAll('[data-spl-bind][data-spl-attr]').forEach((el) => {
      const path = el.getAttribute('data-spl-bind');
      const attr = el.getAttribute('data-spl-attr');
      const value = readSignal(path);
      const text = stringifyBindingValue(value);
      if (attr === 'textContent') {
        el.textContent = text;
        return;
      }
      if (attr === 'html') {
        el.innerHTML = text;
        return;
      }
      el.setAttribute(attr, text);
    });
  }

  function updateConditionals() {
    if (!root) return;
    root.querySelectorAll('[data-spl-if]').forEach((el) => {
      const path = el.getAttribute('data-spl-if');
      el.style.display = readSignal(path) ? '' : 'none';
    });
    root.querySelectorAll('[data-spl-else]').forEach((el) => {
      const path = el.getAttribute('data-spl-else');
      el.style.display = readSignal(path) ? 'none' : '';
    });
  }

  function captureFocus() {
    const active = document.activeElement;
    if (!active || !root || !root.contains(active)) return null;
    let selector = '';
    if (active.id) {
      selector = `#${active.id}`;
    } else if (active.getAttribute('data-spl-model')) {
      selector = `[data-spl-model="${active.getAttribute('data-spl-model')}"]`;
    } else if (active.name) {
      selector = `[name="${active.name}"]`;
    }
    if (!selector) return null;
    return {
      selector,
      start: typeof active.selectionStart === 'number' ? active.selectionStart : null,
      end: typeof active.selectionEnd === 'number' ? active.selectionEnd : null,
    };
  }

  function restoreFocus(snapshot) {
    if (!snapshot || !root) return;
    const next = root.querySelector(snapshot.selector);
    if (!next) return;
    next.focus();
    if (snapshot.start != null && snapshot.end != null && typeof next.setSelectionRange === 'function') {
      next.setSelectionRange(snapshot.start, snapshot.end);
    }
  }

  function applyHTML(html) {
    const focus = captureFocus();
    root.innerHTML = html;
    updateModels();
    updateBindings();
    updateConditionals();
    restoreFocus(focus);
  }

  async function rerender() {
    const html = core.render();
    assertLastError();
    syncSignals();
    syncHandlers();
    applyHTML(html);
  }

  function readModelValue(el) {
    if (el.type === 'checkbox') return Boolean(el.checked);
    if (el.type === 'radio') return el.checked ? el.value : readSignal(el.getAttribute('data-spl-model'));
    if (el.type === 'number' || el.type === 'range') return Number(el.value);
    return el.value;
  }

  function resolveTemplate(source) {
    return String(source || '').replace(/\{\{\s*([A-Za-z0-9_.]+)\s*\}\}/g, (_, path) => {
      const value = readSignal(path);
      if (value == null) return '';
      if (typeof value === 'object') return JSON.stringify(value);
      return String(value);
    });
  }

  function serializeForm(form) {
    const payload = {};
    new FormData(form).forEach((value, key) => {
      payload[key] = value;
    });
    form.querySelectorAll('[data-spl-model]').forEach((el) => {
      const path = el.getAttribute('data-spl-model');
      const key = (el.name || path.split('.').slice(-1)[0] || '').trim();
      if (!key) return;
      payload[key] = readModelValue(el);
    });
    return payload;
  }

  async function runAPIAction(sourceEl, event) {
    const method = (sourceEl.getAttribute('data-spl-api-method') || 'GET').toUpperCase();
    const url = sourceEl.getAttribute('data-spl-api-url');
    if (!url) return;
    const parseMode = (sourceEl.getAttribute('data-spl-api-parse') || 'json').toLowerCase();
    const target = sourceEl.getAttribute('data-spl-api-target') || '';
    const formMode = sourceEl.getAttribute('data-spl-api-form') || '';
    const resetSignals = (sourceEl.getAttribute('data-spl-api-reset') || '')
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);

    const init = { method, headers: {} };
    if (method !== 'GET' && method !== 'HEAD') {
      if (formMode === 'closest') {
        const form = sourceEl.closest('form');
        init.headers['Content-Type'] = 'application/json';
        init.body = JSON.stringify(serializeForm(form));
      } else if (sourceEl.hasAttribute('data-spl-api-body')) {
        init.headers['Content-Type'] = 'application/json';
        init.body = resolveTemplate(sourceEl.getAttribute('data-spl-api-body'));
      }
    }

    const response = await fetch(url, init);
    const payload = parseMode === 'text' ? await response.text() : await response.json();
    if (target) {
      const current = readSignal(target);
      const next = parseMode === 'json' && current != null && typeof current !== 'object'
        ? JSON.stringify(payload, null, 2)
        : payload;
      writeSignal(target, next);
    }
    resetSignals.forEach((name) => writeSignal(name, ''));
    if (event.type === 'submit') {
      event.preventDefault();
    }
    await rerender();
  }

  function findEventElement(event) {
    if (!root) return null;
    let node = event.target;
    while (node && node !== root) {
      if (node.nodeType === 1) {
        if (node.hasAttribute(`data-spl-on-${event.type}`) || node.hasAttribute('data-spl-api-url') || node.hasAttribute('data-spl-model')) {
          return node;
        }
      }
      node = node.parentNode;
    }
    return root;
  }

  async function handleEvent(event) {
    const el = findEventElement(event);
    if (!el) return;

    if (el.hasAttribute('data-spl-model') && (event.type === 'input' || event.type === 'change')) {
      writeSignal(el.getAttribute('data-spl-model'), readModelValue(el));
      await rerender();
      return;
    }

    const apiEvent = (el.getAttribute('data-spl-api-event') || (el.tagName === 'FORM' ? 'submit' : 'click')).toLowerCase();
    if (el.hasAttribute('data-spl-api-url') && apiEvent === event.type.toLowerCase()) {
      event.preventDefault();
      await runAPIAction(el, event);
      return;
    }

    const spec = el.getAttribute(`data-spl-on-${event.type}`);
    if (!spec) return;
    const mods = (el.getAttribute(`data-spl-on-${event.type}-mods`) || '')
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
    if (mods.includes('prevent') && typeof event.preventDefault === 'function') {
      event.preventDefault();
    }
    if (mods.includes('stop') && typeof event.stopPropagation === 'function') {
      event.stopPropagation();
    }
    const shouldRerender = executeEventSpec(spec, event, el);
    if (!shouldRerender) {
      return;
    }
    syncSignals();
    await rerender();
  }

  function attachListeners() {
    if (listenersAttached || !root) return;
    listenersAttached = true;
    ['click', 'input', 'change', 'submit', 'keydown'].forEach((eventName) => {
      root.addEventListener(eventName, (event) => {
        handleEvent(event).catch((err) => {
          console.error('[spl:browser]', err);
        });
      });
    });
  }

  async function ensureWasmLoaded() {
    if (wasmReady) return wasmReady;
    wasmReady = (async () => {
      goRuntime = new Go();
      const response = await fetch('/assets/spl.wasm');
      const bytes = await response.arrayBuffer();
      const result = await WebAssembly.instantiate(bytes, goRuntime.importObject);
      goRuntime.run(result.instance);
      core = getCore();
    })();
    return wasmReady;
  }

  async function boot(options) {
    bootOptions = options || {};
    await ensureWasmLoaded();
    root = document.querySelector(bootOptions.rootSelector || '#app');
    if (!root) {
      throw new Error('SPL browser root was not found');
    }
    const [bundleResponse, dataResponse] = await Promise.all([
      fetch(bootOptions.bundleURL),
      fetch(bootOptions.dataURL),
    ]);
    const bundle = await bundleResponse.json();
    const data = await dataResponse.json();
    const html = core.init(
      JSON.stringify(bundle),
      JSON.stringify(data),
      bootOptions.entry || bundle.entry || '',
      JSON.stringify(bootOptions.globals || {})
    );
    assertLastError();
    syncSignals();
    syncHandlers();
    applyHTML(html);
    attachListeners();
    return html;
  }

  window.SPLWASM = {
    boot,
    async setSignal(name, value) {
      await ensureWasmLoaded();
      writeSignal(name, value);
      await rerender();
    },
    async getSignal(name) {
      await ensureWasmLoaded();
      return readSignal(name);
    },
    rerender: async function () {
      await ensureWasmLoaded();
      await rerender();
    },
  };

  document.addEventListener('DOMContentLoaded', function () {
    const mount = document.querySelector('[data-spl-entry]');
    if (!mount) return;
    window.SPLWASM.boot({
      rootSelector: '#' + (mount.id || 'app'),
      bundleURL: mount.getAttribute('data-spl-bundle-url') || '/assets/spl-bundle.json',
      dataURL: mount.getAttribute('data-spl-data-url') || '/api/browser/page-data',
      entry: mount.getAttribute('data-spl-entry') || 'index.html',
    }).catch((err) => {
      console.error('[spl:browser]', err);
      mount.innerHTML = `<pre style="white-space: pre-wrap; color: #991b1b;">${String(err.message || err)}</pre>`;
    });
  });
}());
