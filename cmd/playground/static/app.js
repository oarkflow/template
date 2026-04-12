const templateEditorRoot = document.getElementById('templateEditor');
const dataEditorRoot = document.getElementById('dataEditor');
const runBtn = document.getElementById('runBtn');
const copyBtn = document.getElementById('copyBtn');
const resetBtn = document.getElementById('resetBtn');
const themeBtn = document.getElementById('themeBtn');
const resultEl = document.getElementById('result');
const errorEl = document.getElementById('error');
const previewEl = document.getElementById('preview');
const durationEl = document.getElementById('duration');
const statusBadge = document.getElementById('statusBadge');
const healthText = document.getElementById('healthText');
const exampleList = document.getElementById('exampleList');
const exampleSearch = document.getElementById('exampleSearch');
const tabButtons = Array.from(document.querySelectorAll('.tab-btn'));
const panels = Array.from(document.querySelectorAll('.panel'));

const TEMPLATE_KEY = 'spl.template.playground.template';
const DATA_KEY = 'spl.template.playground.data';
const THEME_KEY = 'spl.template.playground.theme';

let templateExamples = [];
let templateMonaco = null;
let dataMonaco = null;

function escapeHTML(value) {
  return String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// --- Editor helpers ---

function getTemplateValue() {
  return templateMonaco ? templateMonaco.getValue() : '';
}

function setTemplateValue(value) {
  if (templateMonaco) templateMonaco.setValue(value || '');
}

function getDataValue() {
  return dataMonaco ? dataMonaco.getValue() : '';
}

function setDataValue(value) {
  if (dataMonaco) dataMonaco.setValue(value || '');
}

// --- UI state ---

function setBusy(isBusy) {
  runBtn.disabled = isBusy;
  runBtn.classList.toggle('opacity-70', isBusy);
  runBtn.textContent = isBusy ? 'Rendering...' : 'Run';
}

function setStatus(kind, text) {
  const base = 'ml-1 inline-flex items-center px-2 py-0.5 rounded-full text-[11px]';
  if (kind === 'success') {
    statusBadge.className = `${base} bg-emerald-100 text-emerald-700`;
  } else if (kind === 'error') {
    statusBadge.className = `${base} bg-rose-100 text-rose-700`;
  } else if (kind === 'running') {
    statusBadge.className = `${base} bg-amber-100 text-amber-700`;
  } else {
    statusBadge.className = `${base} bg-slate-200 text-slate-700`;
  }
  statusBadge.textContent = text;
}

function setTab(tab) {
  for (const btn of tabButtons) {
    if (btn.dataset.tab === tab) {
      btn.classList.add('bg-slate-200', 'dark:bg-slate-800');
    } else {
      btn.classList.remove('bg-slate-200', 'dark:bg-slate-800');
    }
  }
  for (const panel of panels) {
    panel.classList.toggle('hidden', panel.dataset.panel !== tab);
  }
}

// --- Persistence ---

function persistTemplate() {
  localStorage.setItem(TEMPLATE_KEY, getTemplateValue());
  localStorage.setItem(DATA_KEY, getDataValue());
}

function restoreTemplate() {
  const tmpl = localStorage.getItem(TEMPLATE_KEY);
  const data = localStorage.getItem(DATA_KEY);
  let restored = false;
  if (tmpl && tmpl.trim()) {
    setTemplateValue(tmpl);
    restored = true;
  }
  if (data && data.trim()) {
    setDataValue(data);
    restored = true;
  }
  return restored;
}

// --- Output ---

function applyResponse(payload) {
  resultEl.textContent = payload.result || '-';
  const err = payload.error || '';
  errorEl.textContent = err ? `ERROR:\n${err}` : '';
  durationEl.textContent = payload.duration_ms != null ? `${payload.duration_ms} ms` : '-';

  if (err) {
    setStatus('error', 'Template Error');
    setTab('error');
  } else if (payload.result) {
    previewEl.srcdoc = payload.result;
    setStatus('success', 'Rendered');
    setTab('preview');
  } else {
    setStatus('success', 'Empty');
    setTab('result');
  }
}

function clearPanels() {
  resultEl.textContent = '-';
  errorEl.textContent = '';
  previewEl.srcdoc = '';
  durationEl.textContent = '-';
  setStatus('idle', 'Idle');
  setTab('preview');
}

// --- Execution ---

async function runTemplate() {
  setBusy(true);
  setStatus('running', 'Rendering');
  errorEl.textContent = '';
  try {
    const res = await fetch('/api/render', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ template: getTemplateValue(), data: getDataValue() }),
    });
    const payload = await res.json();
    applyResponse(payload);
  } catch (err) {
    errorEl.textContent = `Request failed: ${err.message}`;
    setStatus('error', 'Request Error');
    setTab('error');
  } finally {
    setBusy(false);
  }
}

// --- Theme ---

function applyTheme(theme) {
  if (theme === 'dark') {
    document.documentElement.classList.add('dark');
    if (window.monaco) monaco.editor.setTheme('vs-dark');
  } else {
    document.documentElement.classList.remove('dark');
    if (window.monaco) monaco.editor.setTheme('vs');
  }
  localStorage.setItem(THEME_KEY, theme);
}

function initTheme() {
  const saved = localStorage.getItem(THEME_KEY);
  if (saved) { applyTheme(saved); return; }
  const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
  applyTheme(prefersDark ? 'dark' : 'light');
}

// --- Examples ---

function renderExamples(filter = '') {
  exampleList.innerHTML = '';
  const query = filter.trim().toLowerCase();

  const filtered = templateExamples.filter((ex) =>
    ((ex.label || ex.name || '') + ' ' + (ex.category || '') + ' ' + (ex.tags || []).join(' ')).toLowerCase().includes(query)
  );
  let currentCategory = '';
  for (const ex of filtered) {
    const category = ex.category || 'Templates';
    if (category !== currentCategory) {
      currentCategory = category;
      const section = document.createElement('div');
      section.className = 'px-2.5 pt-3 pb-1 text-[11px] uppercase tracking-[0.18em] text-slate-500 dark:text-slate-400';
      section.textContent = currentCategory;
      exampleList.appendChild(section);
    }
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'w-full text-left px-2.5 py-2 text-sm rounded-md border border-transparent hover:border-slate-300 dark:hover:border-slate-700 hover:bg-slate-100 dark:hover:bg-slate-800';
    const tags = Array.isArray(ex.tags) && ex.tags.length
      ? `<div class="mt-1 flex flex-wrap gap-1">${ex.tags.map((tag) => `<span class="inline-flex items-center rounded-full bg-slate-200 dark:bg-slate-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-slate-600 dark:text-slate-300">${escapeHTML(tag)}</span>`).join('')}</div>`
      : '';
    btn.innerHTML = `<span class="font-medium">${escapeHTML(ex.label || ex.name)}</span>${tags}`;
    btn.addEventListener('click', () => {
      setTemplateValue(ex.template || '');
      try {
        const parsed = JSON.parse(ex.data || '{}');
        setDataValue(JSON.stringify(parsed, null, 2));
      } catch {
        setDataValue(ex.data || '{}');
      }
      persistTemplate();
      clearPanels();
    });
    exampleList.appendChild(btn);
  }
  if (filtered.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'px-2.5 py-3 text-xs text-slate-500 dark:text-slate-400';
    empty.textContent = 'No examples match this search.';
    exampleList.appendChild(empty);
  }
}

// --- Health & Examples loading ---

async function loadHealth() {
  try {
    const res = await fetch('/api/health');
    const payload = await res.json();
    healthText.textContent = payload.ok ? 'Service healthy' : 'Service unhealthy';
  } catch {
    healthText.textContent = 'Service unavailable';
  }
}

async function loadExamples() {
  try {
    const res = await fetch('/api/examples');
    const payload = await res.json();
    templateExamples = payload.template_examples || [];
    renderExamples('');

    if (!restoreTemplate()) {
      if (templateExamples.length > 0) {
        const first = templateExamples[0];
        setTemplateValue(first.template || '');
        try {
          setDataValue(JSON.stringify(JSON.parse(first.data || '{}'), null, 2));
        } catch {
          setDataValue(first.data || '{}');
        }
        persistTemplate();
      }
    }
  } catch (err) {
    errorEl.textContent = `Failed to load examples: ${err.message}`;
    setTab('error');
  }
}

// --- Monaco initialization ---

function initMonaco() {
  return new Promise((resolve, reject) => {
    if (!window.require) {
      reject(new Error('Monaco loader not found'));
      return;
    }
    window.require.config({ paths: { vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.52.2/min/vs' } });
    window.require(['vs/editor/editor.main'], () => {
      // Register SPL Template language
      monaco.languages.register({ id: 'spl-template' });
      monaco.languages.setMonarchTokensProvider('spl-template', {
        tokenizer: {
          root: [
            [/@\/\/.*$/, 'comment'],
            [/@(if|elseif|else|for|empty|switch|case|default|raw|include|extends|block|define|component|render|slot|fill|let|computed|watch|signal|effect|reactive|bind|handler|import)\b/, 'keyword'],
            [/\$\{/, { token: 'delimiter.bracket', next: '@expr' }],
            [/<\/?[\w-]+/, 'tag'],
            [/>/, 'tag'],
            [/=/, 'delimiter'],
            [/"([^"\\]|\\.)*"/, 'string'],
            [/'([^'\\]|\\.)*'/, 'string'],
          ],
          expr: [
            [/\}/, { token: 'delimiter.bracket', next: '@pop' }],
            [/\|/, 'operator'],
            [/(raw|upper|lower|trim|title|escape|json|format|default|slug|truncate|nl2br|urlencode|reverse|capitalize|replace)\b/, 'keyword'],
            [/"([^"\\]|\\.)*"/, 'string'],
            [/\b[0-9]+\b/, 'number'],
            [/[a-zA-Z_][\w]*/, 'identifier'],
          ],
        },
      });

      const isDark = document.documentElement.classList.contains('dark');
      const editorTheme = isDark ? 'vs-dark' : 'vs';
      const editorDefaults = {
        automaticLayout: true,
        fontSize: 14,
        fontFamily: 'JetBrains Mono, Fira Code, Menlo, monospace',
        minimap: { enabled: false },
        roundedSelection: true,
        scrollBeyondLastLine: false,
        padding: { top: 12, bottom: 12 },
        theme: editorTheme,
      };

      templateMonaco = monaco.editor.create(templateEditorRoot, {
        ...editorDefaults,
        value: '',
        language: 'spl-template',
      });
      templateMonaco.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => runBtn.click());
      templateMonaco.onDidChangeModelContent(() => persistTemplate());

      dataMonaco = monaco.editor.create(dataEditorRoot, {
        ...editorDefaults,
        value: '{}',
        language: 'json',
      });
      dataMonaco.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, () => runBtn.click());
      dataMonaco.onDidChangeModelContent(() => persistTemplate());

      resolve();
    });
  });
}

// --- Event listeners ---

runBtn.addEventListener('click', () => runTemplate());
copyBtn.addEventListener('click', async () => {
  await navigator.clipboard.writeText(getTemplateValue());
  setStatus('success', 'Copied');
});
resetBtn.addEventListener('click', () => {
  localStorage.removeItem(TEMPLATE_KEY);
  localStorage.removeItem(DATA_KEY);
  setTemplateValue('');
  setDataValue('{}');
  clearPanels();
});
themeBtn.addEventListener('click', () => {
  const next = document.documentElement.classList.contains('dark') ? 'light' : 'dark';
  applyTheme(next);
});
exampleSearch.addEventListener('input', () => renderExamples(exampleSearch.value));
for (const btn of tabButtons) {
  btn.addEventListener('click', () => setTab(btn.dataset.tab));
}

// --- Boot ---

async function boot() {
  initTheme();
  setStatus('idle', 'Idle');
  setTab('preview');
  clearPanels();
  try {
    await initMonaco();
  } catch (e) {
    errorEl.textContent = `Failed to initialize editor: ${e.message}`;
    setTab('error');
    return;
  }
  await loadHealth();
  await loadExamples();
}

boot();
