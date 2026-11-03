import { test, expect, type Page, type Route } from '@playwright/test';
import { CONSOLE_URL, CONSOLE_API_KEY } from '../playwright.config';
const fixture = `<!doctype html><html><head><title>Console fixture</title></head><body><main>
<div class="ob-tabs"><a data-stage="proposed"><span class="c">0</span></a><a data-stage="sponsor_approved"><span class="c">0</span></a></div>
<form><textarea name="decision" aria-label="Decision note"></textarea><button type="reset">Reset</button></form>
<input id="org" aria-label="Organization"><p id="state" role="status"></p><div id="menu" hidden></div>
<input id="nation" aria-label="Nation"><div id="derived" hidden><span id="standing"></span><span id="why"></span></div>
<script src="/onboard-queue.js"></script><script src="/org-suggest.js"></script><script src="/affiliation-derive.js"></script>
<script>initOrgParentSuggest(document.querySelector('#org'),document.querySelector('#state'),document.querySelector('#menu'));
initAffiliationDerive(document.querySelector('#nation'),document.querySelector('#derived'),{standing:document.querySelector('#standing'),standingWhy:document.querySelector('#why')});</script>
</main></body></html>`;
async function open(page: Page) {
 await page.route('**/fixture', r => r.fulfill({contentType:'text/html',body:fixture}));
 await page.goto('/fixture');
}
test('failed and expired queue reads are visible and recover',async({page})=>{
 await page.clock.install(); let status=500;
 await page.route('**/api/onboard/proposals/counts',r=>r.fulfill({status,json: status===200?{proposed:0,sponsor_approved:0}:{error:'synthetic'}}));
 await open(page);await page.clock.fastForward(20000);await expect(page.locator('#queue-read-error')).toContainText('Could not refresh');
 status=401;await page.clock.fastForward(20000);await expect(page.locator('#queue-read-error')).toContainText('session expired');
 status=200;await page.clock.fastForward(20000);await expect(page.locator('#queue-read-error')).toBeHidden();
});
test('changed queue counts preserve decision notes until reset',async({page})=>{
 await page.clock.install(); let pages=0;
 await page.route('**/fixture',r=>{pages++;return r.fulfill({contentType:'text/html',body:fixture});});
 await page.route('**/api/onboard/proposals/counts',r=>r.fulfill({json:{proposed:1,sponsor_approved:0}}));
 await page.goto('/fixture');await page.getByLabel('Decision note').fill('Keep this unsaved note');
 await page.clock.fastForward(20000);await expect(page.locator('.c').first()).toHaveText('1');
 expect(pages).toBe(1);await expect(page.getByLabel('Decision note')).toHaveValue('Keep this unsaved note');
 await page.getByRole('button',{name:'Reset'}).click();await page.clock.runFor(1);await page.clock.fastForward(20000);
 await expect.poll(()=>pages).toBe(2);
});
test('organization and affiliation failures do not masquerade as results',async({page})=>{
 await page.route('**/onboarding/org-suggest?**',r=>r.fulfill({status:401,json:{}}));
 await page.route('**/onboarding/affiliation-derive?**',r=>r.fulfill({status:500,json:{}}));
 await open(page);await page.getByLabel('Organization').fill('fixture');await expect(page.locator('#state')).toContainText('session expired');
 await page.getByLabel('Nation').fill('USA');await expect(page.locator('#why')).toContainText('Could not load');
 await expect(page.locator('#standing')).toHaveText('Unavailable');
});
test('older organization success cannot replace the latest failure',async({page})=>{
 let older: Route|undefined;
 await page.route('**/onboarding/org-suggest?**',r=>{
  if(new URL(r.request().url()).searchParams.get('q')==='old'){older=r;return;}
  return r.fulfill({status:500,json:{}});
 });
 await open(page);await page.getByLabel('Organization').fill('old');await expect.poll(()=>!!older).toBe(true);
 await page.getByLabel('Organization').fill('new');await expect(page.locator('#state')).toContainText('Could not check');
 await older!.fulfill({json:{resolves:true,canonical:'old'}});await page.waitForLoadState('networkidle');
 await expect(page.locator('#state')).toContainText('Could not check');
});
test('older affiliation response cannot replace current results',async({page})=>{
 let older: Route|undefined;
 await page.route('**/onboarding/affiliation-derive?**',r=>{
  if(new URL(r.request().url()).searchParams.get('nation')==='USA'){older=r;return;}
  return r.fulfill({json:{nation:'ISL',organic_known:false,alliance_standing:'Current',alliance_basis:'New result'}});
 });
 await open(page);await page.getByLabel('Nation').fill('USA');await expect.poll(()=>!!older).toBe(true);
 await page.getByLabel('Nation').fill('ISL');await expect(page.locator('#standing')).toHaveText('Current');
 await older!.fulfill({json:{nation:'USA',organic_known:false,alliance_standing:'Old'}});await page.waitForLoadState('networkidle');
 await expect(page.locator('#standing')).toHaveText('Current');
});

test('unknown identity renders the real shared error page', async ({ page }) => {
 test.skip(!!process.env.INTAKE_URL || process.env.CONSOLE_E2E === '0', 'needs the local console');
 await page.setExtraHTTPHeaders({'X-Forwarded-Groups':'PLATFORM-ADMIN','X-Forwarded-Email':'admin@example.invalid'});
 const response = await page.goto(CONSOLE_URL + '/users/missing?idp=tenant');
 expect(response!.status()).toBe(502);
 await expect(page.getByRole('alert')).toContainText('The request could not be completed');
 await expect(page.locator('nav')).toBeVisible();
 await expect(page.locator('body')).not.toContainText('Kratos GET');
 await expect(page.locator('body')).not.toContainText('127.0.0.1');
 await page.screenshot({path:'/tmp/goat-1492-error.png',fullPage:true});
});

test('an older queue response cannot overwrite a newer failure', async ({ page }) => {
 await page.clock.install(); let older: Route | undefined, calls = 0;
 await page.route('**/api/onboard/proposals/counts', r => {
  if (++calls === 1) { older = r; return; }
  return r.fulfill({status:500,json:{}});
 });
 await open(page); await page.clock.fastForward(20000); await expect.poll(() => !!older).toBe(true);
 await page.clock.fastForward(20000); await expect(page.locator('#queue-read-error')).toContainText('Could not refresh');
 await older!.fulfill({json:{proposed:1,sponsor_approved:0}}); await page.waitForLoadState('networkidle');
 await expect(page.locator('#queue-read-error')).toContainText('Could not refresh');
 await expect(page.locator('.c').first()).toHaveText('0');
});
test('malformed counts preserve the last known queue state', async ({ page }) => {
 await page.clock.install();
 await page.route('**/api/onboard/proposals/counts', r => r.fulfill({json:{}}));
 await open(page); await page.clock.fastForward(20000);
 await expect(page.locator('#queue-read-error')).toContainText('Could not refresh');
 await expect(page.locator('.c').first()).toHaveText('0');
});

test('real create-org panel shares failure and stale-sector handling', async ({ page, request }) => {
 test.skip(!!process.env.INTAKE_URL || process.env.CONSOLE_E2E === '0' || process.env.ORG_MATCH_ENABLED !== 'true', 'needs the local console with org matching');
 const invite = await request.post(CONSOLE_URL + '/api/onboard/invite', {headers:{Authorization:`Bearer ${CONSOLE_API_KEY}`},data:{env:'zagros',tier:'developer',affiliation:'partner'}});
 expect(invite.status()).toBe(201);
 const {token} = await invite.json();
 const submission = await request.post(CONSOLE_URL + '/public/onboard/submit', {data:{token,submission:{name:'Read Failure Fixture',email:`read.${Date.now()}@example.invalid`,organization:'Unregistered Fixture Org',nationality:'CAN'}}});
 expect(submission.status()).toBe(201);
 const {proposal_id} = await submission.json();
 await page.setExtraHTTPHeaders({'X-Forwarded-Groups':'PLATFORM-ADMIN','X-Forwarded-Email':'admin@example.invalid'});
 let older: Route | undefined;
 await page.route('**/onboarding/affiliation-derive?**', r => {
  if(new URL(r.request().url()).searchParams.get('sector') === 'state') {older=r;return;}
  return r.fulfill({status:500,json:{}});
 });
 await page.goto(CONSOLE_URL + '/onboarding?status=proposed&id=' + proposal_id);
 await expect(page.locator('#org-nation')).toBeVisible();
 await page.locator('#org-nation').fill('USA');
 await expect(page.locator('#org-derived-standing-why')).toContainText('Could not load');
 await page.locator('#org-sector').selectOption('state'); await expect.poll(() => !!older).toBe(true);
 await page.locator('#org-gov-branch').selectOption('military');
 await page.locator('#org-sector').selectOption('commercial');
 await expect(page.locator('#org-gov-branch-label')).toBeHidden();
 await expect(page.locator('#org-gov-branch')).toHaveValue('');
 await expect(page.locator('#org-derived-standing-why')).toContainText('Could not load');
 await older!.fulfill({json:{nation:'USA',organic_known:false,alliance_standing:'Old sector'}});
 await page.waitForLoadState('networkidle');
 await expect(page.locator('#org-derived-standing')).toHaveText('Unavailable');
 const correction = page.locator('[id^="fe-nation-"]');
 await expect(correction).toBeVisible(); await correction.fill('ISL');
 await expect(page.locator('[id^="fe-derived-standing-why-"]')).toContainText('Could not load');
});
