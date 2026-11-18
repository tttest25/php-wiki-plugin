'use strict';

const http = require('node:http');

http.createServer((request, response) => {
    const routes = {
        '/readyz': { status: 'ready' },
        '/api/v1/jobs?limit=21': [{
            id: '019b2c5d-1234-7000-8000-000000000001',
            process_id: '019b2c5d-1234-7000-8000-000000000002',
            tool_name: 'echo-test',
            status: 'completed',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:01:00Z',
        }],
    };
    const valid = request.method === 'GET' && Object.hasOwn(routes, request.url);
    response.writeHead(valid ? 200 : 404, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(valid ? routes[request.url] : { error: 'unexpected fixture request' }));
}).listen(8080, '0.0.0.0');
