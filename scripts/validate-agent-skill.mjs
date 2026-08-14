import { createHash } from "node:crypto";
import {
  existsSync,
  lstatSync,
  readdirSync,
  readFileSync,
  realpathSync,
  statSync,
} from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

export const DIGEST_ALGORITHM = "engram-package-sha256-v1";
export const TOKEN_ALGORITHM = "engram-body-token-estimate-v1";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const expectedMcpTools = [
  "memory_delete",
  "memory_evidence_get",
  "memory_evidence_purge",
  "memory_evidence_restore",
  "memory_evidence_tombstone",
  "memory_get",
  "memory_ingest_v2",
  "memory_list",
  "memory_search",
  "memory_write",
];
const expectedCliCommands = [
  "add",
  "curate",
  "delete",
  "export",
  "get",
  "ingest",
  "list",
  "namespaces",
  "search",
  "stats",
  "version",
];
const expectedIntents = [
  ["write", true, "memory_write", "add", "explicit user intent"],
  ["search", false, "memory_search", "search", null],
  ["get", false, "memory_get", "get", null],
  ["list", false, "memory_list", "list", null],
  ["delete", true, "memory_delete", "delete", "explicit user intent"],
  ["ingest", true, "memory_ingest_v2", "ingest", "explicit intent + stable session and source IDs"],
  ["curate", true, null, "curate", "explicit intent + LLM"],
  ["stats", false, null, "stats", "CLI store identity confirmed"],
  ["export", false, null, "export", "CLI store identity confirmed"],
  ["namespace-discovery", false, null, "namespaces", "CLI data dir confirmed"],
  ["version", false, null, "version", null],
];
const requiredPackageFiles = [
  "SKILL.md",
  "LICENSE",
  "references/mcp.md",
  "references/cli.md",
  "references/install.md",
  "references/contract.json",
  "evals/evals.json",
  "evals/trigger-evals.json",
];
const requiredSkillReferences = [
  "references/mcp.md",
  "references/cli.md",
  "references/install.md",
  "references/contract.json",
];
const requiredEvalTags = new Set([
  "mcp-only",
  "cli-only",
  "both-surfaces",
  "no-surface",
  "offline",
  "missing-embedding",
  "missing-llm",
  "invalid-namespace",
  "secret-input",
  "empty-result",
  "not-found",
  "cross-store-mismatch",
  "conditional-ingest",
]);
const skillFileSkipDirectories = new Set([".git", "node_modules", ".claude", ".agents", ".codex"]);

export function normalizeLineEndings(value) {
  return value.replace(/\r\n|\r/g, "\n");
}

export function estimateBodyTokens(source) {
  const normalized = normalizeLineEndings(source);
  let asciiCodePoints = 0;
  let nonAsciiCodePoints = 0;
  for (const codePoint of normalized) {
    if (/\s/u.test(codePoint)) {
      continue;
    }
    if (codePoint.codePointAt(0) <= 0x7f) {
      asciiCodePoints += 1;
    } else {
      nonAsciiCodePoints += 1;
    }
  }
  const lines = normalized === "" ? 0 : normalized.endsWith("\n") ? normalized.split("\n").length - 1 : normalized.split("\n").length;
  return {
    lines,
    asciiCodePoints,
    nonAsciiCodePoints,
    tokens: Math.ceil(asciiCodePoints / 4) + nonAsciiCodePoints,
  };
}

function decodeText(buffer, label) {
  if (buffer.includes(0)) {
    throw new Error(`${label} contains NUL`);
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(buffer);
  } catch {
    throw new Error(`${label} is not valid UTF-8`);
  }
}

function resolveDirectory(pathValue, label) {
  const stat = lstatSync(pathValue);
  if (!stat.isDirectory() && !stat.isSymbolicLink()) {
    throw new Error(`${label} must be a directory or root symlink`);
  }
  const resolved = realpathSync(pathValue);
  if (!statSync(resolved).isDirectory()) {
    throw new Error(`${label} must resolve to a directory`);
  }
  return resolved;
}

function collectPackageFiles(packageRoot) {
  const files = [];
  const visit = (directory, prefix = "") => {
    for (const entry of readdirSync(directory, { withFileTypes: true }).sort((a, b) => Buffer.compare(Buffer.from(a.name), Buffer.from(b.name)))) {
      const relativePath = prefix === "" ? entry.name : `${prefix}/${entry.name}`;
      const fullPath = join(directory, entry.name);
      if (entry.isSymbolicLink()) {
        throw new Error(`internal symlink is not allowed: ${relativePath}`);
      }
      if (entry.isDirectory()) {
        visit(fullPath, relativePath);
      } else if (entry.isFile()) {
        files.push({ relativePath, fullPath });
      } else {
        throw new Error(`unsupported package entry: ${relativePath}`);
      }
    }
  };
  visit(packageRoot);
  return files.sort((a, b) => Buffer.compare(Buffer.from(a.relativePath), Buffer.from(b.relativePath)));
}

export function calculatePackageDigest(packageRoot) {
  const resolvedRoot = resolveDirectory(packageRoot, "package root");
  const files = collectPackageFiles(resolvedRoot);
  if (files.length === 0) {
    throw new Error("package must not be empty");
  }
  const hash = createHash("sha256");
  for (const file of files) {
    const normalized = Buffer.from(normalizeLineEndings(decodeText(readFileSync(file.fullPath), file.relativePath)), "utf8");
    hash.update(Buffer.from(file.relativePath, "utf8"));
    hash.update(Buffer.from([0]));
    hash.update(Buffer.from(String(normalized.length), "ascii"));
    hash.update(Buffer.from([0]));
    hash.update(normalized);
    hash.update(Buffer.from([0]));
  }
  return {
    algorithm: DIGEST_ALGORITHM,
    digest: hash.digest("hex"),
    files: files.map((file) => file.relativePath),
  };
}

function parseFrontmatter(source) {
  const normalized = normalizeLineEndings(source);
  const lines = normalized.split("\n");
  if (lines[0] !== "---") {
    throw new Error("SKILL.md must begin with YAML frontmatter");
  }
  let closingIndex = -1;
  for (let index = 1; index < lines.length; index += 1) {
    if (lines[index] === "---") {
      closingIndex = index;
      break;
    }
  }
  if (closingIndex < 0) {
    throw new Error("SKILL.md frontmatter is not closed");
  }

  const fields = {};
  for (let index = 1; index < closingIndex; index += 1) {
    const line = lines[index];
    if (line.trim() === "") {
      continue;
    }
    const match = /^([A-Za-z][A-Za-z0-9_-]*):(?:[ \t]*(.*))?$/.exec(line);
    if (!match) {
      throw new Error(`invalid frontmatter line ${index + 1}`);
    }
    const [, key, rawValue = ""] = match;
    if (Object.hasOwn(fields, key)) {
      throw new Error(`duplicate frontmatter key: ${key}`);
    }
    if ([">", ">-", "|", "|-"].includes(rawValue)) {
      const folded = rawValue.startsWith(">");
      const chunks = [];
      index += 1;
      while (index < closingIndex && /^[ \t]/.test(lines[index])) {
        chunks.push(lines[index].trim());
        index += 1;
      }
      index -= 1;
      if (chunks.length === 0) {
        throw new Error(`frontmatter ${key} must not be empty`);
      }
      fields[key] = folded ? chunks.join(" ") : chunks.join("\n");
    } else {
      if (rawValue === "") {
        throw new Error(`frontmatter ${key} must not be empty`);
      }
      fields[key] = rawValue.replace(/^(["'])(.*)\1$/, "$2");
    }
  }
  return { fields, body: lines.slice(closingIndex + 1).join("\n") };
}

function addError(errors, message) {
  if (!errors.includes(message)) {
    errors.push(message);
  }
}

function readPackageText(packageRoot, relativePath, errors) {
  const fullPath = join(packageRoot, relativePath);
  if (!existsSync(fullPath)) {
    addError(errors, `missing required package file: ${relativePath}`);
    return null;
  }
  try {
    const stat = lstatSync(fullPath);
    if (!stat.isFile()) {
      addError(errors, `package file is not a regular file: ${relativePath}`);
      return null;
    }
    return normalizeLineEndings(decodeText(readFileSync(fullPath), relativePath));
  } catch (error) {
    addError(errors, error.message);
    return null;
  }
}

function validateDescription(description, errors) {
  if (typeof description !== "string" || description.trim() === "") {
    addError(errors, "frontmatter description must be a non-empty scalar");
    return;
  }
  if ([...description].length > 1024) {
    addError(errors, "frontmatter description exceeds 1024 Unicode code points");
  }
  const semanticRequirements = [
    ["engram", /engram/i],
    ["MCP", /\bmcp\b/i],
    ["CLI", /\bcli\b/i],
    ["remember/记住", /remember|记住/i],
    ["recall/召回", /recall|召回/i],
    ["search", /search/i],
    ["inspect/get", /inspect|\bget\b/i],
    ["list", /list/i],
    ["delete", /delete/i],
    ["ingest", /ingest/i],
    ["curation", /curat/i],
    ["stats", /stats?/i],
    ["export", /export/i],
    ["namespace", /namespace/i],
    ["version", /version/i],
    ["persistent memory", /persistent|cross-session|长期记忆/i],
    ["offline", /offline|local-first|本地/i],
    ["secret", /secret|秘密/i],
    ["RAM", /\bram\b/i],
    ["cache", /cache/i],
    ["database", /database|数据库/i],
    ["transient context", /transient|临时.*上下文/i],
  ];
  for (const [name, pattern] of semanticRequirements) {
    if (!pattern.test(description)) {
      addError(errors, `frontmatter description is missing required semantic: ${name}`);
    }
  }
}

function validateSkillReferences(packageRoot, source, errors) {
  const found = new Set();
  const markdownLink = /\[[^\]]+\]\(([^)]+)\)/g;
  for (const match of source.matchAll(markdownLink)) {
    const rawTarget = match[1].trim();
    const target = rawTarget.split(/[?#]/, 1)[0];
    if (target === "" || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(target)) {
      continue;
    }
    if (isAbsolute(target) || target.split("/").includes("..") || target.split("\\").includes("..")) {
      addError(errors, `reference escapes the package: ${rawTarget}`);
      continue;
    }
    if (target.startsWith("references/") && target.split("/").length !== 2) {
      addError(errors, `reference is not one hop from SKILL.md: ${rawTarget}`);
    }
    const resolved = resolve(packageRoot, target);
    const pathFromRoot = relative(packageRoot, resolved);
    if (pathFromRoot === "" || pathFromRoot === ".." || pathFromRoot.startsWith(`..${sep}`)) {
      addError(errors, `reference escapes the package: ${rawTarget}`);
      continue;
    }
    if (!existsSync(resolved) || !lstatSync(resolved).isFile()) {
      addError(errors, `missing referenced file: ${target}`);
      continue;
    }
    found.add(target.replaceAll("\\", "/"));
  }
  for (const requiredReference of requiredSkillReferences) {
    if (!found.has(requiredReference)) {
      addError(errors, `SKILL.md must directly reference ${requiredReference}`);
    }
  }
}

function parseJson(text, label, errors) {
  if (text === null) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    addError(errors, `${label} is not valid JSON: ${error.message}`);
    return null;
  }
}

function sameArray(actual, expected) {
  return Array.isArray(actual) && actual.length === expected.length && actual.every((value, index) => value === expected[index]);
}

function validateManifest(manifest, errors) {
  if (manifest === null || typeof manifest !== "object" || Array.isArray(manifest)) {
    addError(errors, "contract.json must be an object");
    return;
  }
  if (manifest.schema_version !== 1) {
    addError(errors, "contract.json schema_version must equal 1");
  }
  if (manifest.skill?.name !== "engram") {
    addError(errors, "contract.json skill.name must equal engram");
  }
  if (typeof manifest.skill?.version !== "string" || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(manifest.skill.version)) {
    addError(errors, "contract.json skill.version must be semver");
  }
  if (!sameArray(manifest.mcp?.always, expectedMcpTools)) {
    addError(errors, "contract.json mcp.always must be the sorted runtime tool set");
  }
  if (manifest.mcp?.conditional?.memory_ingest !== "llm" || Object.keys(manifest.mcp?.conditional || {}).length !== 1) {
    addError(errors, "contract.json mcp.conditional must contain only memory_ingest: llm");
  }
  if (!sameArray(manifest.cli?.commands, expectedCliCommands)) {
    addError(errors, "contract.json cli.commands must be the sorted runtime command set");
  }
  if (!Array.isArray(manifest.intents) || manifest.intents.length !== expectedIntents.length) {
    addError(errors, "contract.json intents must contain every required intent exactly once");
    return;
  }
  const seen = new Set();
  for (let index = 0; index < expectedIntents.length; index += 1) {
    const [name, mutating, mcp, cli, condition] = expectedIntents[index];
    const actual = manifest.intents[index];
    if (!actual || actual.name !== name || actual.mutating !== mutating || actual.mcp !== mcp || actual.cli !== cli || actual.condition !== condition) {
      addError(errors, `contract.json intent ${name} does not match the frozen routing contract`);
    }
    if (seen.has(actual?.name)) {
      addError(errors, `contract.json has duplicate intent: ${actual.name}`);
    }
    seen.add(actual?.name);
  }
}

function validateEvals(evals, triggers, errors) {
  if (evals === null || typeof evals !== "object" || Array.isArray(evals)) {
    addError(errors, "evals.json must be an object");
  } else {
    if (evals.skill_name !== "engram") {
      addError(errors, "evals.json skill_name must equal engram");
    }
    if (!Array.isArray(evals.evals) || evals.evals.length < 12) {
      addError(errors, "evals.json must contain at least 12 behavior evals");
    } else {
      const ids = new Set();
      const coveredTags = new Set();
      for (const entry of evals.evals) {
        if (entry === null || typeof entry !== "object" || Array.isArray(entry)) {
          addError(errors, "evals.json contains an invalid eval object");
          continue;
        }
        if (typeof entry.id !== "string" && typeof entry.id !== "number") {
          addError(errors, "evals.json eval id must be a string or number");
        } else if (ids.has(entry.id)) {
          addError(errors, `evals.json has duplicate eval id: ${entry.id}`);
        } else {
          ids.add(entry.id);
        }
        if (typeof entry.prompt !== "string" || entry.prompt.trim() === "" || typeof entry.expected_output !== "string" || entry.expected_output.trim() === "" || !Array.isArray(entry.expectations) || entry.expectations.length === 0) {
          addError(errors, `evals.json eval ${entry.id} lacks prompt, expected_output, or expectations`);
        }
        for (const tag of entry.tags || []) {
          coveredTags.add(tag);
        }
      }
      for (const requiredTag of requiredEvalTags) {
        if (!coveredTags.has(requiredTag)) {
          addError(errors, `evals.json is missing required coverage tag: ${requiredTag}`);
        }
      }
    }
  }

  if (!Array.isArray(triggers) || triggers.length < 20) {
    addError(errors, "trigger-evals.json must contain at least 20 cases");
    return;
  }
  let positives = 0;
  let negatives = 0;
  for (const entry of triggers) {
    if (entry === null || typeof entry !== "object" || typeof entry.query !== "string" || typeof entry.should_trigger !== "boolean") {
      addError(errors, "trigger-evals.json contains an invalid trigger case");
      continue;
    }
    if (entry.should_trigger) {
      positives += 1;
    } else {
      negatives += 1;
    }
    if (/\bengram\b/i.test(entry.query) && entry.should_trigger !== true) {
      addError(errors, "explicit engram trigger case must load the skill");
    }
  }
  if (positives < 8 || negatives < 8) {
    addError(errors, "trigger-evals.json must contain at least 8 positive and 8 negative cases");
  }
  if (positives / triggers.length < 0.4 || negatives / triggers.length < 0.4) {
    addError(errors, "trigger-evals.json must be balanced between positive and near-miss cases");
  }
}

function containsSecretShapedText(text) {
  return /\b(?:sk|rk|pk)_[A-Za-z0-9_-]{16,}\b|\b(?:api[_-]?key|token|password|secret)\s*[:=]\s*["']?[A-Za-z0-9_-]{8,}/i.test(text);
}

function containsHostedRerankerRecommendation(text) {
  return text.split(/[.!?]+/).some((sentence) => {
    if (!/\b(?:hosted|cloud|remote)\b/i.test(sentence) || !/\b(?:reranker|recall model)\b/i.test(sentence)) {
      return false;
    }
    if (/\b(?:never|do not|don't|must not|avoid|prohibit|forbid)\b/i.test(sentence)) {
      return false;
    }
    return /\b(?:recommend|use|configure|enable|install|require)\b/i.test(sentence);
  });
}

function containsUnsafeNamespaceExample(text) {
  return /(?:--namespace\s+|["']namespace["']\s*[:=]\s*|namespace\s*=\s*)["']?(?:\.{1,2}(?:[/\\]|$)|[^\s"']*[/\\][^\s"']*)/i.test(text);
}

function findCanonicalEngramSkills(repositoryRoot) {
  const matches = [];
  const visit = (directory, prefix = "") => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.isSymbolicLink()) {
        continue;
      }
      const relativePath = prefix === "" ? entry.name : `${prefix}/${entry.name}`;
      const fullPath = join(directory, entry.name);
      if (entry.isDirectory()) {
        if (!skillFileSkipDirectories.has(entry.name)) {
          visit(fullPath, relativePath);
        }
      } else if (entry.isFile() && entry.name === "SKILL.md") {
        try {
          if (parseFrontmatter(decodeText(readFileSync(fullPath), relativePath)).fields.name === "engram") {
            matches.push(relativePath);
          }
        } catch {
          // The package-specific validation reports malformed canonical files.
        }
      }
    }
  };
  visit(repositoryRoot);
  return matches.sort((a, b) => Buffer.compare(Buffer.from(a), Buffer.from(b)));
}

function extractQuickCommand(text) {
  for (const match of text.matchAll(/```(?:bash|sh)?\s*\n([\s\S]*?)```/g)) {
    const normalized = match[1].replace(/\\\s*\n/g, " ").replace(/\s+/g, " ").trim();
    const start = normalized.indexOf("npx --yes skills@1.5.20 add ");
    if (start >= 0) {
      return normalized.slice(start);
    }
  }
  return null;
}

function validateDocumentation(repositoryRoot, packageRoot, manifest, mode, errors) {
  const userFacingFiles = [
    ["package install reference", join(packageRoot, "references", "install.md")],
    ["README.md", join(repositoryRoot, "README.md")],
    ["README.zh-CN.md", join(repositoryRoot, "README.zh-CN.md")],
    ["docs/README.md", join(repositoryRoot, "docs", "README.md")],
  ];
  const commands = [];
  for (const [label, filePath] of userFacingFiles) {
    if (!existsSync(filePath)) {
      addError(errors, `missing user-facing documentation: ${label}`);
      continue;
    }
    const text = normalizeLineEndings(decodeText(readFileSync(filePath), label));
    const command = extractQuickCommand(text);
    if (command === null) {
      addError(errors, `${label} is missing the canonical quick command`);
      continue;
    }
    commands.push([label, command, text]);
  }
  if (commands.length === 0) {
    return;
  }
  const canonical = commands[0][1];
  for (const [label, command] of commands.slice(1)) {
    if (command !== canonical) {
      addError(errors, `${label} quick command differs from the canonical quick command`);
    }
  }
  if (!canonical.startsWith("npx --yes skills@1.5.20 add ")) {
    addError(errors, "quick command must pin skills@1.5.20");
  }
  for (const agent of ["claude-code", "codex", "opencode"]) {
    if (!canonical.includes(`--agent ${agent}`)) {
      addError(errors, `quick command is missing --agent ${agent}`);
    }
  }
  if (!canonical.includes("--global")) {
    addError(errors, "default quick command must select user scope with --global");
  }
  const commandTail = canonical.slice(canonical.indexOf(" add ") + 5);
  if (/(?:^|\s)(?:--yes|-y)(?:\s|$)/.test(commandTail)) {
    addError(errors, "default quick command must not have a trailing --yes or -y");
  }

  if (mode !== "release") {
    return;
  }
  const expectedTag = `engram-skill-v${manifest?.skill?.version || ""}`;
  const expectedUrl = `https://github.com/wallfacers/engram/tree/${expectedTag}/skills/engram`;
  for (const [label, command, text] of commands) {
    if (text.includes("<ENGRAM_SKILL_TAG>")) {
      addError(errors, `release tag placeholder remains in ${label}`);
    }
    if (!command.includes(expectedUrl)) {
      addError(errors, `${label} must use release URL ${expectedUrl}`);
    }
    if (/\/tree\/(?:main|master|HEAD)\//.test(command)) {
      addError(errors, `${label} contains a mutable branch reference`);
    }
    if (/\/tree\/[a-f0-9]{40}\//i.test(command)) {
      addError(errors, `${label} contains a commit-SHA self-reference`);
    }
  }
}

export function validateSkillPackage({ repositoryRoot = resolve(scriptDirectory, ".."), packageRoot = join(repositoryRoot, "skills", "engram"), mode = "source" } = {}) {
  const errors = [];
  const resolvedRepositoryRoot = resolveDirectory(repositoryRoot, "repository root");
  let resolvedPackageRoot;
  let digest = null;
  try {
    resolvedPackageRoot = resolveDirectory(packageRoot, "package root");
    digest = calculatePackageDigest(resolvedPackageRoot);
  } catch (error) {
    addError(errors, error.message);
    return { errors, metrics: null };
  }

  for (const requiredPath of requiredPackageFiles) {
    if (!digest.files.includes(requiredPath)) {
      addError(errors, `missing required package file: ${requiredPath}`);
    }
  }

  let skillSource = readPackageText(resolvedPackageRoot, "SKILL.md", errors);
  let frontmatter = null;
  if (skillSource !== null) {
    try {
      frontmatter = parseFrontmatter(skillSource);
      const keys = Object.keys(frontmatter.fields).sort();
      for (const key of keys) {
        if (key !== "name" && key !== "description") {
          addError(errors, `unknown frontmatter key: ${key}`);
        }
      }
      if (frontmatter.fields.name !== "engram") {
        addError(errors, "frontmatter name must equal engram");
      }
      if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(frontmatter.fields.name || "") || (frontmatter.fields.name || "").length > 64) {
        addError(errors, "frontmatter name does not meet the portable name constraint");
      }
      validateDescription(frontmatter.fields.description, errors);
      validateSkillReferences(resolvedPackageRoot, skillSource, errors);
    } catch (error) {
      addError(errors, error.message);
    }
  }

  const bodyMetrics = estimateBodyTokens(skillSource || "");
  if (bodyMetrics.lines > 500) {
    addError(errors, `SKILL.md exceeds 500 normalized lines (${bodyMetrics.lines})`);
  }
  if (bodyMetrics.tokens > 5000) {
    addError(errors, `SKILL.md exceeds 5000 ${TOKEN_ALGORITHM} tokens (${bodyMetrics.tokens})`);
  }

  const license = readPackageText(resolvedPackageRoot, "LICENSE", errors);
  const rootLicensePath = join(resolvedRepositoryRoot, "LICENSE");
  if (!existsSync(rootLicensePath)) {
    addError(errors, "repository root LICENSE is missing");
  } else if (license !== null) {
    try {
      if (license !== normalizeLineEndings(decodeText(readFileSync(rootLicensePath), "repository LICENSE"))) {
        addError(errors, "package LICENSE differs from the repository root LICENSE after LF normalization");
      }
    } catch (error) {
      addError(errors, error.message);
    }
  }

  const manifest = parseJson(readPackageText(resolvedPackageRoot, "references/contract.json", errors), "contract.json", errors);
  validateManifest(manifest, errors);
  const evals = parseJson(readPackageText(resolvedPackageRoot, "evals/evals.json", errors), "evals.json", errors);
  const triggers = parseJson(readPackageText(resolvedPackageRoot, "evals/trigger-evals.json", errors), "trigger-evals.json", errors);
  validateEvals(evals, triggers, errors);

  for (const relativePath of digest.files) {
    try {
      const text = decodeText(readFileSync(join(resolvedPackageRoot, relativePath)), relativePath);
      if (containsSecretShapedText(text)) {
        addError(errors, `secret-shaped value found in ${relativePath}`);
      }
      if (containsHostedRerankerRecommendation(text)) {
        addError(errors, `hosted reranker recommendation found in ${relativePath}`);
      }
      if (containsUnsafeNamespaceExample(text)) {
        addError(errors, `unsafe namespace example found in ${relativePath}`);
      }
    } catch (error) {
      addError(errors, error.message);
    }
  }

  const canonicalSkills = findCanonicalEngramSkills(resolvedRepositoryRoot);
  if (canonicalSkills.length !== 1 || canonicalSkills[0] !== "skills/engram/SKILL.md") {
    addError(errors, `expected exactly one canonical engram SKILL.md at skills/engram/SKILL.md; found: ${canonicalSkills.join(", ") || "none"}`);
  }
  validateDocumentation(resolvedRepositoryRoot, resolvedPackageRoot, manifest, mode, errors);

  return {
    errors,
    metrics: {
      algorithm: DIGEST_ALGORITHM,
      digest: digest.digest,
      files: digest.files,
      lines: bodyMetrics.lines,
      tokenAlgorithm: TOKEN_ALGORITHM,
      tokenEstimate: bodyMetrics.tokens,
    },
  };
}

function usage() {
  return "usage: node scripts/validate-agent-skill.mjs [--source|--release] [--root <repository>] [--package <package>]";
}

function main(argv) {
  let mode = "source";
  let repositoryRoot = resolve(scriptDirectory, "..");
  let packageRoot = null;
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--source") {
      mode = "source";
    } else if (argument === "--release") {
      mode = "release";
    } else if (argument === "--root" || argument === "--package") {
      const value = argv[index + 1];
      if (!value) {
        throw new Error(`${argument} requires a value`);
      }
      if (argument === "--root") {
        repositoryRoot = resolve(value);
      } else {
        packageRoot = resolve(value);
      }
      index += 1;
    } else {
      throw new Error(`unknown argument: ${argument}`);
    }
  }
  const result = validateSkillPackage({ repositoryRoot, packageRoot: packageRoot || join(repositoryRoot, "skills", "engram"), mode });
  if (result.errors.length > 0) {
    for (const error of result.errors) {
      console.error(`error: ${error}`);
    }
    process.exitCode = 1;
    return;
  }
  console.log(JSON.stringify({ status: "ok", mode, ...result.metrics }, null, 2));
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(`error: ${error.message}`);
    console.error(usage());
    process.exitCode = 2;
  }
}
