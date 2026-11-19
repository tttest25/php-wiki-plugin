'use strict';

const { test, expect } = require('@playwright/test');

test('ai sees ready form, journal and selected job results on one page', async ({ page, baseURL }) => {
    const ajax = { readyz: [], jobs: [], job_result: [] };
    const resultRequests = [];
    const issues = [];
    page.on('request', request => {
        const origin = new URL(request.url()).origin;
        if (origin !== baseURL) issues.push('browser request outside wiki origin');
    });
    page.on('response', response => {
        const url = new URL(response.url());
        const action = url.searchParams.get('action');
        if (url.origin === baseURL && url.pathname === '/lib/exe/ajax.php' &&
            url.searchParams.get('call') === 'plugin_petar' && Object.hasOwn(ajax, action)) {
            ajax[action].push(`${response.request().method()} ${response.status()}`);
            if (action === 'job_result') {
                resultRequests.push({ query: [...url.searchParams.keys()], job: url.searchParams.get('job_id'),
                    cache: response.headers()['cache-control'], id: response.headers()['x-request-id'] });
                if (response.status() === 409) return; // The failed-job branch is expected.
                if (response.status() === 404) issues.push('job result route/fixture not found');
            }
        }
        if (response.status() >= 400) issues.push(`HTTP ${response.status()}`);
    });
    // Report categories, not arbitrary browser messages (which could contain auth data).
    page.on('pageerror', () => issues.push('pageerror'));
    page.on('console', message => {
        if (message.type() !== 'error') return;
        const url = new URL(message.location().url || baseURL);
        // Chromium logs expected HTTP 409 asset errors; verify the exact response trace below.
        if (url.pathname === '/lib/exe/ajax.php' && url.searchParams.get('action') === 'job_result' &&
            message.text().includes('409')) return;
        issues.push('console error');
    });

    try {
        await test.step('log in to DokuWiki as the fixture ai user', async () => {
            const password = process.env.PETAR_ACCEPTANCE_PASSWORD;
            if (!password) throw new Error('missing fixture password');
            await page.goto('/doku.php?id=petar_smoke&do=login');
            const login = page.locator('#dw__login');
            await login.locator('[name=u]').fill('petar_smoke');
            try {
                await login.locator('[name=p]').fill(password);
            } catch {
                throw new Error('could not fill DokuWiki password field');
            }
            await login.getByRole('button', { name: 'Log In', exact: true }).click();
        });

        const form = page.locator('.plugin_petar form');
        const journal = page.locator('[data-petar-jobs]');
        await expect(form, 'one form after login').toHaveCount(1);
        await expect(journal, 'one journal on the same page').toHaveCount(1);
        await expect(journal).toBeVisible();
        await expect(page.locator('.plugin_petar .petar-status')).toHaveText('Ready');
        const submit = form.getByRole('button', { name: 'Submit', exact: true });
        await expect(submit).toBeVisible();
        await expect(submit).toBeEnabled();

        for (const action of ['readyz', 'jobs']) {
            await expect.poll(() => ajax[action], { message: `wiki AJAX ${action}` }).toEqual(['GET 200']);
        }
        const rows = journal.locator('tbody tr');
        const [first, second, failed, processId] = [1, 2, 3, 9].map(n => `019b2c5d-1234-7000-8000-00000000000${n}`);
        await expect(rows, 'three seeded jobs').toHaveCount(3);
        await expect(rows.first()).toBeVisible();
        await expect(rows.first().locator('td'), 'seeded job fields').toHaveText([
            first, processId,
            'echo-test',
            'Completed',
            '2026-01-01T00:00:00Z',
            '2026-01-01T00:01:00Z',
            'View result',
        ]);
        const filter = journal.getByRole('combobox', { name: 'Status' });
        const card = journal.getByRole('article', { name: 'Job result' });
        const open = id => journal.getByRole('button', { name: `View result: ${id}` });
        await expect(card).toBeHidden();
        await filter.selectOption('completed');
        await expect(rows).toHaveCount(2);
        await open(second).click();
        await expect(card).toBeVisible();
        await expect(card.locator('dd')).toHaveText([
            second, processId, 'echo-test', 'Completed', '2026-01-01T00:02:00Z', '2026-01-01T00:03:00Z',
        ]);
        await expect(card.locator('pre')).toContainText('second-job-public');
        await expect(card).not.toContainText('first-job-public');
        await card.getByRole('button', { name: 'Close result' }).click();
        await expect(card).toBeHidden();
        await expect(open(second)).toBeFocused();
        await expect(filter).toHaveValue('completed');
        await expect(rows).toHaveCount(2);
        await expect(open(first)).toBeVisible();
        await expect(open(second)).toBeVisible();
        await open(first).click();
        await expect(card.locator('pre')).toContainText('first-job-public');
        await expect(card).not.toContainText('second-job-public');
        await filter.selectOption('failed');
        await expect(card).toBeHidden();
        await expect(rows).toHaveCount(1);
        await open(failed).click();
        await expect(card.getByRole('status')).toHaveText('This job failed. No public result is available.');
        await expect(card.locator('pre')).toBeHidden();
        expect(ajax.jobs).toEqual(['GET 200', 'GET 200', 'GET 200']);
        expect(ajax.job_result).toEqual(['GET 200', 'GET 200', 'GET 409']);
        expect(resultRequests.map(r => r.job)).toEqual([second, first, failed]);
        for (const r of resultRequests) {
            expect(r.query).toEqual(['call', 'action', 'job_id']);
            expect(r.cache).toBe('no-store');
            expect(r.id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
        }
        expect(issues, 'browser requests, console and page errors').toEqual([]);
    } finally {
        console.log('Browser checks: %s', JSON.stringify({ ajax, issues }));
    }
});
