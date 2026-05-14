import { formats, types } from './formats.js';
import { highlightDisasm, highlightJSON } from './highlight.js';
import { conversionLinks, pushRoute, routeText, stylesFromLocation } from './routes.js';
import { convertAIL, loadAILWasm } from './wasm.js';

const defaultState = {
  fromStyle: 'openai-chat-completions',
  toStyle: 'anthropic-messages',
  convType: 'request',
  input: '',
  lastOutput: '',
  lastDisasm: '',
  error: '',
  loading: false,
  wasmReady: false,
  examplesReady: false,
  examples: {},
  showDisasm: false,
  toast: '',
  toastType: 'success',
};

export function createPlayground() {
  const initial = stylesFromLocation();

  return {
    ...defaultState,
    formats,
    types,
    links: conversionLinks(),
    page: routeText(initial?.fromStyle, initial?.toStyle),
    fromStyle: initial?.fromStyle || defaultState.fromStyle,
    toStyle: initial?.toStyle || defaultState.toStyle,
    manips: {
      slwin: { enabled: false, keepStart: 1, keepEnd: 10 },
      kvtools: { enabled: false, keepRecent: 1 },
    },

    get highlightedOutput() {
      if (!this.lastOutput) return '';
      return this.toStyle === 'ail' ? highlightDisasm(this.lastOutput) : highlightJSON(this.lastOutput);
    },

    get highlightedDisasm() {
      return this.lastDisasm ? highlightDisasm(this.lastDisasm) : '';
    },

    init() {
      this.applyPageText();
      this.loadExamples();
      this.$watch('fromStyle', () => this.afterRouteChange(true));
      this.$watch('toStyle', () => this.afterRouteChange(false));
      this.$watch('convType', () => this.loadExample());
      loadAILWasm().then(() => { this.wasmReady = true; }).catch((err) => { this.error = err.message; });
    },

    async loadExamples() {
      try {
        const response = await fetch('/examples.json');
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        this.examples = await response.json();
        this.examplesReady = true;
        this.loadExample();
      } catch (err) {
        this.flash(`Example fixtures unavailable: ${err.message}`, 'error');
      }
    },

    afterRouteChange(loadExample) {
      this.page = pushRoute(this.fromStyle, this.toStyle);
      this.applyPageText();
      if (loadExample) this.loadExample();
    },

    applyPageText() {
      document.title = this.page.title;
      const meta = document.querySelector('meta[name="description"]');
      if (meta) meta.setAttribute('content', this.page.intro);
    },

    loadExample() {
      if (!this.examplesReady) return this.flash('Examples are still loading', 'error');
      const pool = this.examples[this.fromStyle]?.[this.convType] || [];
      if (pool.length === 0) return this.flash('No fixture for this combination', 'error');
      const fixture = pool[Math.floor(Math.random() * pool.length)];
      this.input = typeof fixture.payload === 'string'
        ? fixture.payload
        : JSON.stringify(fixture.payload, null, 2);
      this.flash(`Loaded ${fixture.name}`);
      return null;
    },

    async convert() {
      const input = this.input.trim();
      if (!this.validateInput(input)) return;

      this.loading = true;
      this.error = '';
      try {
        const data = await convertAIL({
          input,
          fromStyle: this.fromStyle,
          toStyle: this.toStyle,
          type: this.convType,
          manips: this.currentManips(),
        });
        this.setResult(data);
      } catch (err) {
        this.error = `WASM error: ${err.message}`;
      } finally {
        this.loading = false;
      }
    },

    validateInput(input) {
      if (!input) return this.flash('Please enter input', 'error');
      if (this.fromStyle === 'ail') return true;
      try {
        JSON.parse(input);
        return true;
      } catch (err) {
        return this.flash(`Invalid JSON: ${err.message}`, 'error');
      }
    },

    setResult(data) {
      this.error = data.error || '';
      this.lastOutput = data.error ? '' : data.output || '';
      this.lastDisasm = data.error ? '' : data.disasm || '';
    },

    currentManips() {
      return {
        slwin: {
          enabled: !!this.manips.slwin.enabled,
          keepStart: normalize(this.manips.slwin.keepStart, 1, 0),
          keepEnd: normalize(this.manips.slwin.keepEnd, 10, 1),
        },
        kvtools: {
          enabled: !!this.manips.kvtools.enabled,
          keepRecent: normalize(this.manips.kvtools.keepRecent, 1, 0),
        },
      };
    },

    async copyText(text) {
      if (!text) return;
      try {
        await navigator.clipboard.writeText(text);
        this.flash('Copied');
      } catch {
        this.flash('Copy failed', 'error');
      }
    },

    flash(message, type = 'success') {
      this.toast = message;
      this.toastType = type;
      setTimeout(() => { this.toast = ''; }, 2000);
      return false;
    },
  };
}

function normalize(value, fallback, min) {
  const n = Number(value);
  return Number.isFinite(n) && n >= min ? Math.floor(n) : fallback;
}
