'use strict';

const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
    testMatch: 'smoke.spec.js',
    timeout: 30_000,
    expect: { timeout: 8_000 },
    retries: 0,
    workers: 1,
    reporter: 'list',
    preserveOutput: 'never',
    outputDir: '/tmp/petar-playwright',
    use: {
        baseURL: 'http://wiki',
        browserName: 'chromium',
        headless: true,
        actionTimeout: 8_000,
        navigationTimeout: 8_000,
        trace: 'off',
        screenshot: 'off',
        video: 'off',
    },
});
