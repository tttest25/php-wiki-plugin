'use strict';

const { test, expect } = require('@playwright/test');

async function loginAsAI(page) {
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
}

test('ai sees ready form, journal and selected job results on one page', async ({ page, baseURL }) => {
    const ajax = { readyz: [], actions: [], submit: [], jobs: [], job_result: [] };
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
            // The exact sequence and the public rejection message are asserted below.
            if (action === 'submit' && response.request().method() === 'POST' && response.status() === 400) return;
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
        // Chromium logs expected HTTP errors; verify the exact response trace below.
        const ajaxError = url.origin === baseURL && url.pathname === '/lib/exe/ajax.php' &&
            url.searchParams.get('call') === 'plugin_petar';
        if (ajaxError && url.searchParams.get('action') === 'job_result' &&
            message.text().includes('409')) return;
        if (ajaxError && url.searchParams.get('action') === 'submit' &&
            message.text().includes('400')) return;
        issues.push('console error');
    });

    try {
        await test.step('log in to DokuWiki as the fixture ai user', () => loginAsAI(page));

        const form = page.locator('.plugin_petar form');
        const journal = page.locator('[data-petar-jobs]');
        await expect(form, 'one form after login').toHaveCount(1);
        await expect(journal, 'one journal on the same page').toHaveCount(1);
        await expect(journal).toBeVisible();
        await expect(page.locator('.plugin_petar .petar-status')).toHaveText('Ready');
        const submit = form.getByRole('button', { name: 'Submit', exact: true });
        await expect(submit).toBeVisible();
        await expect(submit).toBeEnabled();

        for (const action of ['readyz', 'actions', 'jobs']) {
            await expect.poll(() => ajax[action], { message: `wiki AJAX ${action}` }).toEqual(['GET 200']);
        }
        const rows = journal.locator('tbody tr');
        const [first, second, failed, processId] = [1, 2, 3, 9].map(n => `019b2c5d-1234-7000-8000-00000000000${n}`);
        const choices = form.locator('select');
        const dataField = form.locator('textarea');
        const submitResult = page.locator('.petar-result');
        await choices.selectOption({ label: 'Echo test (echo-test v1)' });
        await dataField.fill('  <tag> & "quote"  ');
        await submit.click();
        await expect(submitResult).toHaveAttribute('data-input-id', first);
        await choices.selectOption({ label: 'Fixture v2 (fixture-action v2)' });
        await dataField.fill('{');
        await submit.click();
        await expect(submitResult).toContainText('Enter valid JSON');
        expect(ajax.submit).toEqual(['POST 201']);
        await dataField.fill('{"n":9007199254740993}');
        await submit.click();
        await expect(submitResult).toHaveAttribute('data-input-id', second);
        await dataField.fill('[]');
        await submit.click();
        await expect(submitResult).not.toHaveAttribute('data-input-id', second);
        await expect.poll(() => ajax.submit).toEqual(['POST 201', 'POST 201', 'POST 400']);
        await expect(submitResult).toContainText(/rejected/i);
        await expect(submitResult).not.toContainText('PRIVATE_FIXTURE_DIAGNOSTIC');
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
        const special = card.locator('.petar-job-special');
        const json = card.locator('.petar-job-public');
        const toggle = card.locator('.petar-job-json-toggle');
        const open = id => journal.getByRole('button', { name: `View result: ${id}` });
        await expect(card).toBeHidden();
        await filter.selectOption('completed');
        await expect(rows).toHaveCount(2);
        await open(second).click();
        await expect(card).toBeVisible();
        await expect(card.locator('dd')).toHaveText([
            second, processId, 'echo-test', 'Completed', '2026-01-01T00:02:00Z', '2026-01-01T00:03:00Z',
        ]);
        await expect(json).toBeVisible();
        await expect(json).toContainText('second-job-public');
        await expect(special).toBeHidden();
        await expect(toggle).toBeHidden();
        await expect(card).not.toContainText('first-job-public');
        await card.getByRole('button', { name: 'Close result' }).click();
        await expect(card).toBeHidden();
        await expect(open(second)).toBeFocused();
        await expect(filter).toHaveValue('completed');
        await expect(rows).toHaveCount(2);
        await expect(open(first)).toBeVisible();
        await expect(open(second)).toBeVisible();
        await open(first).click();
        const echo = 'first-job-public <img src=x>\nsecond line';
        await expect(special).toBeVisible();
        expect(await special.textContent()).toBe(echo);
        await expect(card.locator('img')).toHaveCount(0);
        await expect(json).toBeHidden();
        await expect(toggle).toHaveAccessibleName('Show JSON');
        await expect(toggle).toHaveAttribute('aria-expanded', 'false');
        await toggle.click();
        await expect(json).toBeVisible();
        expect(await json.textContent()).toBe(JSON.stringify({ type: 'echo-test', version: 1, data: { echo } }, null, 2));
        await expect(special).toBeVisible();
        await expect(toggle).toHaveAttribute('aria-expanded', 'true');
        await expect(toggle).toHaveAccessibleName('Hide JSON');
        await expect(toggle).toBeFocused();
        await page.keyboard.press('Enter');
        await expect(json).toBeHidden();
        await expect(toggle).toHaveAccessibleName('Show JSON');
        await expect(toggle).toHaveAttribute('aria-expanded', 'false');
        await expect(card).not.toContainText('second-job-public');
        await filter.selectOption('failed');
        await expect(card).toBeHidden();
        await expect(rows).toHaveCount(1);
        await open(failed).click();
        await expect(card.getByRole('status')).toHaveText('This job failed. No public result is available.');
        await expect(json).toBeHidden();
        await expect(special).toBeHidden();
        await expect(toggle).toBeHidden();
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

test('word-count real backend', async ({ page, baseURL }) => {
    test.skip(process.env.PETAR_REAL_WORD_COUNT !== '1', 'requires the isolated, accepted T27 backend');
    test.setTimeout(60_000);
    const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
    const parseAjax = raw => {
        const url = new URL(raw);
        return url.origin === baseURL && url.pathname === '/lib/exe/ajax.php' &&
            url.searchParams.get('call') === 'plugin_petar' ? url.searchParams : null;
    };
    const submitRequests = [], resultRequests = [], outsideWiki = [];
    page.on('request', request => {
        if (new URL(request.url()).origin !== baseURL) outsideWiki.push('request outside wiki origin');
        const query = parseAjax(request.url());
        if (query?.get('action') === 'submit') submitRequests.push(request.method());
        if (query?.get('action') === 'job_result') resultRequests.push([request.method(), query.get('job_id')]);
    });
    const responseFor = action => page.waitForResponse(r => parseAjax(r.url())?.get('action') === action);
    await loginAsAI(page);
    const form = page.locator('.plugin_petar form');
    const journal = page.locator('[data-petar-jobs]');
    const submit = form.getByRole('button', { name: 'Submit', exact: true });
    await expect(submit).toBeEnabled();
    await form.getByRole('combobox').selectOption({ label: 'Word count (word-count v1)' });
    const jsonField = form.getByRole('textbox', { name: 'JSON data', exact: true });
    await expect(form.getByRole('textbox')).toHaveCount(1);
    await expect(form.locator('.petar-pattern')).toBeHidden();
    await jsonField.fill('{"text":"один два три"}');
    const [created] = await Promise.all([responseFor('submit'), submit.click()]);
    expect(created.status()).toBe(201);
    const submission = page.locator('.petar-result');
    await expect(submission).toHaveAttribute('data-input-id', uuid);
    await expect(submission).toHaveAttribute('data-process-id', uuid);
    const inputId = await submission.getAttribute('data-input-id');
    const processId = await submission.getAttribute('data-process-id');
    const table = journal.locator('.petar-jobs-table');
    await expect(table).toHaveAttribute('aria-busy', 'false');
    const completed = journal.locator('tbody tr')
        .filter({ has: page.getByRole('cell', { name: processId, exact: true }) })
        .filter({ has: page.getByRole('cell', { name: 'Completed', exact: true }) });
    const refresh = journal.getByRole('button', { name: 'Refresh', exact: true });
    await expect.poll(async () => {
        const [jobs] = await Promise.all([responseFor('jobs'), refresh.click()]);
        expect(jobs.status()).toBe(200);
        await expect(table).toHaveAttribute('aria-busy', 'false');
        return completed.count();
    }, { timeout: 25_000, intervals: [500, 1000, 2000], message: 'wait for this process to complete' }).toBe(1);
    const jobId = await completed.getByRole('cell').first().textContent();
    expect(jobId).toMatch(uuid);
    expect(jobId).not.toBe(inputId);
    expect(jobId).not.toBe(processId);
    expect(inputId).not.toBe(processId);
    const [result] = await Promise.all([responseFor('job_result'),
        completed.getByRole('button', { name: `View result: ${jobId}`, exact: true }).click()]);
    expect(result.status()).toBe(200);
    const card = journal.getByRole('article', { name: 'Job result' });
    await expect(card.locator('.petar-job-special')).toBeHidden();
    await expect(card.locator('.petar-job-json-toggle')).toBeHidden();
    const fallback = card.locator('.petar-job-public');
    await expect(fallback).toBeVisible();
    const expected = { type: 'word-count', version: 1, data: { words: 3 } };
    const text = await fallback.textContent();
    expect(JSON.parse(text)).toEqual(expected);
    expect(text).toBe(JSON.stringify(expected, null, 2));
    expect(submitRequests).toEqual(['POST']);
    expect(resultRequests).toEqual([['GET', jobId]]);
    expect(outsideWiki).toEqual([]);
    console.log('Real backend: submit POST 201 → jobs GET 200 (completed) → job_result GET 200; job=%s; words=3', jobId);
});
