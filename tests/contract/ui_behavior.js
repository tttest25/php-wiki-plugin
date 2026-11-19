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
        <select name="tool" disabled></select>
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
    focus() { this.focused = true; }
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
    for (const select of root.querySelectorAll('select')) select.value = select.querySelector('option')?.value || '';
    return root;
}
const flush = async () => { for (let i = 0; i < 25; i++) await Promise.resolve(); };
async function run(locale) {
    const document = parse(rendered[locale].html), requests = [], timers = new Map();
    document.createElement = tag => new Element(tag);
    document.getElementById = id => document.querySelectorAll('[id]').find(e => e.attrs.id === id);
    let timerID = 0, firstCatalog, holdCatalog = false;
    const heldCatalogs = [];
    const actions = [
        {action: 'yt-dlp', version: 1, title: 'Video'},
        {action: 'fabric', version: 1, title: 'Fabric'},
        {action: 'echo-test', version: 1, title: 'Echo'},
        {action: 'fabric', version: 2, title: '<Fabric v2>'},
    ];
    const context = {
        document, URLSearchParams, AbortController, console,
        setTimeout(cb, delay) { assert(delay > 0 && delay <= 15000); timers.set(++timerID, cb); return timerID; },
        clearTimeout(id) { timers.delete(id); },
        fetch(url, options) {
            return new Promise((resolve, reject) => {
                if (url.includes('action=actions')) {
                    if (holdCatalog) {
                        heldCatalogs.push({resolve});
                        return;
                    }
                    const finish = () => resolve({ok: true, status: 200, headers: {get: () => null}, json: async () => ({actions})});
                    if (!firstCatalog) firstCatalog = finish;
                    else finish();
                    return;
                }
                if (url.includes('action=jobs')) {
                    resolve({ok: true, status: 200, headers: {get: () => null},
                        json: async () => ({jobs: [], next_cursor: null})});
                    return;
                }
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
    assert(a.row.hidden && a.pattern.disabled);
    a.form.fire('submit');
    assert.equal(requests.length, 2, 'programmatic submit blocked before readiness');
    const reply = async (index, code, data) => {
        requests[index].resolve({ok: code >= 200 && code < 300, status: code, headers: {get: () => '<trace>'}, json: async () => data});
        await flush();
    };
    await reply(0, 503, {status: 'unavailable', error: '<img src=x onerror=evil()>'});
    await reply(1, 200, {status: 'ready'});
    assert(b.submit.disabled, 'ready without a catalog must not enable submit');
    assert(firstCatalog, 'ready form must request the catalog');
    firstCatalog();
    await flush();
    assert(a.submit.disabled && !b.submit.disabled);
    assert.equal(a.status.textContent, s.err_backend, 'localized readiness error, no raw backend text');
    a.retry.fire('click');
    assert.equal(requests.length, 3);
    await reply(2, 200, {status: 'ready'});
    assert(!a.submit.disabled);
    a.tool.value = '1'; a.tool.fire('change');
    assert.equal(a.label.textContent, s.label_text);
    assert(!a.row.hidden && !a.pattern.disabled && a.pattern.required);
    assert.equal(a.pattern.value, 'summarize');
    a.pattern.value = 'custom_pattern'; a.message.value = '  <b>source</b>\n'; a.form.fire('submit');
    assert(a.submit.disabled && !b.submit.disabled);
    const body = new URLSearchParams(requests[3].options.body);
    assert.deepEqual(JSON.parse(body.get('data')), {text: '  <b>source</b>\n', pattern: 'custom_pattern'});
    assert.equal(body.get('action_name'), 'fabric');
    assert.equal(body.get('version'), '1');
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
    b.tool.value = '2'; b.tool.fire('change');
    assert.equal(b.label.textContent, s.label_message);
    assert(b.pattern.disabled && b.row.hidden);
    b.message.value = 'hello'; b.form.fire('submit');
    assert.deepEqual(JSON.parse(new URLSearchParams(requests[5].options.body).get('data')), {message: 'hello'});
    for (const cb of [...timers.values()]) cb();
    await flush();
    assert(b.submit.disabled);
    assert.equal(b.result.textContent, s.err_submit_uncertain);
    assert.equal(requests.length, 6, 'no automatic retry');
    assert.equal(timers.size, 0, 'timers cleaned');
    b.retry.fire('click');
    assert.equal(requests[6].options.method, 'GET', 'manual readiness does not submit');
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
    a.tool.value = '0'; a.tool.fire('change');
    assert.equal(a.label.textContent, s.label_url);
    assert.equal(a.message.getAttribute('placeholder'), s.hint_url);
    a.tool.value = '3'; a.tool.fire('change');
    assert.equal(a.label.textContent, s.label_json);
    assert(a.row.hidden);
    a.message.value = '{'; a.form.fire('submit');
    assert.equal(requests.length, 12);
    assert.equal(a.result.textContent, s.err_invalid_json);
    a.message.value = '9007199254740993'; a.form.fire('submit');
    const raw = new URLSearchParams(requests[12].options.body);
    assert.equal(raw.get('action_name'), 'fabric');
    assert.equal(raw.get('version'), '2');
    assert.equal(raw.get('data'), '9007199254740993');
    await reply(12, 400, {error: 'PRIVATE'});
    assert(a.result.textContent.includes(s.err_submit_rejected));
    a.tool.value = '0'; a.tool.fire('change');
    a.message.value = 'https://example.org/video'; a.form.fire('submit');
    const urlBody = new URLSearchParams(requests[13].options.body);
    assert.deepEqual(JSON.parse(urlBody.get('data')), {url: 'https://example.org/video'});
    await reply(13, 502, {error: '<script>secret backend</script>'});
    assert(a.submit.disabled);
    assert(a.result.textContent.includes(s.err_submit_uncertain));
    assert(!a.result.textContent.includes('secret backend'));
    assert.equal(requests.length, 14, 'server error never automatically retries submit');
    assert.equal(timers.size, 0);
    const postCount = () => requests.filter(r => r.options.method === 'POST').length;
    const before = postCount(), rejectionIndex = requests.length;
    const privateMarker = 'PRIVATE_UPSTREAM_BODY';
    assert(!b.submit.disabled);
    b.form.fire('submit');
    assert.equal(postCount(), before + 1, 'exactly one POST for a new submission');
    await reply(rejectionIndex, 400, {error: privateMarker});
    const rejectionText = b.result.textContent;
    assert.notEqual(b.result.querySelector('p').textContent, s.err_request, `${locale}: HTTP 400 needs a distinct rejection`);
    assert(typeof s.err_submit_rejected === 'string' && s.err_submit_rejected.trim(), `${locale}: missing rejection translation`);
    assert(rejectionText.includes(s.err_submit_rejected), `${locale}: expected localized rejection`);
    assert(!rejectionText.includes(privateMarker) && !rejectionText.includes(s.err_submit_uncertain));
    assert.equal(b.result.dataset.inputId, undefined);
    assert.equal(b.result.dataset.processId, undefined);
    assert(!b.submit.disabled, 'the user can correct the input and submit manually');
    assert.equal(postCount(), before + 1, 'no automatic POST after rejection');
    holdCatalog = true;
    const ready = async () => {
        b.retry.fire('click');
        await reply(requests.length - 1, 200, {status: 'ready'});
    };
    const finishCatalog = async (index, code, data) => {
        heldCatalogs[index].resolve({ok: code === 200, status: code, headers: {get: () => null}, json: async () => data});
        await flush();
    };
    await ready();
    await ready();
    await finishCatalog(1, 502, {error: privateMarker});
    await finishCatalog(0, 200, {actions});
    assert(b.submit.disabled, 'late catalog never takes over a newer failed check');
    assert.equal(b.status.textContent, s.catalog_error);
    b.form.fire('submit');
    assert.equal(postCount(), before + 1, 'no stale selection submitted after catalog error');
    await ready();
    await finishCatalog(2, 200, {actions: []});
    assert(b.submit.disabled);
    assert.equal(b.status.textContent, s.catalog_empty);
    assert.equal(b.tool.children.length, 0, 'no static fallback');
    await ready();
    await finishCatalog(3, 200, {actions});
    assert(!b.submit.disabled);
    assert.equal(b.tool.children[3].textContent, '<Fabric v2> (fabric v2)');
    for (const r of requests) {
        assert(r.url.startsWith('/lib/exe/ajax.php?call=plugin_petar&action='));
        assert(r.options.signal, 'finite timeout signal');
        assert.equal(r.options.credentials, 'same-origin');
    }
}
async function runJobs(locale) {
    const document = parse(rendered[locale].html), requests = [];
    document.createElement = tag => new Element(tag);
    const context = {
        document, URLSearchParams, AbortController, DOKU_BASE: '/wiki/',
        setTimeout: () => 1, clearTimeout: () => {},
        fetch(url, options) {
            return new Promise((resolve, reject) => requests.push({url, options, resolve, reject}));
        }
    };
    vm.runInNewContext(source, context);
    document.fire('DOMContentLoaded');
    const jobs = requests.filter(r => r.url.includes('action=jobs'));
    assert.equal(jobs.length, 2, 'each journal requests its first page');
    const parts = w => ({
        status: w.querySelector('.petar-jobs-status'), filter: w.querySelector('select'),
        list: w.querySelector('tbody'), more: w.querySelector('.petar-jobs-more'),
        refresh: w.querySelector('.petar-jobs-refresh'),
        card: w.querySelector('.petar-job-card'), close: w.querySelector('.petar-job-close'),
        result: w.querySelector('pre'), cardStatus: w.querySelector('.petar-job-status')
    });
    const [a, b] = document.querySelectorAll('[data-petar-jobs]').map(parts);
    const s = rendered[locale].strings;
    const checkURL = (r, status, cursor) => {
        const expected = '/wiki/lib/exe/ajax.php?call=plugin_petar&action=jobs' +
            (status ? '&status=' + status : '') + (cursor ? '&cursor=' + cursor : '');
        assert.equal(r.url, expected);
        assert.equal(r.options.method, 'GET');
        assert.equal(r.options.credentials, 'same-origin');
        assert(!r.options.body);
    };
    const row = tool => ({job_id: 'job', process_id: 'process', tool, status: 'completed',
        created_at: 'created', updated_at: 'updated', payload: 'SECRET_PAYLOAD'});
    const answer = async (req, rows, cursor = null, code = 200) => {
        req.resolve({ok: code === 200, status: code, headers: {get: () => null},
            json: async () => ({jobs: rows, next_cursor: cursor, error: 'SECRET_ERROR'})});
        await flush();
    };
    const cursor = '019b2c5d-1234-7000-8000-000000000001';
    checkURL(jobs[0]); checkURL(jobs[1]);
    assert.equal(a.status.textContent, s.jobs_loading);
    assert(a.more.hidden);
    const statuses = ['pending', 'dispatched', 'in_progress', 'completed', 'failed'];
    assert.deepEqual(a.filter.querySelectorAll('option').map(o => [o.value, o.textContent]),
        [['', s.jobs_all], ...statuses.map(v => [v, s['jobs_' + v]])]);
    assert.equal(a.more.textContent, s.jobs_more);
    assert.equal(a.refresh.textContent, s.jobs_refresh);
    await answer(jobs[0], [row('<img src=x onerror=evil()>')], cursor);
    await answer(jobs[1], []);
    assert.equal(b.status.textContent, s.jobs_empty);
    assert(a.list.textContent.includes('<img src=x onerror=evil()>'));
    assert(!a.list.querySelector('img'));
    assert(!a.list.textContent.includes('SECRET'));
    assert(!a.more.hidden);
    let count = requests.length;
    a.more.fire('click'); a.more.fire('click');
    assert.equal(requests.length, count + 1);
    assert(a.more.disabled);
    checkURL(requests.at(-1), '', cursor);
    await answer(requests.at(-1), [], null, 502);
    assert.equal(a.status.textContent, s.jobs_error);
    assert.equal(a.list.children.length, 1);
    assert(!a.more.hidden && !a.more.disabled);
    a.more.fire('click');
    checkURL(requests.at(-1), '', cursor);
    await answer(requests.at(-1), [row('next tool')], cursor);
    assert.equal(a.list.children.length, 2);
    a.more.fire('click');
    await answer(requests.at(-1), []);
    assert.equal(a.list.children.length, 2);
    assert(a.more.hidden);
    for (const status of statuses) {
        a.filter.value = status; a.filter.fire('change');
        checkURL(requests.at(-1), status);
        assert.equal(a.list.children.length, 0);
        await answer(requests.at(-1), [row(status)], cursor);
    }
    a.more.fire('click');
    const staleMore = requests.at(-1);
    checkURL(staleMore, 'failed', cursor);
    a.refresh.fire('click');
    const staleRefresh = requests.at(-1);
    checkURL(staleRefresh, 'failed');
    a.filter.value = ''; a.filter.fire('change');
    checkURL(requests.at(-1));
    const current = requests.at(-1);
    staleRefresh.reject(Error('stale'));
    await flush();
    assert.equal(a.status.textContent, s.jobs_loading);
    assert(a.more.hidden);
    await answer(current, [row('fresh')]);
    await answer(staleMore, [row('stale')], cursor);
    assert.equal(a.list.children.length, 1);
    assert(a.list.textContent.includes('fresh') && !a.list.textContent.includes('stale'));
    assert(a.more.hidden);
    assert.equal(b.status.textContent, s.jobs_empty, 'other widget untouched');
    assert.equal(b.list.children.length, 0);
    assert.equal(requests.filter(r => r.url.includes('action=readyz')).length, 2);

    assert(a.card.hidden && b.card.hidden);
    const first = {...row('first tool'), job_id: cursor};
    const second = {...row('second tool'), job_id: '019b2c5d-1234-7000-8000-000000000002'};
    const failed = {...row('failed tool'), status: 'failed', job_id: '019b2c5d-1234-7000-8000-000000000003'};
    const active = {...row('active tool'), status: 'in_progress'};
    a.filter.value = 'completed'; a.filter.fire('change');
    await answer(requests.at(-1), [first, second, failed, active], cursor);
    b.refresh.fire('click');
    await answer(requests.at(-1), [first], cursor);
    const buttons = a.list.querySelectorAll('button');
    assert.equal(buttons.length, 3, 'only terminal jobs have buttons');
    const [openA, openB, openFailed] = buttons;
    const open = (button, job) => {
        button.fire('click');
        const req = requests.at(-1);
        assert.equal(req.url, '/wiki/lib/exe/ajax.php?call=plugin_petar&action=job_result&job_id=' + job.job_id);
        assert.equal(req.options.method, 'GET');
        assert.equal(req.options.credentials, 'same-origin');
        assert(!req.options.body);
        return req;
    };
    const replyCard = async (req, status, data) => {
        req.resolve({ok: status >= 200 && status < 300, status, headers: {get: () => null}, json: async () => data});
        await flush();
    };
    const publication = (job, marker) => ({job_id: job.job_id, result: {type: 'echo', version: 1, data: {marker}}});
    const pendingA = open(openA, first), pendingB = open(openB, second);
    assert(!a.card.hidden && b.card.hidden);
    assert.equal(a.cardStatus.textContent, s.jobs_result_loading);
    const bRequest = open(b.list.querySelector('button'), first);
    const marker = '<img src=x onerror=evil()>';
    await replyCard(pendingB, 200, publication(second, marker));
    await replyCard(pendingA, 200, publication(first, 'late A'));
    assert.deepEqual(JSON.parse(a.result.textContent), publication(second, marker).result);
    assert(!a.result.querySelector('img'));
    for (const [key, value] of Object.entries(second)) {
        if (key === 'payload') continue;
        assert.equal(a.card.querySelector(`[data-job-field="${key}"]`).textContent,
            key === 'status' ? s.jobs_completed : value);
    }
    const failedRequest = open(openFailed, failed);
    assert(a.result.hidden && a.result.textContent === '', 'new selection clears old result immediately');
    await replyCard(failedRequest, 409, {error: 'job_failed', diagnostic: 'SECRET'});
    assert.equal(a.cardStatus.textContent, s.jobs_result_failed);
    await replyCard(bRequest, 200, publication(first, 'independent'));
    assert(b.result.textContent.includes('independent'));
    for (const [code, body, text] of [
        [202, {status: 'in_progress', result: 'SECRET'}, s.jobs_result_not_ready],
        [404, {error: 'job_not_found'}, s.jobs_result_missing],
        [410, {error: 'result_unavailable'}, s.jobs_result_unavailable],
        [502, {error: 'SECRET'}, s.jobs_result_error],
        [200, publication(first, 'mismatched job'), s.jobs_result_error]
    ]) {
        assert(text && text !== s.jobs_result_failed);
        const req = open(openB, second);
        await replyCard(req, code, body);
        assert.equal(a.cardStatus.textContent, text);
        assert(a.result.hidden && !a.card.textContent.includes('SECRET'));
    }
    const assertClosed = () => assert(a.card.hidden && a.result.hidden && a.result.textContent === '');
    const closed = open(openB, second);
    count = requests.length;
    a.close.fire('click');
    assert.equal(requests.length, count, 'close must not fetch');
    assert.equal(a.filter.value, 'completed');
    assert.equal(a.list.children.length, 4);
    assert(!a.more.hidden && openB.focused);
    await replyCard(closed, 200, publication(second, 'closed'));
    assertClosed();
    for (const [control, event, buttonIndex] of [[a.refresh, 'click', 1], [a.filter, 'change', 0]]) {
        const late = open(a.list.querySelectorAll('button')[buttonIndex], second);
        control.fire(event);
        await replyCard(late, 200, publication(second, 'invalidated'));
        assertClosed();
        await answer(requests.at(-1), [second], cursor);
    }
    assert(!b.card.hidden && b.result.textContent.includes('independent'));
    for (const role of ['anonymous', 'ordinary']) {
        const denied = parse(rendered[role].html);
        assert(!denied.querySelector('[data-petar-jobs]'));
        count = requests.length;
        vm.runInNewContext(source, {...context, document: denied});
        denied.fire('DOMContentLoaded');
        assert.equal(requests.length, count, 'ACL denial must not fetch');
    }
}
(async () => {
    const scenario = process.argv[3] === 'jobs' ? runJobs : run;
    await scenario('en'); await scenario('ru');
    console.log('UI behavior passed (EN/RU)');
})().catch(e => { console.error(e); process.exitCode = 1; });
