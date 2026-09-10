import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { bindSightkick } from '../dist/executor.mjs';
const root = fileURLToPath(new URL('../../../', import.meta.url));
let browser, server, origin, dir, generated;
before(async () => {
  dir = await mkdtemp(new URL('../.test-', import.meta.url));
  const output = `${dir}/search.mjs`;
  execFileSync('go', ['run', '.', 'build', '../examples/search', '--target', 'playwright', '-o', output], {cwd: `${root}/generator`});
  generated = await import(output);
  server = createServer(async (req, res) => {
    res.setHeader('content-type', req.url?.startsWith('/search.bundle.js') ? 'text/javascript' : 'text/html');
    res.end(await readFile(`${root}/packages/runtime/demo/${req.url?.startsWith('/search.bundle.js') ? 'search.bundle.js' : 'search.html'}`));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({headless: true, ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? {executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH} : {})});
});
after(async () => {
  await browser?.close();
  if (server) await new Promise(resolve => server.close(resolve));
  if (dir) await rm(dir, {recursive: true, force: true});
});
async function withPage(fn) { const page = await browser.newPage(); try { await fn(page); } finally { await page.close(); } }
const query = (selector, preds = []) => ({parts: [{locators: [selector], preds}]});
const pred = (extractor, op, value, ci = false) => ({extractor, op, value, ci});
const text = {kind: 'text'};
const valueReturn = q => ({kind: 'value', query: q, extractor: text});
function tool(name, steps = [], rest = {}) { return {name, mode: 'live', inputSchema: {type: 'object', properties: {}}, steps, ...rest}; }
function bind(page, tools, views = []) { return bindSightkick(page, {version: 1, name: 'test', views, tools}, {timeoutMs: 300, log() {}}); }

test('generated module drives real search SPA with guidance and idempotent returns', () => withPage(async page => {
  await page.goto(`${origin}/?noboot`);
  const sk = generated.createSightkick(page);
  const result = await sk.tools.search({query: 'ATL to LHR'});
  assert.equal(result.ok, true);
  assert.equal(result.guidance[0].when, 'after_navigation');
  await sk.views.Results.waitFor();
  assert.equal((await sk.tools.list_results()).items.length, 3);
  assert.equal((await sk.tools.select_flight({flight_id: 'f1'})).ok, true);
  assert.equal((await sk.tools.select_flight({flight_id: 'f1'})).skipped, true);
  assert.equal((await sk.tools.book()).value, 'BK-F1');
  assert.equal((await sk.tools.book()).skipped, true);
  assert.equal((await sk.tools.set_sort()).items[0].id, 'f3');
}));

test('declarations resolve under NodeNext and reject invalid params', async () => {
  await writeFile(`${dir}/consumer.mts`, `import type { Page } from 'playwright';
import { createSightkick } from './search.mjs';
declare const page: Page;
const {tools, views} = createSightkick(page);
await tools.search({query: 'flight'});
await views.Results.waitFor();
const items = (await tools.list_results()).items;
items?.map(item => item.price.toUpperCase());
// @ts-expect-error required param
await tools.search();
// @ts-expect-error wrong type
await tools.search({query: 1});
// @ts-expect-error unknown tool
await tools.no_such_tool();
// @ts-expect-error wrong field
items?.map(item => item.not_a_field);
`);
  execFileSync(process.execPath, [`${root}/node_modules/typescript/bin/tsc`, '--noEmit', '--strict', '--skipLibCheck', '--target', 'ES2022', '--module', 'NodeNext', '--moduleResolution', 'NodeNext', `${dir}/consumer.mts`], {stdio: 'pipe'});
});

test('query semantics retain scoped accessible-name predicates, ordering, indices and shadow roots', () => withPage(async page => {
  await page.setContent(`<section data-group="ALPHA"><span class="name" aria-label="Special Name">wrong</span><button class="choice" aria-label="Pick Me">wrong</button></section>
<section data-group="beta"><button class="choice">Other</button></section><div id="host"></div><label for="input">Native name</label><input id="input"><img alt="Picture name">`);
  await page.evaluate(() => document.querySelector('#host').attachShadow({mode:'open'}).innerHTML='<b class="shadow">Shadow</b>');
  const q = {parts: [
    {locators:['section'], preds:[pred({kind:'attr',attr:'data-group'},'^=','al',true), pred({kind:'text',within:'.name'},'=','Special Name'), pred({kind:'exists',within:'.choice'},'=','true')]},
    {locators:['.choice'],preds:[pred(text,'*=','pick',true)]}
  ]};
  const sk=bind(page,[tool('read',[],{returns:valueReturn(q)}),tool('order',[],{returns:valueReturn({parts:[{locators:['img','#input']}],index:1})}),tool('shadow',[],{returns:valueReturn(query('.shadow'))})]);
  assert.equal((await sk.tools.read()).value,'Pick Me');
  assert.equal((await sk.tools.order()).value,'Native name');
  assert.equal((await sk.tools.shadow()).value,'Shadow');
}));

test('trusted actions select visible duplicates; optional empty values and combobox keypress work', () => withPage(async page => {
  await page.setContent(`<input class="field" style="display:none"><input class="field" role="combobox" value="old"><button id="button">Go</button><output></output><script>
  document.querySelector('[role=combobox]').addEventListener('keydown',e=>{if(e.isTrusted&&e.key==='ArrowDown')document.querySelector('output').textContent='opened'});
  document.querySelector('#button').addEventListener('click',e=>{if(e.isTrusted)document.querySelector('output').textContent='clicked'});
  </script>`);
  const sk=bind(page,[tool('fill',[{op:'fill',query:query('.field'),value:'{{value}}'}],{inputSchema:{type:'object',properties:{value:{type:'string'}}}}),tool('click',[{op:'click',query:query('#button')}]),tool('key',[{op:'keypress',key:'ArrowDown'}])]);
  assert.equal((await sk.tools.fill()).ok,true);
  assert.equal(await page.locator('[role=combobox]').inputValue(),'old');
  await sk.tools.fill({value:''});
  assert.equal(await page.locator('[role=combobox]').inputValue(),'');
  await sk.tools.fill({value:'new'});
  assert.equal(await page.locator('output').textContent(),'opened');
  assert.equal(await page.locator('[role=combobox]').inputValue(),'new');
  await sk.tools.click();
  assert.equal(await page.locator('output').textContent(),'clicked');
}));

test('guards, absence, hidden presence waits, missing values and errors retain result shapes', () => withPage(async page => {
  await page.setContent('<p hidden>present</p>');
  const sk=bind(page,[tool('guard',[],{guard:{kind:'absent',query:query('.missing')},returns:{kind:'list',query:query('.missing'),fields:{}}}),tool('missing',[],{returns:valueReturn(query('.missing'))}),tool('wait',[{op:'waitFor',query:query('p')}]),tool('timeout',[{op:'waitFor',query:query('.missing'),timeoutMs:10}]),tool('api',[],{mode:'api'}),tool('zero',[{op:'waitFor',query:query('p'),timeoutMs:0}])]);
  assert.deepEqual((await sk.tools.guard()).items,[]);
  assert.equal((await sk.tools.guard()).skipped,true);
  assert.deepEqual(await sk.tools.missing(),{ok:true});
  assert.equal((await sk.tools.wait()).ok,true);
  assert.equal((await sk.tools.timeout()).ok,false);
  assert.equal((await sk.tools.api()).ok,false);
  assert.equal((await sk.tools.zero()).ok,false);
}));

test('full-document navigation retains bindings and route matching semantics', () => withPage(async page => {
  await page.goto(`${origin}/?noboot`);
  const sk=bind(page,[tool('go',[{op:'goto',url:'/results?noboot'},{op:'waitFor',route:'/results'}],{returns:valueReturn(query('h1'))})],[{name:'Any',route:'/:view'},{name:'Nested',route:'/results/**'}]);
  const result=await sk.tools.go();
  assert.equal(result.ok,true);
  await page.waitForSelector('.result');
  await sk.views.Any.waitFor();
  await sk.views.Nested.waitFor();
  const read=bind(page,[tool('read',[],{returns:valueReturn(query('h1'))})]);
  assert.match((await read.tools.read()).value,/Results/);
}));

test('prototype names and adversarial strings survive evaluation without executing code', () => withPage(async page => {
  const attack=`\"');throw new Error('injected');//`;
  await page.setContent('<p></p>');
  await page.locator('p').evaluate((el,value)=>el.setAttribute('aria-label',value),attack);
  const schema=JSON.parse('{"type":"object","properties":{"__proto__":{"type":"string"}},"required":["__proto__"]}');
  const sk=bind(page,[tool('__proto__',[],{inputSchema:schema,returns:{kind:'list',query:query('p',[pred(text,'=','{{__proto__}}')]),fields:JSON.parse('{"__proto__":{"extractor":{"kind":"text"}}}')}})],[{name:'constructor',route:'/'}]);
  const result=await sk.tools.__proto__({});
  assert.equal(result.ok,false);
  const args=Object.fromEntries([['__proto__',attack]]);
  const returned=await sk.tools.__proto__(args);
  assert.equal(returned.items[0].__proto__,attack);
  assert.equal(Object.getPrototypeOf(sk.tools),null);
  assert.equal(typeof sk.views.constructor.waitFor,'function');
}));
