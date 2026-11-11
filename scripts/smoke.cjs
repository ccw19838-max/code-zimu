/* Run against scripts/run-demo.py. Requires Playwright and Chromium.
 * PLAYWRIGHT_MODULE may point to an existing Playwright installation.
 * Screenshots contain demo metadata only; credentials are never printed.
 */
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const root = path.resolve(__dirname, '..');
const origin = process.env.CATALOG_ORIGIN || 'http://127.0.0.1:18091';
const account = JSON.parse(fs.readFileSync(path.join(root, 'data/demo/dev-account.json'), 'utf8'));
const output = path.join(root, 'docs/ui');

(async () => {
  const browser = await chromium.launch({executablePath: process.env.CHROMIUM_PATH || '/usr/bin/chromium', args: ['--no-sandbox']});
  try {
    const context = await browser.newContext({viewport: {width: 1440, height: 1100}, locale: 'zh-CN', timezoneId: 'Asia/Shanghai'});
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', err => errors.push(err.message));
    fs.mkdirSync(output, {recursive: true});
    await page.goto(origin);
    assert.equal((await context.request.get(origin + '/api/github/projects')).status(), 200);
    const catalog = await (await context.request.get(origin + '/api/github/projects')).json();
    const assets = catalog.projects.find(project => project.name === 'lab-asset-system');
    assert.equal(catalog.projects.filter(project => project.profile?.kind === 'team').length, 3);
    assert.ok(assets.profile.outputs.length && assets.profile.development.entry_points.length);
    assert.equal((await (await context.request.get(origin + '/api/session')).json()).authenticated, false);
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    assert.equal(await page.locator('#gh-refresh').count(), 0, 'anonymous visitor cannot force GitHub refresh');
    assert.equal(await page.locator('.workspace-header').count(), 0, 'standalone catalog must not contain the workbench shell');
    await page.locator('#catalog-admin-login').click();
    await page.locator('#catalog-login-form [name=password]').fill(account.password);
    await page.locator('#catalog-login-submit').click();
    await page.locator('#gh-refresh').waitFor();
    assert.match(await page.locator('#content').innerText(), /演示/);
    const links = await page.locator('[data-gh-link]').evaluateAll(nodes => nodes.map(n => ({href: n.href, target: n.target, rel: n.rel})));
    assert.equal(links.length, 6);
    for (const link of links) {
      assert.equal(new URL(link.href).origin, 'https://github.com');
      assert.equal(link.target, '_blank');
      assert.match(link.rel, /noopener/);
      assert.match(link.rel, /noreferrer/);
    }
    await page.locator('#gh-search').fill('XRplan');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 1);
    assert.match(await page.locator('[data-gh-card]').innerText(), /XRplan/);
    await page.locator('#gh-search').fill('no-project-with-this-name');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 0);
    await page.locator('.gh-empty [data-gh-action=reset]').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    await page.locator('#gh-search').fill(assets.profile.outputs[1].description);
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 1);
    assert.equal(await page.locator('[data-gh-card]').getAttribute('data-name'), 'lab-asset-system');
    await page.locator('#gh-search').fill('');
    await page.locator('#gh-kind').selectOption('team');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 3);
    await page.locator('#gh-kind').selectOption('reference');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 3);
    await page.locator('#gh-kind').selectOption('');
    await page.locator('#gh-language').selectOption('Python');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 1);
    assert.match(await page.locator('[data-gh-card]').innerText(), /flask/);
    await page.locator('#gh-language').selectOption('');
    await page.locator('#gh-visibility').selectOption('private');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 3);
    await page.locator('#gh-visibility').selectOption('');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    await page.locator('#gh-sort').selectOption('name');
    const names = await page.locator('[data-gh-card]').evaluateAll(nodes => nodes.map(n => n.textContent));
    assert.match(names[0], /flask/);
    await page.locator('#gh-sort').selectOption('updated');
    await page.locator('#gh-archived').selectOption('true');
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 0);
    await page.locator('#gh-archived').selectOption('');
    const refresh = page.waitForResponse(r => r.url().endsWith('/api/github/refresh'));
    await page.locator('#gh-refresh').click();
    assert.equal((await refresh).status(), 200);
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6 && !document.querySelector('#gh-refresh').disabled);
    await page.locator('#gh-directory-title').click();
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({path: path.join(output, 'github-projects-demo.png'), fullPage: true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, 'desktop overflow');
    await page.locator('[data-gh-card][data-name="lab-asset-system"] .gh-detail-open').click();
    await page.locator('#gh-project-dialog[open]').waitFor();
    const detail = await page.locator('#gh-project-dialog').innerText();
    for (const capability of assets.profile.capabilities) assert.ok(detail.includes(capability));
    for (const outcome of assets.profile.outputs) assert.ok(detail.includes(outcome.description));
    for (const entry of assets.profile.development.entry_points) assert.ok(detail.includes(entry.path));
    assert.ok(detail.includes(assets.profile.maintainer));
    await page.waitForFunction(() => [...document.querySelectorAll('#gh-project-dialog img')].every(img => img.complete && img.naturalWidth > 0));
    await page.screenshot({path: path.join(output, 'github-project-detail.png')});
    await page.locator('[data-gh-detail-jump="gh-detail-development"]').click();
    await page.locator('#gh-detail-entry-points').scrollIntoViewIfNeeded();
    await page.screenshot({path: path.join(output, 'github-project-development.png')});
    await page.locator('[data-gh-action="close-detail"]').click();
    await page.locator('#gh-search').fill('XRplan');
    await page.locator('[data-catalog-page="rules"]').click();
    await page.locator('#gh-rules-page').waitFor();
    const rules = await (await context.request.get(origin + '/team-rules.json')).json();
    assert.equal(await page.locator('[data-rule-section]').count(), rules.sections.length);
    assert.match(await page.locator('#content').innerText(), /待团队采纳/);
    assert.equal(await page.locator('[data-gh-card]').count(), 0);
    assert.equal(await page.locator('#gh-project-dialog').count(), 0);
    for (const section of rules.sections) {
      const text = await page.locator(`[data-rule-section="${section.id}"]`).innerText();
      for (const item of section.items) assert.ok(text.includes(item));
    }
    await page.screenshot({path: path.join(output, 'github-team-rules.png')});
    const downloads = await page.locator('[data-rule-template]').evaluateAll(nodes => nodes.map(node => node.getAttribute('href')));
    assert.equal(downloads.length, 5);
    for (const url of downloads) {
      const response = await context.request.get(origin + url);
      assert.equal(response.status(), 200);
      assert.equal(await response.text(), fs.readFileSync(path.join(root, 'web', url), 'utf8'));
    }
    await page.locator('[data-rule-jump="gh-rule-templates"]').click();
    await page.locator('#gh-rule-templates').scrollIntoViewIfNeeded();
    await page.screenshot({path: path.join(output, 'github-team-templates.png')});
    const download = page.waitForEvent('download');
    await page.locator('[data-rule-template]').first().click();
    assert.equal((await download).suggestedFilename(), 'README.template.md');
    await page.locator('[data-catalog-page="projects"]').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 1);
    assert.equal(await page.locator('#gh-search').inputValue(), 'XRplan');
    await page.locator('#gh-search').fill('');
    const missingProfileFixture = async route => {
      const response = await route.fetch();
      const body = await response.json();
      body.projects.find(project => project.name === 'lab-asset-system').profile = null;
      body.projects.find(project => project.name === 'workbench-navigation').profile.summary = '<img src=x onerror=alert(1)>';
      await route.fulfill({response, json: body});
    };
    await page.route('**/api/github/projects', missingProfileFixture);
    await page.locator('#catalog-reload').click();
    await page.waitForFunction(() => document.querySelector('[data-name="lab-asset-system"]')?.textContent.includes('功能说明待补充'));
    assert.equal(await page.locator('img[src=x]').count(), 0, 'profile text became HTML');
    assert.match(await page.locator('[data-name="workbench-navigation"]').innerText(), /<img src=x/);
    await page.locator('[data-gh-card][data-name="lab-asset-system"] .gh-detail-open').click();
    await page.locator('.gh-detail-missing').waitFor();
    assert.equal(await page.locator('[data-gh-screenshot]').count(), 0);
    assert.match(await page.locator('#gh-detail-entry-points').innerText(), /待补充/);
    await page.locator('[data-gh-action="close-detail"]').click();
    await page.unroute('**/api/github/projects', missingProfileFixture);
    await page.locator('#catalog-reload').click();
    await page.waitForFunction(() => document.querySelector('[data-name="lab-asset-system"] h3')?.textContent.includes('实验室资产与财务平台'));
    await page.locator('#gh-flow-open').click();
    await page.locator('dialog[open]').waitFor();
    assert.match(await page.locator('dialog[open]').innerText(), /演示|模拟/);
    const externalRequests = [];
    const observe = request => { if (new URL(request.url()).origin !== origin) externalRequests.push(request.url()); };
    page.on('request', observe);
    for (let stage = 1; stage <= 5; stage++) {
      await page.locator('#gh-flow-next').click();
      assert.equal(await page.locator('#github-flow-dialog').getAttribute('data-stage'), String(stage));
    }
    assert.equal(await page.locator('#gh-flow-next').isDisabled(), true);
    assert.deepEqual(externalRequests, [], 'simulation made an external request');
    page.off('request', observe);
    await page.locator('#gh-flow-reset').click();
    assert.equal(await page.locator('#github-flow-dialog').getAttribute('data-stage'), '0');
    await page.screenshot({path: path.join(output, 'github-join-demo.png'), fullPage: true});
    await page.keyboard.press('Escape');
    // Revoked credentials and failed updates must remove previously visible metadata.
    const fail = route => route.fulfill({status: 502, contentType: 'application/json', body: JSON.stringify({error: 'GitHub 令牌无效或已过期'})});
    await page.route('**/api/github/refresh', fail);
    await page.locator('#gh-refresh').click();
    await page.locator('#gh-catalog-error').waitFor();
    assert.equal(await page.locator('[data-gh-card]').count(), 0);
    await page.unroute('**/api/github/refresh', fail);
    await page.locator('#gh-error-retry').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    await page.route('**/api/github/projects', fail);
    await page.locator('#catalog-reload').click();
    await page.locator('#gh-catalog-error').waitFor();
    assert.equal(await page.locator('[data-gh-card]').count(), 0);
    await page.unroute('**/api/github/projects', fail);
    await page.locator('#gh-error-retry').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    // Simulate a live directory to exercise private metadata in multiple tabs.
    // Backend authorization is covered separately by the Go API tests.
    const liveFixture = async route => {
      const response = await route.fetch();
      const body = await response.json();
      await route.fulfill({response, json: {...body, mode: 'live'}});
    };
    await context.route('**/api/github/projects', liveFixture);
    const second = await context.newPage();
    second.on('pageerror', err => errors.push(err.message));
    await second.goto(origin);
    await second.locator('#gh-refresh').waitFor();
    await page.locator('[data-gh-card][data-name="lab-asset-system"] .gh-detail-open').click();
    let resumeSession;
    const gate = new Promise(resolve => { resumeSession = resolve; });
    const delayedSession = async route => { await gate; await route.continue(); };
    await page.route('**/api/session', delayedSession);
    await page.evaluate(() => window.dispatchEvent(new Event('focus')));
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 0);
    assert.equal(await page.locator('#gh-project-dialog').count(), 0, 'focus revalidation retained private details');
    resumeSession();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    await page.unroute('**/api/session', delayedSession);
    await page.locator('[data-gh-card][data-name="lab-asset-system"] .gh-detail-open').click();
    await second.locator('#catalog-admin-logout').click();
    await page.locator('#catalog-admin-login').waitFor({state: 'visible'});
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 3);
    assert.equal(await page.locator('.gh-private').count(), 0, 'another tab logout retained private metadata');
    assert.equal(await page.locator('#gh-project-dialog').count(), 0, 'another tab logout retained private project details');
    await second.close();
    await context.unroute('**/api/github/projects', liveFixture);
    await page.locator('#catalog-reload').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-gh-card]').length === 6);
    assert.equal(await page.locator('#gh-refresh').count(), 0);
    assert.equal((await (await context.request.get(origin + '/api/session')).json()).authenticated, false);
    assert.deepEqual(errors, []);
    console.log('PASS: project capabilities, outcomes, development details, result images, team rules, template downloads, navigation, search, filters, safe links, refresh, simulation, error recovery and private metadata clearing.');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
