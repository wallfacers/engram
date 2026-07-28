import {existsSync, readFileSync, statSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import {pathToFileURL} from 'node:url';

const AUDIENCES = new Set(['users', 'maintainers', 'agents']);
const STATUSES = new Set(['stable', 'active', 'proposed', 'archived', 'relocated']);
const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export const EXPECTED_RELOCATIONS = [
  ['docs/background-extraction-from-workhorse-agent.md', 'docs/architecture/provenance.md'],
  ['docs/cli.md', 'docs/guides/cli.md'],
  ['docs/competitive-benchmarks.md', 'docs/evaluation/competitors.md'],
  ['docs/locomo-e2e-eval-reproduction.md', 'docs/operations/evaluation/locomo-runbook.md'],
  ['docs/locomo-score-levers.md', 'docs/evaluation/experiment-verdicts.md'],
  ['docs/mcp-server.md', 'docs/guides/mcp-server.md'],
  ['docs/memory-architecture.md', 'docs/architecture/memory-system.md'],
  ['docs/memory-freshness-and-retrieval-policy.md', 'docs/product/backlog/memory-freshness.md'],
  ['docs/memory-strategy.md', 'docs/product/roadmap.md'],
  ['docs/memos-inhouse-locomo-repro.md', 'docs/evaluation/reports/memos-locomo-reproduction.md'],
  ['docs/remote-eval-box.md', 'docs/operations/evaluation/remote-gpu-runbook.md'],
  ['docs/results-matrix-2026-07-26.md', 'docs/evaluation/results.md'],
].map(([legacy_path, canonical_path]) => ({legacy_path, canonical_path}));

export const SCORE_CONSUMERS = [
  'docs/product/capabilities.md',
  'docs/product/roadmap.md',
  'docs/evaluation/competitors.md',
  'docs/research/paper-direction.md',
];

function issue(file, message) {
  return `${file}: ${message}`;
}

function readDocument(root, file) {
  const text = readFileSync(path.join(root, file), 'utf8').replace(/\r\n/g, '\n');
  const match = text.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!match) return {body: text, data: null, text};

  const data = new Map();
  for (const line of match[1].split('\n')) {
    const field = line.match(/^([a-z_]+):\s*(.*)$/);
    if (field) data.set(field[1], field[2]);
  }
  return {body: match[2], data, text};
}

function scalar(data, key) {
  const value = data?.get(key)?.trim() ?? '';
  return value.replace(/^['"]|['"]$/g, '');
}

function array(data, key) {
  const value = scalar(data, key);
  if (!value.startsWith('[') || !value.endsWith(']')) return null;
  return value.slice(1, -1).split(',').map((item) => item.trim().replace(/^['"]|['"]$/g, '')).filter(Boolean);
}

function isValidDate(value) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  if (!match) return false;
  const [year, month, day] = match.slice(1).map(Number);
  const parsed = new Date(Date.UTC(year, month - 1, day));
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day;
}

export function slugifyHeading(heading) {
  return heading.toLowerCase().trim()
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .replace(/\s+/g, '-');
}

function headings(body) {
  const result = [];
  let fenced = false;
  for (const [index, line] of body.split('\n').entries()) {
    if (/^\s*```/.test(line)) {
      fenced = !fenced;
      continue;
    }
    if (fenced) continue;
    const match = line.match(/^(#{1,6})\s+(.+?)\s*#*\s*$/);
    if (match) result.push({level: match[1].length, line: index + 1, text: match[2].trim()});
  }
  return result;
}

export function validateMetadata({root, files, today}) {
  const issues = [];
  const topics = new Map();

  for (const file of files) {
    const {body, data} = readDocument(root, file);
    if (!data) {
      issues.push(issue(file, 'missing leading front matter'));
      continue;
    }

    const title = scalar(data, 'title');
    const summary = scalar(data, 'summary');
    const status = scalar(data, 'status');
    const owner = scalar(data, 'owner');
    const reviewed = scalar(data, 'last_reviewed');
    const audience = array(data, 'audience');
    const canonical = array(data, 'canonical_for');
    const tags = array(data, 'tags');

    if (!title) issues.push(issue(file, 'title is required'));
    if (!summary) issues.push(issue(file, 'summary is required'));
    if (!STATUSES.has(status)) issues.push(issue(file, `invalid status ${status || '(missing)'}`));
    if (owner !== 'engram-maintainers') issues.push(issue(file, 'owner must be engram-maintainers'));
    if (!isValidDate(reviewed) || (today && reviewed > today)) issues.push(issue(file, 'invalid last_reviewed'));

    for (const [name, values, allowed] of [
      ['audience', audience, AUDIENCES],
      ['canonical_for', canonical, null],
      ['tags', tags, null],
    ]) {
      if (!values?.length || new Set(values).size !== values.length) {
        issues.push(issue(file, `invalid ${name}`));
      } else if (allowed && values.some((value) => !allowed.has(value))) {
        issues.push(issue(file, `invalid ${name} member`));
      } else if (!allowed && values.some((value) => !SLUG.test(value))) {
        issues.push(issue(file, `invalid ${name} slug`));
      }
    }

    for (const topic of canonical ?? []) {
      if (topics.has(topic)) issues.push(issue(file, `canonical topic ${topic} also belongs to ${topics.get(topic)}`));
      else topics.set(topic, file);
    }

    const outcome = scalar(data, 'outcome');
    const supersededBy = scalar(data, 'superseded_by');
    const canonicalPath = scalar(data, 'canonical_path');
    const feature = scalar(data, 'feature');
    if (status === 'archived' && !outcome && !supersededBy) issues.push(issue(file, 'archived document needs outcome or superseded_by'));
    if (status === 'relocated' && !canonicalPath) issues.push(issue(file, 'relocated document needs canonical_path'));
    if (status !== 'relocated' && canonicalPath) issues.push(issue(file, 'canonical_path is only valid for relocated documents'));
    if (status === 'proposed' && !body.includes('未实现')) issues.push(issue(file, 'proposed document must visibly state 未实现'));
    if (file.includes('/designs/') && !feature) issues.push(issue(file, 'archived design needs feature'));
  }

  return issues;
}

export function validateHeadings({root, files}) {
  const issues = [];
  for (const file of files) {
    const {body, data} = readDocument(root, file);
    const documentHeadings = headings(body);
    const h1 = documentHeadings.filter((heading) => heading.level === 1);
    const title = scalar(data, 'title');
    if (h1.length !== 1) issues.push(issue(file, 'exactly one H1 is required'));
    else if (title && h1[0].text !== title) issues.push(issue(file, 'H1 must match title'));

    let previous = 0;
    const slugs = new Set();
    for (const heading of documentHeadings) {
      if (previous && heading.level > previous + 1) issues.push(issue(file, `heading jumps H${previous}->H${heading.level} at line ${heading.line}`));
      previous = heading.level;
      const slug = slugifyHeading(heading.text);
      if (slugs.has(slug)) issues.push(issue(file, `duplicate heading slug ${slug}`));
      slugs.add(slug);
    }
  }
  return issues;
}

function localLinks(body) {
  const links = [];
  const withoutFences = body.replace(/```[\s\S]*?```/g, '');
  for (const match of withoutFences.matchAll(/!?\[[^\]]*\]\(([^)\s]+)(?:\s+['"][^'"]*['"])?\)/g)) {
    const href = match[1].replace(/^<|>$/g, '');
    if (/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(href)) continue;
    const [target = '', fragment = ''] = href.split('#', 2);
    links.push({target, fragment, href});
  }
  return links;
}

function markdownAnchors(body) {
  const anchors = new Set(headings(body).map((heading) => slugifyHeading(heading.text)));
  for (const match of body.matchAll(/<a\s+(?:name|id)=["']([^"']+)["']/gi)) anchors.add(match[1]);
  return anchors;
}

function resolveLink(root, from, target) {
  if (!target) return from;
  const resolved = path.relative(root, path.resolve(root, path.dirname(from), decodeURIComponent(target)));
  return resolved.split(path.sep).join('/');
}

function isFile(root, file) {
  const fullPath = path.join(root, file);
  return existsSync(fullPath) && statSync(fullPath).isFile();
}

export function validateLinks({root, files}) {
  const issues = [];
  const anchorCache = new Map();
  for (const file of files) {
    const {body} = readDocument(root, file);
    for (const link of localLinks(body)) {
      const target = resolveLink(root, file, link.target);
      if (!isFile(root, target)) {
        issues.push(issue(file, `missing file ${link.href}`));
        continue;
      }
      if (!link.fragment) continue;
      if (!anchorCache.has(target)) anchorCache.set(target, markdownAnchors(readDocument(root, target).body));
      if (!anchorCache.get(target).has(slugifyHeading(link.fragment))) issues.push(issue(file, `missing anchor ${link.href}`));
    }
  }
  return issues;
}

export function validateNavigation({root, files}) {
  const issues = [];
  const known = new Set(files);
  const graph = new Map(files.map((file) => [file, []]));
  const statuses = new Map();
  const incoming = new Map(files.map((file) => [file, 0]));

  for (const file of files) {
    const {body, data} = readDocument(root, file);
    statuses.set(file, scalar(data, 'status'));
    for (const link of localLinks(body)) {
      const target = resolveLink(root, file, link.target);
      if (!known.has(target)) continue;
      graph.get(file).push(target);
      incoming.set(target, (incoming.get(target) ?? 0) + 1);
    }
  }

  const portal = 'docs/README.md';
  const distances = new Map();
  if (known.has(portal)) {
    distances.set(portal, 0);
    const queue = [portal];
    while (queue.length) {
      const from = queue.shift();
      for (const target of graph.get(from) ?? []) {
        if (!distances.has(target)) {
          distances.set(target, distances.get(from) + 1);
          queue.push(target);
        }
      }
    }
  } else {
    issues.push('docs/README.md: portal is missing from navigation set');
  }

  for (const file of files) {
    const status = statuses.get(file);
    if (['stable', 'active', 'proposed'].includes(status) && (!distances.has(file) || distances.get(file) > 2)) {
      issues.push(issue(file, 'not reachable within 2 hops from docs/README.md'));
    }
    if (file !== portal && ['stable', 'active', 'proposed'].includes(status) && incoming.get(file) === 0) {
      issues.push(issue(file, 'orphan document has no inbound documentation link'));
    }
  }

  return issues;
}

function isCurrentStatus(status) {
  return ['stable', 'active', 'proposed'].includes(status);
}

function allDocumentTopics(root, files) {
  return new Map(files.map((file) => {
    const {data} = readDocument(root, file);
    return [file, {status: scalar(data, 'status'), topics: array(data, 'canonical_for') ?? []}];
  }));
}

export function validateRetrieval({root, files, fixtures}) {
  const issues = [];
  const documentTopics = allDocumentTopics(root, files);
  for (const fixture of fixtures) {
    const owner = documentTopics.get(fixture.canonical_path);
    if (!owner) {
      issues.push(`${fixture.id}: canonical path is not a tracked document: ${fixture.canonical_path}`);
      continue;
    }
    const currentOwners = [...documentTopics.entries()]
      .filter(([, document]) => isCurrentStatus(document.status) && document.topics.includes(fixture.topic));
    if (currentOwners.length !== 1 || currentOwners[0][0] !== fixture.canonical_path) {
      issues.push(`${fixture.id}: topic ${fixture.topic} must have exactly one current canonical owner at ${fixture.canonical_path}`);
    }
    if (fixture.forbidden_lifecycles.includes(owner.status)) {
      issues.push(`${fixture.id}: canonical owner has forbidden lifecycle ${owner.status}`);
    }
    const source = readDocument(root, fixture.canonical_path).text;
    for (const assertion of fixture.required_assertions) {
      for (const pattern of assertion.patterns) {
        if (!source.includes(pattern)) issues.push(`${fixture.id}: missing required assertion ${assertion.label}: ${pattern}`);
      }
    }
  }
  return issues;
}

export function validateRelocation({root, files, expectedMappings = EXPECTED_RELOCATIONS}) {
  const issues = [];
  const expectedByLegacy = new Map(expectedMappings.map((mapping) => [mapping.legacy_path, mapping.canonical_path]));
  const relocatedFiles = files.filter((file) => scalar(readDocument(root, file).data, 'status') === 'relocated');

  for (const file of relocatedFiles) {
    if (!expectedByLegacy.has(file)) issues.push(issue(file, 'relocated document has no declared migration mapping'));
  }
  for (const {legacy_path: legacyPath, canonical_path: canonicalPath} of expectedMappings) {
    if (!files.includes(legacyPath)) {
      issues.push(`${legacyPath}: expected relocated document is missing`);
      continue;
    }
    if (!files.includes(canonicalPath)) {
      issues.push(`${legacyPath}: canonical target is missing: ${canonicalPath}`);
      continue;
    }
    const legacy = readDocument(root, legacyPath);
    const target = readDocument(root, canonicalPath);
    if (scalar(legacy.data, 'status') !== 'relocated') issues.push(issue(legacyPath, 'migration source must use relocated status'));
    if (scalar(legacy.data, 'canonical_path') !== canonicalPath) issues.push(issue(legacyPath, `canonical_path must be ${canonicalPath}`));
    if (!isCurrentStatus(scalar(target.data, 'status'))) issues.push(issue(legacyPath, 'canonical target must be stable, active, or proposed'));
    if (scalar(target.data, 'status') === 'relocated') issues.push(issue(legacyPath, 'relocation chains are not allowed'));

    const h1 = headings(legacy.body).filter((heading) => heading.level === 1);
    const links = localLinks(legacy.body);
    if (h1.length !== 1) issues.push(issue(legacyPath, 'relocation body must have one H1'));
    if (links.length !== 1 || resolveLink(root, legacyPath, links[0]?.target ?? '') !== canonicalPath) {
      issues.push(issue(legacyPath, 'relocation body must contain one canonical link'));
    }
    if (legacy.body.trim().length > 500) issues.push(issue(legacyPath, 'relocation body exceeds 500 characters'));
  }
  return issues;
}

function scoreTokens(body) {
  return new Set([...body.matchAll(/\b\d+(?:\.\d+)?%|\b\d+\/\d+\b/g)].map((match) => match[0]));
}

export function validateScoreConsumers({root, consumers = SCORE_CONSUMERS, resultsPath = 'docs/evaluation/results.md'}) {
  const issues = [];
  if (!isFile(root, resultsPath)) return [`${resultsPath}: canonical results document is missing`];
  const scores = scoreTokens(readDocument(root, resultsPath).body);
  for (const consumer of consumers) {
    if (!isFile(root, consumer)) {
      issues.push(`${consumer}: score consumer is missing`);
      continue;
    }
    const document = readDocument(root, consumer);
    const targets = localLinks(document.body).map((link) => resolveLink(root, consumer, link.target));
    if (!targets.includes(resultsPath)) issues.push(issue(consumer, `must link to canonical results ${resultsPath}`));
    for (const score of scores) {
      if (document.body.includes(score)) issues.push(issue(consumer, `duplicated score ${score}; link to canonical results instead`));
    }
  }
  return issues;
}

export function validateCurrentCapabilities({root, file = 'docs/product/capabilities.md', architecturePath = 'docs/architecture/memory-system.md'}) {
  if (!isFile(root, file)) return [`${file}: current capabilities document is missing`];
  const document = readDocument(root, file);
  const required = [
    '新鲜度', '状态一致性', '尚未实现', 'memory-freshness',
    '习惯记忆', '未立项', '未实现', 'habit-memory',
  ];
  const issues = required.filter((pattern) => !document.body.includes(pattern))
    .map((pattern) => issue(file, `missing required boundary ${pattern}`));
  const targets = localLinks(document.body).map((link) => resolveLink(root, file, link.target));
  if (!targets.includes(architecturePath)) issues.push(issue(file, `must link to architecture boundary ${architecturePath}`));
  return issues;
}

function trackedMarkdownFiles(root) {
  return execFileSync('git', ['-C', root, 'ls-files', '--', 'docs'], {encoding: 'utf8'})
    .split('\n').filter((file) => file.endsWith('.md'));
}

export function runChecks({root = process.cwd(), modes = ['all']} = {}) {
  const files = trackedMarkdownFiles(root);
  const selected = new Set(modes.includes('all') ? ['metadata', 'headings', 'links', 'navigation', 'retrieval', 'relocation'] : modes);
  const issues = [];
  if (selected.has('metadata')) issues.push(...validateMetadata({root, files, today: new Date().toISOString().slice(0, 10)}));
  if (selected.has('headings')) issues.push(...validateHeadings({root, files}));
  if (selected.has('links')) issues.push(...validateLinks({root, files}));
  if (selected.has('navigation')) issues.push(...validateNavigation({root, files}));
  if (selected.has('retrieval')) {
    const fixtures = JSON.parse(readFileSync(path.join(root, 'docs/validation/retrieval-fixtures.json'), 'utf8'));
    issues.push(...validateRetrieval({root, files, fixtures}));
    issues.push(...validateScoreConsumers({root}));
    issues.push(...validateCurrentCapabilities({root}));
  }
  if (selected.has('relocation')) issues.push(...validateRelocation({root, files}));
  return issues;
}

function runCli() {
  const allowed = new Set(['all', 'metadata', 'headings', 'links', 'navigation', 'retrieval', 'relocation']);
  const modes = process.argv.slice(2).map((argument) => argument.replace(/^--/, ''));
  if (modes.some((mode) => !allowed.has(mode))) {
    process.stderr.write(`Usage: node docs/validation/check-docs.mjs [--all|--metadata|--headings|--links|--navigation|--retrieval|--relocation]\n`);
    process.exitCode = 2;
    return;
  }
  const issues = runChecks({modes: modes.length ? modes : ['all']});
  if (issues.length) {
    process.stderr.write(`${issues.join('\n')}\n`);
    process.exitCode = 1;
  } else {
    process.stdout.write('Documentation validation passed.\n');
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) runCli();
