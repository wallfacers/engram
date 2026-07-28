import assert from 'node:assert/strict';
import {mkdtemp, mkdir, readFile, rm, writeFile} from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import * as validator from './check-docs.mjs';

async function fixtureRoot() {
  return mkdtemp(path.join(os.tmpdir(), 'engram-docs-validator-'));
}

function frontMatter({
  title,
  summary = '本文回答当前命令；不回答评测分数。',
  status = 'stable',
  audience = '[users, maintainers]',
  owner = 'engram-maintainers',
  lastReviewed = '2026-07-28',
  canonicalFor = '[test-topic]',
  tags = '[test, documentation]',
  extra = [],
} = {}) {
  return [
    '---',
    `title: ${title}`,
    `summary: ${summary}`,
    `status: ${status}`,
    `audience: ${audience}`,
    `owner: ${owner}`,
    `last_reviewed: ${lastReviewed}`,
    `canonical_for: ${canonicalFor}`,
    `tags: ${tags}`,
    ...extra,
    '---',
  ].join('\n');
}

async function writeDoc(root, relativePath, options = {}) {
  const fullPath = path.join(root, relativePath);
  await mkdir(path.dirname(fullPath), {recursive: true});
  const title = options.title ?? '测试文档';
  const body = options.body ?? `# ${title}\n\n本文是可验证的当前正文；不提供其他主题。\n\n## 说明\n\n内容。\n`;
  await writeFile(fullPath, `${frontMatter({...options, title})}\n${body}`, 'utf8');
  return relativePath;
}

test('tracked Markdown parser preserves Unicode paths and includes non-docs Markdown links', () => {
  const paths = validator.parseTrackedMarkdownPaths(
    'docs/archive/designs/2026-07-21-judge-口径-alignment-design.md\0specs/009/research.md\0memory/retriever.go\0',
  );
  assert.deepEqual(paths, [
    'docs/archive/designs/2026-07-21-judge-口径-alignment-design.md',
    'specs/009/research.md',
  ]);
});

test('metadata accepts a complete current document set with unique topics', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const files = [
    await writeDoc(root, 'docs/README.md', {title: '文档门户', canonicalFor: '[docs-portal]'}),
    await writeDoc(root, 'docs/guides/cli.md', {title: 'CLI 指南', canonicalFor: '[cli-usage]'}),
  ];

  assert.deepEqual(validator.validateMetadata({root, files, today: '2026-07-28'}), []);
});

test('metadata reports required-field and duplicate-topic violations', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const files = [
    await writeDoc(root, 'docs/README.md', {title: '文档门户', summary: '', canonicalFor: '[shared-topic]'}),
    await writeDoc(root, 'docs/guides/cli.md', {title: 'CLI 指南', canonicalFor: '[shared-topic]'}),
  ];

  const issues = validator.validateMetadata({root, files, today: '2026-07-28'});
  assert.ok(issues.some((issue) => issue.includes('summary')));
  assert.ok(issues.some((issue) => issue.includes('shared-topic')));
});

test('headings report extra H1, skipped level, and duplicate GitHub slug', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const file = await writeDoc(root, 'docs/README.md', {
    title: '文档门户',
    canonicalFor: '[docs-portal]',
    body: '# 文档门户\n\n## 重复\n\n#### 跳级\n\n## 重复\n\n# 第二主标题\n',
  });

  const issues = validator.validateHeadings({root, files: [file]});
  assert.ok(issues.some((issue) => issue.includes('exactly one H1')));
  assert.ok(issues.some((issue) => issue.includes('heading jumps')));
  assert.ok(issues.some((issue) => issue.includes('duplicate heading slug')));
});

test('links accept valid local targets and report missing files and anchors', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const valid = [
    await writeDoc(root, 'docs/README.md', {
      title: '文档门户', canonicalFor: '[docs-portal]', body: '# 文档门户\n\n[CLI](guides/cli.md#命令)\n',
    }),
    await writeDoc(root, 'docs/guides/cli.md', {
      title: 'CLI 指南', canonicalFor: '[cli-usage]', body: '# CLI 指南\n\n## 命令\n\n内容。\n',
    }),
  ];
  assert.deepEqual(validator.validateLinks({root, files: valid}), []);

  const invalid = await writeDoc(root, 'docs/broken.md', {
    title: '坏链接', canonicalFor: '[broken-links]', body: '# 坏链接\n\n[丢失](missing.md)\n\n[坏锚点](guides/cli.md#不存在)\n',
  });
  const issues = validator.validateLinks({root, files: [...valid, invalid]});
  assert.ok(issues.some((issue) => issue.includes('missing file')));
  assert.ok(issues.some((issue) => issue.includes('missing anchor')));
});

test('links accept tracked documentation directories', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const files = [
    await writeDoc(root, 'docs/README.md', {
      title: '文档门户', canonicalFor: '[docs-portal]', body: '# 文档门户\n\n[契约目录](contracts/)\n',
    }),
    await writeDoc(root, 'docs/contracts/example.md', {title: '契约示例', canonicalFor: '[contract-example]'}),
  ];
  assert.deepEqual(validator.validateLinks({root, files}), []);
});

test('navigation requires every current document to be reachable within two hops and non-orphaned', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const reachable = [
    await writeDoc(root, 'docs/README.md', {
      title: '文档门户', canonicalFor: '[docs-portal]', body: '# 文档门户\n\n[指南](guides/index.md)\n',
    }),
    await writeDoc(root, 'docs/guides/index.md', {
      title: '指南索引', canonicalFor: '[guide-index]', body: '# 指南索引\n\n[CLI](cli.md)\n',
    }),
    await writeDoc(root, 'docs/guides/cli.md', {
      title: 'CLI 指南', canonicalFor: '[cli-usage]', body: '# CLI 指南\n\n内容。\n',
    }),
  ];
  assert.deepEqual(validator.validateNavigation({root, files: reachable}), []);

  const hidden = await writeDoc(root, 'docs/architecture/hidden.md', {
    title: '隐藏文档', canonicalFor: '[hidden-topic]', body: '# 隐藏文档\n\n内容。\n',
  });
  const issues = validator.validateNavigation({root, files: [...reachable, hidden]});
  assert.ok(issues.some((issue) => issue.includes('not reachable within 2 hops')));
  assert.ok(issues.some((issue) => issue.includes('orphan document')));
});

test('retrieval fixtures locate one current owner for every Q1–Q8 assertion', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const fixtures = JSON.parse(await readFile(new URL('./retrieval-fixtures.json', import.meta.url), 'utf8'));
  const files = [];
  for (const fixture of fixtures) {
    if (files.includes(fixture.canonical_path)) continue;
    const related = fixtures.filter((candidate) => candidate.canonical_path === fixture.canonical_path);
    const patterns = related.flatMap((candidate) => candidate.required_assertions.flatMap((assertion) => assertion.patterns));
    files.push(await writeDoc(root, fixture.canonical_path, {
      title: fixture.topic,
      canonicalFor: `[${fixture.topic}]`,
      body: `# ${fixture.topic}\n\n${patterns.join('；')}\n`,
    }));
  }

  assert.deepEqual(validator.validateRetrieval({root, files, fixtures}), []);
  const broken = structuredClone(fixtures);
  broken[1].required_assertions[0].patterns = ['不存在的 CLI 断言'];
  const issues = validator.validateRetrieval({root, files, fixtures: broken});
  assert.ok(issues.some((item) => item.includes('Q2')));
});

test('relocation records, archive evidence, score consumers, and capability boundaries are enforceable', async (t) => {
  const root = await fixtureRoot();
  t.after(() => rm(root, {recursive: true, force: true}));
  const target = await writeDoc(root, 'docs/guides/cli.md', {title: 'CLI 指南', canonicalFor: '[cli-usage]'});
  const relocated = await writeDoc(root, 'docs/legacy-cli.md', {
    title: '旧 CLI 文档',
    status: 'relocated',
    canonicalFor: '[legacy-cli]',
    extra: ['canonical_path: docs/guides/cli.md'],
    body: '# 旧 CLI 文档\n\n本文已迁移，请阅读[CLI 指南](guides/cli.md)。\n',
  });
  const mappings = [{legacy_path: relocated, canonical_path: target}];
  assert.deepEqual(validator.validateRelocation({root, files: [target, relocated], expectedMappings: mappings}), []);

  const badRelocation = await writeDoc(root, 'docs/bad-relocation.md', {
    title: '坏重定向', status: 'relocated', canonicalFor: '[bad-relocation]',
    extra: ['canonical_path: docs/guides/cli.md'],
    body: '# 坏重定向\n\n[CLI](guides/cli.md) 和 [第二链接](guides/cli.md)。\n',
  });
  const relocationIssues = validator.validateRelocation({
    root, files: [target, badRelocation], expectedMappings: [{legacy_path: badRelocation, canonical_path: target}],
  });
  assert.ok(relocationIssues.some((item) => item.includes('one canonical link')));

  const archive = await writeDoc(root, 'docs/archive/bad.md', {
    title: '无结果归档', status: 'archived', canonicalFor: '[bad-archive]',
  });
  assert.ok(validator.validateMetadata({root, files: [archive], today: '2026-07-28'}).some((item) => item.includes('outcome or superseded_by')));

  const results = await writeDoc(root, 'docs/evaluation/results.md', {
    title: '评测结果', canonicalFor: '[evaluation-results]', body: '# 评测结果\n\nLongMemEval-S：80.80%。\n',
  });
  const consumers = await Promise.all([
    writeDoc(root, 'docs/product/capabilities.md', {title: '产品能力', canonicalFor: '[current-capabilities]', body: '# 产品能力\n\n[结果](../evaluation/results.md)\n'}),
    writeDoc(root, 'docs/product/roadmap.md', {title: '产品路线图', canonicalFor: '[product-roadmap]', body: '# 产品路线图\n\n[结果](../evaluation/results.md)\n'}),
    writeDoc(root, 'docs/evaluation/competitors.md', {title: '竞品', canonicalFor: '[competitors]', body: '# 竞品\n\n[结果](results.md)\n'}),
    writeDoc(root, 'docs/research/paper-direction.md', {title: '论文方向', canonicalFor: '[research-direction]', body: '# 论文方向\n\n[结果](../evaluation/results.md)\n'}),
  ]);
  assert.deepEqual(validator.validateScoreConsumers({root, consumers, resultsPath: results}), []);
  await writeDoc(root, consumers[0], {title: '产品能力', canonicalFor: '[current-capabilities]', body: '# 产品能力\n\n当前为 80.80%。[结果](../evaluation/results.md)\n'});
  assert.ok(validator.validateScoreConsumers({root, consumers, resultsPath: results}).some((item) => item.includes('duplicated score')));

  const architecture = await writeDoc(root, 'docs/architecture/memory-system.md', {title: '记忆系统', canonicalFor: '[memory-architecture]'});
  const capability = await writeDoc(root, 'docs/product/capability-boundaries.md', {
    title: '能力边界', canonicalFor: '[capability-boundaries]',
    body: '# 能力边界\n\n新鲜度与状态一致性尚未实现；详见 [backlog](backlog/memory-freshness.md)。习惯记忆未立项、未实现；详见 [探索](explorations/habit-memory.md)。[架构](../architecture/memory-system.md)。\n',
  });
  assert.deepEqual(validator.validateCurrentCapabilities({root, file: capability, architecturePath: architecture}), []);
  await writeDoc(root, capability, {title: '能力边界', canonicalFor: '[capability-boundaries]', body: '# 能力边界\n\n当前能力。\n'});
  assert.ok(validator.validateCurrentCapabilities({root, file: capability, architecturePath: architecture}).some((item) => item.includes('missing required boundary')));
});
