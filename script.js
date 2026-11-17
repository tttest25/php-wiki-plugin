/**
 * Petar form submission through the DokuWiki BFF endpoint.
 */
(function () {
    'use strict';

    function apiUrl(action) {
        var base = typeof DOKU_BASE !== 'undefined' ? DOKU_BASE : '/';
        return base + 'lib/exe/ajax.php?call=plugin_petar&action=' + encodeURIComponent(action);
    }

    async function request(action, body) {
        var options = {
            method: body ? 'POST' : 'GET',
            credentials: 'same-origin',
            headers: { 'Accept': 'application/json' }
        };
        if (body) {
            options.headers['Content-Type'] = 'application/x-www-form-urlencoded';
            options.body = body.toString();
        }
        var response = await fetch(apiUrl(action), options);
        return {
            ok: response.ok,
            data: await response.json(),
            requestId: response.headers.get('X-Request-ID')
        };
    }

    document.addEventListener('DOMContentLoaded', function () {
        var form = document.getElementById('petar-form');
        var result = document.getElementById('petar-result');
        if (!form || !result) return;
        var submit = form.querySelector('button[type="submit"]');
        var ready = false;
        var submitting = false;

        function showResult(text, state, requestId, data) {
            result.hidden = false;
            result.className = 'petar-result ' + state;
            result.textContent = '';
            var message = document.createElement('p');
            message.textContent = text;
            result.appendChild(message);
            if (data) {
                var details = document.createElement('dl');
                ['status', 'input_id', 'process_id'].forEach(function (key) {
                    var label = document.createElement('dt');
                    var value = document.createElement('dd');
                    label.textContent = key;
                    value.textContent = data[key];
                    details.appendChild(label);
                    details.appendChild(value);
                });
                result.appendChild(details);
            }
            if (requestId) {
                var trace = document.createElement('div');
                trace.className = 'petar-request-id';
                trace.textContent = 'X-Request-ID: ' + requestId;
                result.appendChild(trace);
            }
        }

        async function checkReadiness() {
            submit.disabled = true;
            showResult('Проверка готовности сервиса…', 'loading');
            try {
                var response = await request('readyz');
                ready = response.ok && response.data && response.data.status === 'ready';
                showResult(ready ? 'Сервис готов.' : 'Сервис недоступен.',
                    ready ? 'success' : 'error', response.requestId);
            } catch (_) {
                showResult('Не удалось проверить готовность сервиса.', 'error');
            }
            submit.disabled = !ready;
        }

        form.addEventListener('submit', async function (event) {
            event.preventDefault();
            if (!ready || submitting) return;
            submitting = true;
            submit.disabled = true;
            result.removeAttribute('data-input-id');
            result.removeAttribute('data-process-id');
            showResult('Отправка…', 'loading');
            var body = new URLSearchParams();
            body.append('call', 'plugin_petar');
            body.append('action', 'submit');
            body.append('sectok', form.querySelector('[name="sectok"]').value);
            body.append('tool', form.querySelector('[name="tool"]').value);
            body.append('message', form.querySelector('[name="message"]').value);
            try {
                var response = await request('submit', body);
                var data = response.data;
                if (!response.ok || !data || typeof data.input_id !== 'string' ||
                    typeof data.process_id !== 'string' || typeof data.status !== 'string') {
                    showResult('Не удалось отправить задачу.', 'error', response.requestId);
                } else {
                    result.setAttribute('data-input-id', data.input_id);
                    result.setAttribute('data-process-id', data.process_id);
                    showResult('Задача принята.', 'success', response.requestId, data);
                }
            } catch (_) {
                showResult('Ошибка соединения с сервисом.', 'error');
            } finally {
                submitting = false;
                submit.disabled = !ready;
            }
        });

        checkReadiness();
    });
})();
