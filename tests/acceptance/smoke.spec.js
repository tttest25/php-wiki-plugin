'use strict';

const { test, expect } = require('@playwright/test');

test('ai sees ready form and seeded jobs on one page', async ({ page, baseURL }) => {
    const ajax = { readyz: [], jobs: [] };
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
        }
        if (response.status() >= 400) issues.push(`HTTP ${response.status()}`);
    });
    // Report categories, not arbitrary browser messages (which could contain auth data).
    page.on('pageerror', () => issues.push('pageerror'));
    page.on('console', message => {
        if (message.type() === 'error') issues.push('console error');
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
        await expect(rows, 'exactly one seeded job row').toHaveCount(1);
        await expect(rows).toBeVisible();
        await expect(rows.locator('td'), 'seeded job fields').toHaveText([
            '019b2c5d-1234-7000-8000-000000000001',
            '019b2c5d-1234-7000-8000-000000000002',
            'echo-test',
            'Completed',
            '2026-01-01T00:00:00Z',
            '2026-01-01T00:01:00Z',
        ]);
        expect(issues, 'browser requests, console and page errors').toEqual([]);
    } finally {
        console.log('Browser checks: %s', JSON.stringify({ ajax, issues }));
    }
});
