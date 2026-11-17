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

    async function request(action, body) {
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
            var response = await fetch(apiUrl(action), options);
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
                    showResult(response.status >= 500 ? strings.err_submit_uncertain : strings.err_request,
                        'error', response.requestId);
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

    document.addEventListener('DOMContentLoaded', function () {
        document.querySelectorAll('.plugin_petar').forEach(initialize);
    });
})();
