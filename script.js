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
        var actions = [];
        var submitting = false;
        var generation = 0;

        function updateState() {
            var state = submitting ? 'submitting' : readiness;
            widget.dataset.state = state;
            submit.disabled = submitting || readiness !== 'ready' || !selected();
            tool.disabled = submitting || readiness !== 'ready';
            form.setAttribute('aria-busy', submitting ? 'true' : 'false');
            status.textContent = strings[submitting ? 'status_busy' :
                readiness === 'ready' ? 'status_ready' :
                readiness === 'checking' ? 'status_checking' :
                readiness === 'unavailable' ? 'err_backend' : readiness];
        }

        function selected() {
            return tool.value === '' ? null : actions[Number(tool.value)];
        }

        function special(pair) {
            if (!pair || pair.version !== 1) return 'json';
            switch (pair.action) {
                case 'yt-dlp': return 'url';
                case 'fabric': return 'text';
                case 'echo-test': return 'message';
                default: return 'json';
            }
        }

        function updateTool() {
            var kind = special(selected());
            label.textContent = strings['label_' + kind];
            hint.textContent = strings['hint_' + kind];
            message.setAttribute('placeholder', strings['hint_' + kind]);
            patternRow.hidden = kind !== 'text';
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
            var previous = selected();
            readiness = 'checking';
            updateState();
            try {
                var response = await request('readyz');
                if (current !== generation) return;
                if (!response.ok || !response.data || response.data.status !== 'ready') {
                    readiness = 'unavailable';
                    updateState();
                    return;
                }
                readiness = 'catalog_loading';
                updateState();
                var catalog = await request('actions');
                if (current !== generation) return;
                if (!catalog.ok || !catalog.data || !Array.isArray(catalog.data.actions) ||
                    !catalog.data.actions.every(function (pair) {
                        return pair && typeof pair.action === 'string' && pair.action.trim() !== '' &&
                            Number.isSafeInteger(pair.version) && pair.version > 0 && typeof pair.title === 'string';
                    })) throw new Error('Invalid action catalog');
                actions = catalog.data.actions;
                tool.textContent = '';
                actions.forEach(function (pair, index) {
                    var option = document.createElement('option');
                    option.value = String(index);
                    option.textContent = pair.title + ' (' + pair.action + ' v' + pair.version + ')';
                    tool.appendChild(option);
                });
                var index = previous ? actions.findIndex(function (pair) {
                    return pair.action === previous.action && pair.version === previous.version;
                }) : -1;
                tool.value = actions.length ? String(index < 0 ? 0 : index) : '';
                readiness = actions.length ? 'ready' : 'catalog_empty';
                updateTool();
            } catch (_) {
                if (current !== generation) return;
                readiness = readiness === 'catalog_loading' ? 'catalog_error' : 'unavailable';
            }
            updateState();
        }

        form.addEventListener('submit', async function (event) {
            event.preventDefault();
            var pair = selected();
            if (submitting || readiness !== 'ready' || !pair) return;
            var kind = special(pair);
            var dataJson;
            result.removeAttribute('data-input-id');
            result.removeAttribute('data-process-id');
            if (kind === 'json') {
                dataJson = message.value;
                try { JSON.parse(dataJson); } catch (_) {
                    showResult(strings.err_invalid_json, 'error');
                    return;
                }
            } else {
                var data = {};
                data[kind] = message.value;
                if (kind === 'text') data.pattern = pattern.value;
                dataJson = JSON.stringify(data);
            }
            submitting = true;
            updateState();
            showResult(strings.status_busy, 'loading');
            var body = new URLSearchParams();
            body.append('sectok', form.querySelector('[name="sectok"]').value);
            body.append('action_name', pair.action);
            body.append('version', String(pair.version));
            body.append('data', dataJson);
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

    var resultRenderers = [
        {type: 'echo-test', version: 1, render: function (data) {
            return data && typeof data.echo === 'string' ? data.echo : null;
        }}
    ];

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
        var card = widget.querySelector('.petar-job-card');
        var close = card.querySelector('.petar-job-close');
        var cardStatus = card.querySelector('.petar-job-status');
        var publication = card.querySelector('.petar-job-public');
        var special = card.querySelector('.petar-job-special');
        var jsonToggle = card.querySelector('.petar-job-json-toggle');
        var cardFields = card.querySelectorAll('[data-job-field]');
        var selection = 0;
        var opener = null;
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

        function jobText(job, key) {
            return key === 'status' ? strings[job.status] : job[key];
        }

        function setJSONExpanded(expanded) {
            publication.hidden = !expanded;
            jsonToggle.setAttribute('aria-expanded', expanded ? 'true' : 'false');
            jsonToggle.textContent = expanded ? strings.result_hide_json : strings.result_show_json;
        }

        function closeCard(restoreFocus) {
            ++selection;
            card.hidden = true;
            cardStatus.textContent = '';
            publication.textContent = '';
            setJSONExpanded(false);
            special.textContent = '';
            special.hidden = true;
            jsonToggle.hidden = true;
            cardFields.forEach(function (field) { field.textContent = ''; });
            if (opener) {
                opener.setAttribute('aria-expanded', 'false');
                if (restoreFocus) opener.focus();
                opener = null;
            }
        }

        async function openCard(job, button) {
            closeCard(false);
            var current = selection;
            opener = button;
            button.setAttribute('aria-expanded', 'true');
            cardFields.forEach(function (field) {
                field.textContent = jobText(job, field.dataset.jobField);
            });
            card.hidden = false;
            cardStatus.textContent = strings.result_loading;
            close.focus();
            try {
                var response = await request('job_result', null, new URLSearchParams({job_id: job.job_id}));
                if (current !== selection) return;
                var data = response.data || {};
                if (response.status === 200 && data.job_id === job.job_id && data.result) {
                    var renderer = resultRenderers.find(function (entry) {
                        return entry.type === data.result.type && entry.version === data.result.version;
                    });
                    var text = renderer ? renderer.render(data.result.data) : null;
                    publication.textContent = JSON.stringify(data.result, null, 2);
                    if (text === null) {
                        publication.hidden = false;
                    } else {
                        special.textContent = text;
                        special.hidden = false;
                        jsonToggle.hidden = false;
                    }
                    cardStatus.textContent = '';
                } else if (response.status === 202 && ['pending', 'dispatched', 'in_progress'].includes(data.status)) {
                    cardStatus.textContent = strings.result_not_ready;
                } else if (response.status === 409 && data.error === 'job_failed') {
                    cardStatus.textContent = strings.result_failed;
                } else if (response.status === 404 && data.error === 'job_not_found') {
                    cardStatus.textContent = strings.result_missing;
                } else if (response.status === 410 && data.error === 'result_unavailable') {
                    cardStatus.textContent = strings.result_unavailable;
                } else {
                    cardStatus.textContent = strings.result_error;
                }
            } catch (_) {
                if (current === selection) cardStatus.textContent = strings.result_error;
            }
        }

        function makeRow(job) {
            var row = document.createElement('tr');
            ['job_id', 'process_id', 'tool', 'status', 'created_at', 'updated_at'].forEach(function (key) {
                var cell = document.createElement('td');
                cell.textContent = jobText(job, key);
                row.appendChild(cell);
            });
            var action = document.createElement('td');
            if (job.status === 'completed' || job.status === 'failed') {
                var button = document.createElement('button');
                button.setAttribute('type', 'button');
                button.setAttribute('class', 'button');
                button.setAttribute('aria-label', strings.result_open + ': ' + job.job_id);
                button.setAttribute('aria-controls', card.getAttribute('id'));
                button.setAttribute('aria-expanded', 'false');
                button.textContent = strings.result_open;
                button.addEventListener('click', function () { openCard(job, button); });
                action.appendChild(button);
            }
            row.appendChild(action);
            return row;
        }

        async function load(append) {
            if (append && (busy || cursor === null)) return;
            var current = ++generation;
            if (!append) {
                closeCard(false);
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
        close.addEventListener('click', function () { closeCard(true); });
        jsonToggle.addEventListener('click', function () {
            if (!jsonToggle.hidden) setJSONExpanded(publication.hidden);
        });
        refresh.addEventListener('click', function () { load(false); });
        more.addEventListener('click', function () { load(true); });
        load(false);
    }

    document.addEventListener('DOMContentLoaded', function () {
        document.querySelectorAll('.plugin_petar').forEach(initialize);
        document.querySelectorAll('[data-petar-jobs]').forEach(initializeJobs);
    });
})();
