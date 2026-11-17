// Dependency-free DOM/fetch/timer fixture. Tests execute the actual script.js.
'use strict';
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const input = fs.readFileSync(0, 'utf8');
// Standalone mode exercises JS even when Docker/PHP is unavailable. The Go
// harness instead passes real PHP output and validates renderer markup.
function standaloneRender(locale) {
    const path = require('node:path');
    const lang = fs.readFileSync(path.join(path.dirname(process.argv[2]), 'lang', locale, 'lang.php'), 'utf8');
    const strings = {};
    for (const m of lang.matchAll(/\$lang\['([^']+)'\] = '([^']*)';/g)) strings[m[1]] = m[2];
    const escape = s => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
    const html = `<div class="plugin_petar" data-ui="${escape(JSON.stringify(strings))}"><form>
        <select name="tool"><option value="yt-dlp">yt-dlp</option></select>
        <label class="petar-message-label"></label><textarea name="message"></textarea>
        <small class="petar-hint"></small><p class="petar-pattern" hidden>
        <input name="pattern" value="summarize" disabled /></p>
        <input name="sectok" value="token&quot;&lt;&amp;" />
        <button type="submit" disabled>submit</button><button class="petar-retry" type="button">retry</button>
        </form><p class="petar-status"></p><div class="petar-result" hidden></div></div>`;
    return {html: html + html, strings};
}
const rendered = input.trim() ? JSON.parse(input) : {en: standaloneRender('en'), ru: standaloneRender('ru')};
const source = fs.readFileSync(process.argv[2], 'utf8');
const decode = s => s.replace(/&quot;/g, '"').replace(/&#0?39;/g, "'").replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
class Element {
    constructor(tag, attrs = {}) {
        this.tagName = tag; this.attrs = attrs; this.children = []; this.events = {};
        this.dataset = {};
        for (const [k, v] of Object.entries(attrs)) if (k.startsWith('data-')) this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = v;
        this.value = attrs.value || ''; this.disabled = 'disabled' in attrs; this.hidden = 'hidden' in attrs;
        this.required = 'required' in attrs; this._text = '';
    }
    set innerHTML(_) { throw Error('Unsafe HTML sink'); }
    set textContent(s) { this._text = String(s); this.children = []; }
    get textContent() { return this._text + this.children.map(c => c.textContent).join(''); }
    appendChild(e) { this.children.push(e); e.parentElement = this; return e; }
    setAttribute(k, v) { this.attrs[k] = String(v); if (k.startsWith('data-')) this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = String(v); }
    getAttribute(k) { return this.attrs[k] ?? null; }
    removeAttribute(k) { delete this.attrs[k]; if (k.startsWith('data-')) delete this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())]; }
    addEventListener(k, cb) { (this.events[k] ||= []).push(cb); }
    fire(k) { for (const cb of this.events[k] || []) cb({target: this, preventDefault() {}}); }
    querySelectorAll(selector) {
        const matches = e => {
            const cls = selector.match(/^\.([\w-]+)$/);
            if (cls) return (e.attrs.class || '').split(' ').includes(cls[1]);
            const m = selector.match(/^([\w-]+)?(?:\[([\w-]+)(?:="([^"]*)")?\])?$/);
            return m && (!m[1] || e.tagName === m[1]) && (!m[2] || m[2] in e.attrs && (m[3] === undefined || e.attrs[m[2]] === m[3]));
        };
        return this.children.flatMap(c => [...(matches(c) ? [c] : []), ...c.querySelectorAll(selector)]);
    }
    querySelector(s) { return this.querySelectorAll(s)[0] || null; }
}
function parse(html) {
    const root = new Element('document'), stack = [root];
    for (const token of html.match(/<[^>]+>|[^<]+/g) || []) {
        if (token.startsWith('</')) { stack.pop(); continue; }
        if (token.startsWith('<')) {
            const tag = token.match(/^<([\w-]+)/)[1], attrs = {};
            const rest = token.slice(tag.length + 1, -1);
            for (const m of rest.matchAll(/([\w-]+)(?:="([^"]*)")?/g)) attrs[m[1]] = decode(m[2] || '');
            const e = stack.at(-1).appendChild(new Element(tag, attrs));
            if (!['input', 'br', 'hr'].includes(tag)) stack.push(e);
        } else stack.at(-1)._text += decode(token);
    }
    for (const select of root.querySelectorAll('select')) select.value = select.querySelector('option').value;
    return root;
}
const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve(); };
async function run(locale) {
    const document = parse(rendered[locale].html), requests = [], timers = new Map();
    document.createElement = tag => new Element(tag);
    document.getElementById = id => document.querySelectorAll('[id]').find(e => e.attrs.id === id);
    let timerID = 0;
    const context = {
        document, URLSearchParams, AbortController, console,
        setTimeout(cb, delay) { assert(delay > 0 && delay <= 15000); timers.set(++timerID, cb); return timerID; },
        clearTimeout(id) { timers.delete(id); },
        fetch(url, options) {
            return new Promise((resolve, reject) => {
                const r = {url, options, resolve, reject}; requests.push(r);
                if (options.signal) options.signal.addEventListener('abort', () => reject(Object.assign(Error('timeout'), {name: 'AbortError'})));
            });
        }
    };
    vm.runInNewContext(source, context);
    document.fire('DOMContentLoaded');
    const widgets = document.querySelectorAll('.plugin_petar');
    assert.equal(widgets.length, 2);
    assert.equal(requests.length, 2, 'each widget checks readiness');
    const parts = w => ({
        form: w.querySelector('form'), submit: w.querySelector('button[type="submit"]'),
        retry: w.querySelector('.petar-retry'), tool: w.querySelector('[name="tool"]'),
        message: w.querySelector('[name="message"]'), pattern: w.querySelector('[name="pattern"]'),
        row: w.querySelector('.petar-pattern'), label: w.querySelector('.petar-message-label'),
        result: w.querySelector('.petar-result'), status: w.querySelector('.petar-status')
    });
    const a = parts(widgets[0]), b = parts(widgets[1]), s = rendered[locale].strings;
    assert(a.submit.disabled && b.submit.disabled);
    assert.equal(a.label.textContent, s.label_url);
    assert(a.row.hidden && a.pattern.disabled);
    a.form.fire('submit');
    assert.equal(requests.length, 2, 'programmatic submit blocked before readiness');
    const reply = async (index, code, data) => {
        requests[index].resolve({ok: code >= 200 && code < 300, status: code, headers: {get: () => '<trace>'}, json: async () => data});
        await flush();
    };
    await reply(0, 503, {status: 'unavailable', error: '<img src=x onerror=evil()>'});
    await reply(1, 200, {status: 'ready'});
    assert(a.submit.disabled && !b.submit.disabled);
    assert.equal(a.status.textContent, s.err_backend, 'localized readiness error, no raw backend text');
    a.retry.fire('click');
    assert.equal(requests.length, 3);
    await reply(2, 200, {status: 'ready'});
    assert(!a.submit.disabled);
    a.tool.value = 'fabric'; a.tool.fire('change');
    assert.equal(a.label.textContent, s.label_text);
    assert(!a.row.hidden && !a.pattern.disabled && a.pattern.required);
    assert.equal(a.pattern.value, 'summarize');
    a.pattern.value = 'custom_pattern'; a.message.value = '  <b>source</b>\n'; a.form.fire('submit');
    assert(a.submit.disabled && !b.submit.disabled);
    const body = new URLSearchParams(requests[3].options.body);
    assert.equal(body.get('message'), '  <b>source</b>\n');
    assert.equal(body.get('pattern'), 'custom_pattern');
    assert.equal(body.get('tool'), 'fabric');
    assert.equal(body.get('sectok'), 'token"<&');
    assert(!body.has('url') && !body.has('text'));
    a.form.fire('submit'); assert.equal(requests.length, 4, 'no duplicate submit');
    // A later readiness failure must not be undone by submit completion.
    a.retry.fire('click');
    await reply(4, 503, {status: 'unavailable'});
    await reply(3, 201, {status: 'new', input_id: '<input>', process_id: '<process>'});
    assert(a.submit.disabled);
    assert.equal(a.result.dataset.inputId, '<input>');
    assert.equal(a.result.dataset.processId, '<process>');
    assert(a.result.textContent.includes(s.status_accepted));
    assert(a.result.textContent.includes('<input>') && a.result.textContent.includes('<trace>'));
    assert.equal(b.result.textContent, '', 'other result untouched');
    // Timeout releases busy but requires explicit retry and never resubmits.
    b.tool.value = 'echo-test'; b.tool.fire('change');
    assert.equal(b.label.textContent, s.label_message);
    assert(b.pattern.disabled && b.row.hidden);
    b.message.value = 'hello'; b.form.fire('submit');
    assert(!new URLSearchParams(requests[5].options.body).has('pattern'));
    for (const cb of [...timers.values()]) cb();
    await flush();
    assert(b.submit.disabled);
    assert.equal(b.result.textContent, s.err_submit_uncertain);
    assert.equal(requests.length, 6, 'no automatic retry');
    assert.equal(timers.size, 0, 'timers cleaned');
    b.retry.fire('click');
    await reply(6, 200, {status: 'ready'});
    assert(!b.submit.disabled);
    // Newer readiness takes precedence even when old fetch ignores abort.
    a.retry.fire('click'); a.retry.fire('click');
    await reply(8, 503, {status: 'unavailable'});
    await reply(7, 200, {status: 'ready'});
    assert(a.submit.disabled, 'stale ready response ignored');
    a.retry.fire('click');
    await reply(9, 200, {status: 'unavailable'});
    assert(a.submit.disabled, 'HTTP 200 without ready is not ready');
    a.retry.fire('click');
    for (const cb of [...timers.values()]) cb();
    await flush();
    assert(a.submit.disabled);
    assert.equal(a.status.textContent, s.err_backend, 'readiness timeout localized');
    a.retry.fire('click');
    await reply(11, 200, {status: 'ready'});
    a.tool.value = 'yt-dlp'; a.tool.fire('change');
    assert.equal(a.label.textContent, s.label_url);
    assert.equal(a.message.getAttribute('placeholder'), s.hint_url);
    a.message.value = 'https://example.org/video'; a.form.fire('submit');
    const urlBody = new URLSearchParams(requests[12].options.body);
    assert.equal(urlBody.get('message'), 'https://example.org/video');
    assert(!urlBody.has('pattern') && !urlBody.has('url'));
    await reply(12, 502, {error: '<script>secret backend</script>'});
    assert(a.submit.disabled);
    assert(a.result.textContent.includes(s.err_submit_uncertain));
    assert(!a.result.textContent.includes('secret backend'));
    assert.equal(requests.length, 13, 'server error never automatically retries submit');
    assert.equal(timers.size, 0);
    for (const r of requests) {
        assert(r.url.startsWith('/lib/exe/ajax.php?call=plugin_petar&action='));
        assert(r.options.signal, 'finite timeout signal');
        assert.equal(r.options.credentials, 'same-origin');
    }
}
(async () => { await run('en'); await run('ru'); console.log('UI behavior passed (EN/RU)'); })().catch(e => { console.error(e); process.exitCode = 1; });
