/**
 * Each widget owns its readiness, request generation and submission state.
 * A browser timeout stops waiting; it cannot cancel an already-created job.
 */
(function () {
    'use strict';

    function apiUrl(action) {
        var base = typeof DOKU_BASE !== 'undefined' ? DOKU_BASE : '/';
        return base + 'lib/exe/ajax.php?call=plugin_petar&action=' + encodeURIComponent(action);
    }

    async function request(action, body, query) {
        var controller = new AbortController();
        var timer = setTimeout(function () { controller.abort(); }, 15000);
        try {
            var options = {
                method: body ? 'POST' : 'GET',
                credentials: 'same-origin',
                headers: { 'Accept': 'application/json' },
                signal: controller.signal
            };
            if (body) {
                options.headers['Content-Type'] = 'application/x-www-form-urlencoded';
                options.body = body.toString();
            }
            var url = apiUrl(action);
            if (query && query.toString()) url += '&' + query.toString();
            var response = await fetch(url, options);
            var data = await response.json();
            return {
                ok: response.ok, status: response.status, data: data,
                requestId: response.headers.get('X-Request-ID')
            };
        } finally {
            clearTimeout(timer);
        }
    }

    function initialize(widget) {
        var form = widget.querySelector('form');
        if (!form || widget.dataset.initialized) return;
        widget.dataset.initialized = 'true';
        var strings = JSON.parse(widget.dataset.ui);
        var submit = form.querySelector('button[type="submit"]');
        var retry = form.querySelector('.petar-retry');
        var tool = form.querySelector('[name="tool"]');
        var message = form.querySelector('[name="message"]');
        var pattern = form.querySelector('[name="pattern"]');
        var patternRow = form.querySelector('.petar-pattern');
        var label = form.querySelector('.petar-message-label');
        var hint = form.querySelector('.petar-hint');
        var status = widget.querySelector('.petar-status');
        var result = widget.querySelector('.petar-result');
        var readiness = 'checking';
        var submitting = false;
        var generation = 0;

        function updateState() {
            var state = submitting ? 'submitting' : readiness;
            widget.dataset.state = state;
            submit.disabled = submitting || readiness !== 'ready';
            form.setAttribute('aria-busy', submitting ? 'true' : 'false');
            status.textContent = strings[submitting ? 'status_busy' :
                readiness === 'ready' ? 'status_ready' :
                readiness === 'checking' ? 'status_checking' : 'err_backend'];
        }

        function updateTool() {
            var kind = tool.value === 'yt-dlp' ? 'url' : tool.value === 'fabric' ? 'text' : 'message';
            label.textContent = strings['label_' + kind];
            hint.textContent = strings['hint_' + kind];
            message.setAttribute('placeholder', strings['hint_' + kind]);
            patternRow.hidden = tool.value !== 'fabric';
            pattern.disabled = patternRow.hidden;
            pattern.required = !patternRow.hidden;
        }

        function showResult(text, css, requestId, data) {
            result.hidden = false;
            result.className = 'petar-result ' + css;
            result.textContent = '';
            var p = document.createElement('p');
            p.textContent = text;
            result.appendChild(p);
            if (data) {
                var dl = document.createElement('dl');
                ['status', 'input_id', 'process_id'].forEach(function (key) {
                    var dt = document.createElement('dt');
                    var dd = document.createElement('dd');
                    dt.textContent = strings['label_' + key];
                    dd.textContent = data[key];
                    dl.appendChild(dt);
                    dl.appendChild(dd);
                });
                result.appendChild(dl);
            }
            if (requestId) {
                var trace = document.createElement('div');
                trace.className = 'petar-request-id';
                trace.textContent = strings.label_request_id + ': ' + requestId;
                result.appendChild(trace);
            }
        }

        async function checkReadiness() {
            var current = ++generation;
            readiness = 'checking';
            updateState();
            try {
                var response = await request('readyz');
                if (current !== generation) return;
                readiness = response.ok && response.data && response.data.status === 'ready' ? 'ready' : 'unavailable';
            } catch (_) {
                if (current !== generation) return;
                readiness = 'unavailable';
            }
            updateState();
        }

        form.addEventListener('submit', async function (event) {
            event.preventDefault();
            if (submitting || readiness !== 'ready') return;
            submitting = true;
            updateState();
            result.removeAttribute('data-input-id');
            result.removeAttribute('data-process-id');
            showResult(strings.status_busy, 'loading');
            var body = new URLSearchParams();
            body.append('sectok', form.querySelector('[name="sectok"]').value);
            body.append('tool', tool.value);
            body.append('message', message.value);
            body.append('action', 'submit');
            if (tool.value === 'fabric') body.append('pattern', pattern.value);
            try {
                var response = await request('submit', body);
                var data = response.data;
                if (!response.ok) {
                    if (response.status >= 500) {
                        // Invalidate an older readiness check still in flight.
                        ++generation;
                        readiness = 'unavailable';
                    }
                    var errorKey = response.status >= 500 ? 'err_submit_uncertain' :
                        response.status === 400 ? 'err_submit_rejected' : 'err_request';
                    showResult(strings[errorKey], 'error', response.requestId);
                } else if (!data || data.status !== 'new' ||
                    typeof data.input_id !== 'string' || typeof data.process_id !== 'string') {
                    throw new Error('Invalid submission response');
                } else {
                    result.setAttribute('data-input-id', data.input_id);
                    result.setAttribute('data-process-id', data.process_id);
                    showResult(strings.status_accepted, 'success', response.requestId, data);
                }
            } catch (_) {
                ++generation;
                readiness = 'unavailable';
                showResult(strings.err_submit_uncertain, 'error');
            } finally {
                submitting = false;
                updateState();
            }
        });
        tool.addEventListener('change', updateTool);
        retry.addEventListener('click', checkReadiness);
        updateTool();
        checkReadiness();
    }

    function initializeJobs(widget) {
        if (widget.dataset.initialized) return;
        widget.dataset.initialized = 'true';
        var strings = JSON.parse(widget.dataset.ui);
        var filter = widget.querySelector('select');
        var refresh = widget.querySelector('.petar-jobs-refresh');
        var more = widget.querySelector('.petar-jobs-more');
        var table = widget.querySelector('.petar-jobs-table');
        var rows = widget.querySelector('tbody');
        var status = widget.querySelector('.petar-jobs-status');
        var generation = 0;
        var cursor = null;
        var busy = false;
        var rowCount = 0;

        function updateControls() {
            more.hidden = cursor === null;
            more.disabled = busy;
            table.hidden = rowCount === 0;
            table.setAttribute('aria-busy', busy ? 'true' : 'false');
        }

        function makeRow(job) {
            var row = document.createElement('tr');
            ['job_id', 'process_id', 'tool', 'status', 'created_at', 'updated_at'].forEach(function (key) {
                var cell = document.createElement('td');
                cell.textContent = key === 'status' ? strings[job.status] : job[key];
                row.appendChild(cell);
            });
            return row;
        }

        async function load(append) {
            if (append && (busy || cursor === null)) return;
            var current = ++generation;
            if (!append) {
                cursor = null;
                rowCount = 0;
                rows.textContent = '';
            }
            var query = new URLSearchParams();
            if (filter.value) query.set('status', filter.value);
            if (cursor !== null) query.set('cursor', cursor);
            busy = true;
            status.textContent = strings.loading;
            updateControls();
            try {
                var response = await request('jobs', null, query);
                if (current !== generation) return;
                var data = response.data;
                if (!response.ok || !data || !Array.isArray(data.jobs) ||
                    (data.next_cursor !== null && typeof data.next_cursor !== 'string')) {
                    throw new Error('Invalid journal response');
                }
                var page = data.jobs.map(makeRow);
                page.forEach(function (row) { rows.appendChild(row); });
                rowCount += page.length;
                cursor = data.next_cursor;
                status.textContent = rowCount === 0 ? strings.empty : '';
            } catch (_) {
                if (current === generation) status.textContent = strings.error;
            } finally {
                if (current === generation) {
                    busy = false;
                    updateControls();
                }
            }
        }

        filter.addEventListener('change', function () { load(false); });
        refresh.addEventListener('click', function () { load(false); });
        more.addEventListener('click', function () { load(true); });
        load(false);
    }

    document.addEventListener('DOMContentLoaded', function () {
        document.querySelectorAll('.plugin_petar').forEach(initialize);
        document.querySelectorAll('[data-petar-jobs]').forEach(initializeJobs);
    });
})();
