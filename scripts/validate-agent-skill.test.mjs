import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import test from "node:test";

import {
  calculatePackageDigest,
  estimateBodyTokens,
  validateSkillPackage,
} from "./validate-agent-skill.mjs";

const scriptDirectory = dirname(new URL(import.meta.url).pathname);
const repositoryRoot = resolve(scriptDirectory, "..");
const scratchBase = process.env.ENGRAM_SKILL_TEST_SCRATCH || tmpdir();

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

const intentRows = [
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

function writeText(filePath, content) {
  mkdirSync(dirname(filePath), { recursive: true });
  writeFileSync(filePath, content, "utf8");
}

function makeScratchDirectory(t) {
  mkdirSync(scratchBase, { recursive: true });
  const directory = mkdtempSync(join(scratchBase, "engram-skill-validator-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

function canonicalQuickCommand(tag = "<ENGRAM_SKILL_TAG>") {
  return `npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/${tag}/skills/engram --global`;
}

function validManifest() {
  return {
    schema_version: 1,
    skill: { name: "engram", version: "0.1.0" },
    mcp: {
      always: [...expectedMcpTools],
      conditional: { memory_ingest: "llm" },
    },
    cli: { commands: [...expectedCliCommands] },
    intents: intentRows.map(([name, mutating, mcp, cli, condition]) => ({
      name,
      mutating,
      mcp,
      cli,
      condition,
    })),
  };
}

function validEvals() {
  const tags = [
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
  ];
  return {
    skill_name: "engram",
    evals: tags.map((tag, index) => ({
      id: `workflow-${index + 1}`,
      prompt: `Exercise the ${tag} workflow without inventing an engram result.`,
      expected_output: "Reports one real surface and evidence or an honest block.",
      files: [],
      expectations: ["Uses an actual documented engram path"],
      tags: [tag],
    })),
  };
}

function validTriggers() {
  const positive = [
    "Use engram to remember that I prefer tea.",
    "请用 engram 记住我的工作时区。",
    "Recall the preference I saved with engram yesterday.",
    "Search my engram long-term memories for travel facts.",
    "Inspect an engram memory entry by name.",
    "List the memories stored in engram.",
    "Delete this saved engram fact.",
    "Ingest this conversation into engram after I confirm it.",
    "Run engram curation for this namespace.",
    "What engram namespace and version am I using?",
  ];
  const negative = [
    "How much RAM does this process use?",
    "Clear the browser cache.",
    "Design a PostgreSQL database schema.",
    "Keep this only in transient chat context.",
    "Explain CPU cache locality.",
    "What is the memory layout of a Go struct?",
    "Tune Redis cache eviction.",
    "Summarize the conversation without saving anything.",
    "Compare database indexes.",
    "How can I free RAM on Linux?",
  ];
  return [
    ...positive.map((query) => ({ query, should_trigger: true })),
    ...negative.map((query) => ({ query, should_trigger: false })),
  ];
}

function validSkillBody(extra = "") {
  return `---
name: engram
description: >-
  Use engram local-first persistent memory through MCP tools or the CLI whenever a
  user asks to remember/记住, recall/召回, search, inspect or get saved facts, list,
  delete, ingest conversations, curate, inspect stats, export, inspect namespaces,
  or diagnose the version. Preserve namespace isolation, offline behavior, and
  secret safety; do not use for ordinary RAM, cache, generic database, or transient
  chat context.
---

# engram memory workflow

Use this skill only for an explicit persistent or cross-session memory request.
Do not persist ordinary conversation automatically. Preflight the connected MCP
tools, then the \`engram version\` CLI probe. Choose one namespace and one surface;
prefer a connected MCP tool and use the CLI only when that operation is unavailable.
Require explicit intent for write, delete, ingest, and curate. Stop before writing
secrets, reject invalid namespaces, and report actual evidence, degradation, or an
honest block.

Read [the MCP reference](references/mcp.md) for tool inputs and errors, [the CLI
reference](references/cli.md) for commands and flags, [the installation reference](references/install.md)
for setup, and [the machine contract](references/contract.json) for the exact public sets.
${extra}`;
}

function createValidFixture(t, options = {}) {
  const root = makeScratchDirectory(t);
  const packageRoot = join(root, "skills", "engram");
  const tag = options.tag || "<ENGRAM_SKILL_TAG>";
  const quickCommand = canonicalQuickCommand(tag);

  writeText(join(root, "LICENSE"), readFileSync(join(repositoryRoot, "LICENSE"), "utf8"));
  writeText(join(packageRoot, "SKILL.md"), options.skillBody || validSkillBody());
  writeText(join(packageRoot, "LICENSE"), readFileSync(join(repositoryRoot, "LICENSE"), "utf8"));
  writeText(join(packageRoot, "references", "mcp.md"), "# MCP\nUse only the documented MCP tools.\n");
  writeText(join(packageRoot, "references", "cli.md"), "# CLI\nPut global flags before the command.\n");
  writeText(join(packageRoot, "references", "install.md"), `# Install\n\n\`\`\`bash\n${quickCommand}\n\`\`\`\n`);
  writeText(join(packageRoot, "references", "contract.json"), `${JSON.stringify(validManifest(), null, 2)}\n`);
  writeText(join(packageRoot, "evals", "evals.json"), `${JSON.stringify(validEvals(), null, 2)}\n`);
  writeText(join(packageRoot, "evals", "trigger-evals.json"), `${JSON.stringify(validTriggers(), null, 2)}\n`);
  for (const relativePath of ["README.md", "README.zh-CN.md", join("docs", "README.md")]) {
    writeText(join(root, relativePath), `# engram\n\n\`\`\`bash\n${quickCommand}\n\`\`\`\n`);
  }
  return { root, packageRoot };
}

function expectError(result, text) {
  assert.ok(result.errors.some((error) => error.includes(text)), `${text}: ${result.errors.join(" | ")}`);
}

test("valid source package satisfies the frozen format contract", (t) => {
  const { root, packageRoot } = createValidFixture(t);
  const result = validateSkillPackage({ repositoryRoot: root, packageRoot, mode: "source" });

  assert.deepEqual(result.errors, []);
  assert.equal(result.metrics.algorithm, "engram-package-sha256-v1");
  assert.match(result.metrics.digest, /^[a-f0-9]{64}$/);
  assert.ok(result.metrics.lines > 0);
  assert.ok(result.metrics.tokenEstimate > 0);
});

test("validator rejects bad frontmatter, references, schema, evals, and secret-shaped fixtures", (t) => {
  const badFrontmatter = createValidFixture(t, {
    skillBody: validSkillBody().replace("name: engram", "name: engram\ncompatibility: private"),
  });
  expectError(
    validateSkillPackage({ repositoryRoot: badFrontmatter.root, packageRoot: badFrontmatter.packageRoot, mode: "source" }),
    "unknown frontmatter key",
  );

  const badReference = createValidFixture(t, {
    skillBody: validSkillBody().replace("references/mcp.md", "../outside.md"),
  });
  expectError(
    validateSkillPackage({ repositoryRoot: badReference.root, packageRoot: badReference.packageRoot, mode: "source" }),
    "escapes the package",
  );

  const badManifest = createValidFixture(t);
  const manifestPath = join(badManifest.packageRoot, "references", "contract.json");
  const manifest = validManifest();
  manifest.cli.commands.pop();
  writeText(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  expectError(
    validateSkillPackage({ repositoryRoot: badManifest.root, packageRoot: badManifest.packageRoot, mode: "source" }),
    "cli.commands",
  );

  const badEvals = createValidFixture(t);
  const evalPath = join(badEvals.packageRoot, "evals", "evals.json");
  const evals = validEvals();
  evals.evals[1].id = evals.evals[0].id;
  writeText(evalPath, `${JSON.stringify(evals, null, 2)}\n`);
  expectError(
    validateSkillPackage({ repositoryRoot: badEvals.root, packageRoot: badEvals.packageRoot, mode: "source" }),
    "duplicate eval id",
  );

  const syntheticSecret = `sk_${"abcdefghijkl"}${"mnopqrstuvwx"}`;
  const secretFixture = createValidFixture(t, {
    skillBody: `${validSkillBody()}\nExample token=${syntheticSecret}\n`,
  });
  expectError(
    validateSkillPackage({ repositoryRoot: secretFixture.root, packageRoot: secretFixture.packageRoot, mode: "source" }),
    "secret-shaped",
  );
});

test("engram-body-token-estimate-v1 handles line endings, whitespace, and Unicode deterministically", () => {
  assert.deepEqual(estimateBodyTokens("a \t猫\r\n"), {
    lines: 1,
    asciiCodePoints: 1,
    nonAsciiCodePoints: 1,
    tokens: 2,
  });
  assert.deepEqual(estimateBodyTokens("abcd\n猫"), {
    lines: 2,
    asciiCodePoints: 4,
    nonAsciiCodePoints: 1,
    tokens: 2,
  });
  assert.deepEqual(estimateBodyTokens(""), {
    lines: 0,
    asciiCodePoints: 0,
    nonAsciiCodePoints: 0,
    tokens: 0,
  });
});

function independentDigest(files) {
  const hash = createHash("sha256");
  for (const [relativePath, content] of [...files].sort(([a], [b]) => Buffer.compare(Buffer.from(a), Buffer.from(b)))) {
    const normalized = Buffer.from(content.replace(/\r\n|\r/g, "\n"), "utf8");
    hash.update(relativePath);
    hash.update("\0");
    hash.update(String(normalized.length));
    hash.update("\0");
    hash.update(normalized);
    hash.update("\0");
  }
  return hash.digest("hex");
}

test("engram-package-sha256-v1 is stable for ordering, line endings, and root symlinks", (t) => {
  const root = makeScratchDirectory(t);
  const first = join(root, "first");
  const second = join(root, "second");
  writeText(join(first, "z.txt"), "z\r\n");
  writeText(join(first, "a.txt"), "猫\r");
  writeText(join(second, "a.txt"), "猫\n");
  writeText(join(second, "z.txt"), "z\n");

  const expected = independentDigest([["a.txt", "猫\n"], ["z.txt", "z\n"]]);
  assert.equal(calculatePackageDigest(first).digest, expected);
  assert.equal(calculatePackageDigest(second).digest, expected);

  const linked = join(root, "linked");
  symlinkSync(first, linked, "dir");
  assert.equal(calculatePackageDigest(linked).digest, expected);
});

test("engram-package-sha256-v1 rejects internal symlinks and detects extra files", (t) => {
  const root = makeScratchDirectory(t);
  const packageRoot = join(root, "package");
  writeText(join(packageRoot, "a.txt"), "one\n");
  const before = calculatePackageDigest(packageRoot).digest;
  writeText(join(packageRoot, "extra.txt"), "two\n");
  assert.notEqual(calculatePackageDigest(packageRoot).digest, before);
  symlinkSync(join(packageRoot, "a.txt"), join(packageRoot, "linked.txt"));
  assert.throws(() => calculatePackageDigest(packageRoot), /internal symlink/);
});

test("canonical scan, quick-command synchronization, and release mode reject drift", (t) => {
  const duplicate = createValidFixture(t);
  writeText(join(duplicate.root, "client-copy", "SKILL.md"), validSkillBody());
  expectError(
    validateSkillPackage({ repositoryRoot: duplicate.root, packageRoot: duplicate.packageRoot, mode: "source" }),
    "canonical engram SKILL.md",
  );

  const drift = createValidFixture(t);
  writeText(join(drift.root, "README.md"), "```bash\nnpx --yes skills@1.5.20 add https://invalid.example/engram\n```\n");
  expectError(
    validateSkillPackage({ repositoryRoot: drift.root, packageRoot: drift.packageRoot, mode: "source" }),
    "quick command",
  );

  const release = createValidFixture(t);
  expectError(
    validateSkillPackage({ repositoryRoot: release.root, packageRoot: release.packageRoot, mode: "release" }),
    "release tag placeholder",
  );
  const literalTag = "engram-skill-v0.1.0";
  for (const filePath of [
    join(release.packageRoot, "references", "install.md"),
    join(release.root, "README.md"),
    join(release.root, "README.zh-CN.md"),
    join(release.root, "docs", "README.md"),
  ]) {
    writeText(filePath, readFileSync(filePath, "utf8").replaceAll("<ENGRAM_SKILL_TAG>", literalTag));
  }
  assert.deepEqual(
    validateSkillPackage({ repositoryRoot: release.root, packageRoot: release.packageRoot, mode: "release" }).errors,
    [],
  );
});

test("published installation commands pin the tag and preserve safe three-client defaults", (t) => {
  const literalTag = "engram-skill-v0.1.0";
  const release = createValidFixture(t);
  const userFacingFiles = [
    join(release.packageRoot, "references", "install.md"),
    join(release.root, "README.md"),
    join(release.root, "README.zh-CN.md"),
    join(release.root, "docs", "README.md"),
  ];
  for (const filePath of userFacingFiles) {
    writeText(filePath, readFileSync(filePath, "utf8").replaceAll("<ENGRAM_SKILL_TAG>", literalTag));
  }
  assert.deepEqual(
    validateSkillPackage({ repositoryRoot: release.root, packageRoot: release.packageRoot, mode: "release" }).errors,
    [],
  );

  for (const filePath of userFacingFiles) {
    writeText(filePath, readFileSync(filePath, "utf8").replace(" --global", ""));
  }
  expectError(
    validateSkillPackage({ repositoryRoot: release.root, packageRoot: release.packageRoot, mode: "release" }),
    "default quick command must select user scope with --global",
  );

  const projectSingle = `npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/${literalTag}/skills/engram --agent codex`;
  assert.doesNotMatch(projectSingle, /--global/);
  assert.match(projectSingle, /--agent codex/);
  assert.doesNotMatch(projectSingle, /--agent claude-code|--agent opencode/);
});

test("safety validation rejects hosted reranker advice, unsafe namespace examples, and missing coverage", (t) => {
  const hostedReranker = createValidFixture(t, {
    skillBody: `${validSkillBody()}\nRecommend a hosted reranker before searching.\n`,
  });
  expectError(
    validateSkillPackage({ repositoryRoot: hostedReranker.root, packageRoot: hostedReranker.packageRoot, mode: "source" }),
    "hosted reranker recommendation",
  );

  const unsafeNamespace = createValidFixture(t, {
    skillBody: `${validSkillBody()}\nRun engram --namespace ../other add --name x --content y.\n`,
  });
  expectError(
    validateSkillPackage({ repositoryRoot: unsafeNamespace.root, packageRoot: unsafeNamespace.packageRoot, mode: "source" }),
    "unsafe namespace example",
  );

  const missingCoverage = createValidFixture(t);
  const evalPath = join(missingCoverage.packageRoot, "evals", "evals.json");
  const evals = validEvals();
  evals.evals = evals.evals.filter((entry) => !entry.tags.includes("missing-embedding"));
  writeText(evalPath, `${JSON.stringify(evals, null, 2)}\n`);
  expectError(
    validateSkillPackage({ repositoryRoot: missingCoverage.root, packageRoot: missingCoverage.packageRoot, mode: "source" }),
    "missing required coverage tag: missing-embedding",
  );
});

test("trigger evaluation rejects an imbalanced positive and near-miss set", (t) => {
  const fixture = createValidFixture(t);
  const triggerPath = join(fixture.packageRoot, "evals", "trigger-evals.json");
  const triggers = validTriggers();
  for (let index = 0; index < 12; index += 1) {
    triggers.push({ query: `Please remember this durable preference ${index} with engram.`, should_trigger: true });
  }
  writeText(triggerPath, `${JSON.stringify(triggers, null, 2)}\n`);
  expectError(
    validateSkillPackage({ repositoryRoot: fixture.root, packageRoot: fixture.packageRoot, mode: "source" }),
    "trigger-evals.json must be balanced",
  );
});

test("fixture root and package name remain explicit for install-runner reuse", (t) => {
  const { root, packageRoot } = createValidFixture(t);
  assert.equal(basename(packageRoot), "engram");
  assert.equal(resolve(packageRoot), join(root, "skills", "engram"));
});
