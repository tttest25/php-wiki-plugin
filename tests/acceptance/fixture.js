'use strict';

const http = require('node:http');

const [first, second, failed, processId] = [1, 2, 3, 9].map(n => `019b2c5d-1234-7000-8000-00000000000${n}`);
const jobs = [first, second, failed].map((id, index) => ({
    id,
    process_id: processId,
    tool_name: 'echo-test',
    status: id === failed ? 'failed' : 'completed',
    created_at: `2026-01-01T00:0${2 * index}:00Z`,
    updated_at: `2026-01-01T00:0${2 * index + 1}:00Z`,
}));
const resultPath = job => `/api/v1/jobs/${job}/result`;
const publication = marker => ({ type: 'echo', version: 1, data: { marker } });
const routes = new Map([
    ['/readyz', [200, { status: 'ready' }]],
    ['/api/v1/jobs?limit=21', [200, jobs]],
    ['/api/v1/jobs?limit=21&status=completed', [200, jobs.slice(0, 2)]],
    ['/api/v1/jobs?limit=21&status=failed', [200, jobs.slice(2)]],
    [resultPath(first), [200, publication('first-job-public')]],
    [resultPath(second), [200, publication('second-job-public')]],
    [resultPath(failed), [409, { error: 'job_failed' }]],
]);
http.createServer((request, response) => {
    const route = request.method === 'GET' && routes.get(request.url);
    if (!route) console.error('Unexpected fixture route: %s %s', request.method, request.url);
    const [status, body] = route || [404, { error: 'unexpected fixture request' }];
    response.writeHead(status, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(body));
}).listen(8080, '0.0.0.0');
