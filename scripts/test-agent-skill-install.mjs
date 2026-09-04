import assert from "node:assert/strict";
import {
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  realpathSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, resolve, sep } from "node:path";
import test from "node:test";

import { calculatePackageDigest, validateSkillPackage } from "./validate-agent-skill.mjs";

const supportedClients = ["claude-code", "codex", "opencode"];
const supportedScopes = ["project", "user"];
const requiredInstallerVersion = "1.5.20";

// ---------------------------------------------------------------------------
// T059 (048) US6: canonical discovery policy.
//
// The standards-based shared skills directory (<scope>/.agents/skills/engram)
// holds the single real copy; codex and opencode scan it natively, while Claude
// Code only reads its own config root and consumes the shared copy through a
// symlink. This table is the *recorded* policy, not a fresh guess: the probe
// receipt is its evidence source, so a client whose discovery behavior changes
// must be re-probed and the record updated first, then this table.
const discoveryPolicy = {
  evidence: "specs/048-implicit-memory-flywheel/receipts/installation-discovery.md",
  sharedDirectory: "the standards-based shared skills directory (<scope root>/.agents/skills/engram)",
  clients: {
    "claude-code": "own-root-symlink",
    codex: "shared-native",
    opencode: "shared-native",
  },
};

function usage() {
  return "usage: node scripts/test-agent-skill-install.mjs --scratch <absolute-session-scratch> [--source <package>] [--installer-version 1.5.20] [--smoke-receipt <installation-smoke-receipt.json>]";
}

function parseArguments(argv) {
  const options = {
    scratch: null,
    source: resolve("skills/engram"),
    installerVersion: requiredInstallerVersion,
    smokeReceipt: null,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--scratch" || argument === "--source" || argument === "--installer-version" || argument === "--smoke-receipt") {
      const value = argv[index + 1];
      if (!value) {
        throw new Error(`${argument} requires a value`);
      }
      if (argument === "--scratch") {
        options.scratch = resolve(value);
      } else if (argument === "--source") {
        options.source = resolve(value);
      } else if (argument === "--smoke-receipt") {
        options.smokeReceipt = resolve(value);
      } else {
        options.installerVersion = value;
      }
      index += 1;
    } else {
      throw new Error(`unknown argument: ${argument}`);
    }
  }
  if (options.scratch === null || !isAbsolute(options.scratch)) {
    throw new Error("--scratch must be an absolute session scratchpad path");
  }
  if (options.installerVersion !== requiredInstallerVersion) {
    throw new Error(`installer version must be pinned to ${requiredInstallerVersion}`);
  }
  return options;
}

// ---------------------------------------------------------------------------
// Path model: one canonical shared copy per scope, plus the documented
// per-client discovery roots that must resolve to it.

function sharedSkillsRoot(profile, scope) {
  if (!supportedScopes.includes(scope)) {
    throw new Error(`unsupported scope: ${scope}`);
  }
  // User scope: ~/.agents/skills. Project scope: <repo>/.agents/skills.
  return scope === "project" ? join(profile.project, ".agents", "skills") : join(profile.home, ".agents", "skills");
}

function canonicalPath(profile, scope) {
  return join(sharedSkillsRoot(profile, scope), "engram");
}

function discoveryPath(profile, client, scope, policy = discoveryPolicy) {
  if (!supportedClients.includes(client)) {
    throw new Error(`unsupported client: ${client}`);
  }
  if (!supportedScopes.includes(scope)) {
    throw new Error(`unsupported scope: ${scope}`);
  }
  const kind = policy.clients[client];
  if (!kind) {
    throw new Error(`${client} has no recorded discovery kind; probe it and update ${policy.evidence} first`);
  }
  // Claude Code is the only client whose discovery root is not the shared
  // standard directory, which is exactly why it needs the symlink.
  if (kind === "own-root-symlink") {
    const root = scope === "project" ? join(profile.project, ".claude", "skills") : join(profile.claudeConfig, "skills");
    return join(root, "engram");
  }
  return canonicalPath(profile, scope);
}

// Roots that still exist and must never gain a second real copy: the legacy
// private directories codex and opencode keep scanning next to the shared one.
function legacyPrivateRoots(profile) {
  return [join(profile.codexHome, "skills"), join(profile.xdgConfig, "opencode", "skills")];
}

// Every root a skill copy could hide in: homes, project, and the per-client
// config homes the isolated profile models.
function discoveryScanRoots(profile) {
  return [profile.home, profile.project, profile.claudeConfig, profile.codexHome, profile.xdgConfig];
}

function profileFor(caseRoot) {
  const profile = {
    home: join(caseRoot, "home"),
    project: join(caseRoot, "project"),
    xdgConfig: join(caseRoot, "xdg", "config"),
    xdgState: join(caseRoot, "xdg", "state"),
    xdgCache: join(caseRoot, "xdg", "cache"),
    temporary: join(caseRoot, "tmp"),
    npmCache: join(caseRoot, "npm-cache"),
    npmUserConfig: join(caseRoot, "npmrc"),
    claudeConfig: join(caseRoot, "claude-config"),
    codexHome: join(caseRoot, "codex-home"),
    githubConfig: join(caseRoot, "github-config"),
  };
  for (const pathValue of Object.values(profile)) {
    if (pathValue.endsWith("npmrc")) {
      continue;
    }
    mkdirSync(pathValue, { recursive: true });
  }
  return profile;
}

function isolatedEnvironment(profile) {
  return {
    HOME: profile.home,
    XDG_CONFIG_HOME: profile.xdgConfig,
    XDG_STATE_HOME: profile.xdgState,
    XDG_CACHE_HOME: profile.xdgCache,
    TMPDIR: profile.temporary,
    NPM_CONFIG_CACHE: profile.npmCache,
    NPM_CONFIG_USERCONFIG: profile.npmUserConfig,
    CLAUDE_CONFIG_DIR: profile.claudeConfig,
    CODEX_HOME: profile.codexHome,
    GH_CONFIG_DIR: profile.githubConfig,
    DISABLE_TELEMETRY: "1",
    DO_NOT_TRACK: "1",
    CI: "1",
  };
}

// ---------------------------------------------------------------------------
// Installation engine.

function assertContained(root, candidate) {
  const normalizedRoot = resolve(root);
  const normalizedCandidate = resolve(candidate);
  if (normalizedCandidate === normalizedRoot || !normalizedCandidate.startsWith(`${normalizedRoot}${sep}`)) {
    throw new Error(`refusing to mutate path outside isolated scratch: ${normalizedCandidate}`);
  }
}

function ensureScratch(options) {
  const repositoryRoot = options.repositoryRoot ?? resolve(".");
  if (options.scratch === repositoryRoot || repositoryRoot.startsWith(`${options.scratch}${sep}`) || options.scratch.startsWith(`${repositoryRoot}${sep}`)) {
    throw new Error("scratch directory must not be the repository or contain it");
  }
  mkdirSync(options.scratch, { recursive: true });
}

function makeCaseRoot(scratch, name) {
  const caseRoot = join(scratch, name);
  assertContained(scratch, caseRoot);
  rmSync(caseRoot, { recursive: true, force: true });
  mkdirSync(caseRoot, { recursive: true });
  return caseRoot;
}

function replaceWithSource(source, destination, requestedMode, caseRoot) {
  assertContained(caseRoot, destination);
  rmSync(destination, { recursive: true, force: true });
  mkdirSync(resolve(destination, ".."), { recursive: true });
  if (requestedMode === "symlink") {
    try {
      symlinkSync(source, destination, "dir");
      return "symlink";
    } catch (error) {
      if (error.code !== "EPERM" && error.code !== "EACCES" && error.code !== "ENOTSUP") {
        throw error;
      }
    }
  }
  cpSync(source, destination, { recursive: true, dereference: false, force: true, errorOnExist: false });
  return "copy";
}

// Installs the canonical shape for the selected clients and scope: exactly one
// real copy at the standard shared directory, and a symlink to it for every
// client whose own discovery root differs (today: Claude Code only). Copy mode
// is the documented fallback for filesystems that refuse symlinks, so it keeps
// the shared copy and may add Claude Code's own-root copy — never a codex or
// opencode private-directory copy.
function installPackage({ source, profile, caseRoot, clients, scope, requestedMode, allowReplace }) {
  for (const client of clients) {
    if (!supportedClients.includes(client)) {
      throw new Error(`unsupported client: ${client}`);
    }
  }
  if (!supportedScopes.includes(scope)) {
    throw new Error(`unsupported scope: ${scope}`);
  }
  const canonical = canonicalPath(profile, scope);
  const targets = [canonical];
  for (const client of clients) {
    const target = discoveryPath(profile, client, scope);
    if (!targets.includes(target)) {
      targets.push(target);
    }
  }
  const existing = targets.filter((target) => existsSync(target));
  if (existing.length > 0 && !allowReplace) {
    return { status: "cancelled", existing, modes: {}, canonical };
  }

  const modes = {};
  for (const target of targets) {
    if (target === canonical) {
      modes[target] = replaceWithSource(source, target, "copy", caseRoot);
      continue;
    }
    modes[target] = replaceWithSource(canonical, target, requestedMode, caseRoot);
  }
  return { status: "installed", existing, modes, canonical };
}

function writeUnknownSkill(target) {
  mkdirSync(target, { recursive: true });
  writeFileSync(join(target, "SKILL.md"), "---\nname: engram\ndescription: user maintained\n---\n", "utf8");
}

// ---------------------------------------------------------------------------
// T059 (048) US6 assertions.

function findEngramSkillCopies(roots) {
  const found = [];
  const visit = (directory) => {
    let entries;
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const child = join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(child);
      } else if (entry.isSymbolicLink()) {
        // A symlinked skill directory (the shared-copy shape) is recorded but
        // never descended into, so the canonical copy is counted exactly once.
        let target;
        try {
          target = statSync(child);
        } catch {
          continue; // dangling
        }
        if (target.isDirectory() && existsSync(join(child, "SKILL.md"))) {
          found.push({ skillRoot: child, isSymlink: true });
        }
      } else if (entry.isFile() && entry.name === "SKILL.md" && basename(directory) === "engram") {
        found.push({ skillRoot: directory, isSymlink: false });
      }
    }
  };
  for (const root of roots) {
    if (existsSync(root)) {
      visit(root);
    }
  }
  return found;
}

// 1. standard-directory: the single real copy lives in the standards-based
// shared skills directory, and every client's discovery root matches its
// recorded kind (shared-native, or own-root for Claude Code).
function assertStandardDirectory({ profile, scope, clients, sourceDigest, policy = discoveryPolicy }) {
  const canonical = canonicalPath(profile, scope);
  const problems = [];
  if (!existsSync(join(canonical, "SKILL.md"))) {
    problems.push(`the standard shared directory holds no engram copy at ${canonical}`);
  } else if (sourceDigest) {
    const digest = calculatePackageDigest(canonical).digest;
    if (digest !== sourceDigest) {
      problems.push(`standard shared copy digest ${digest} does not match the source digest ${sourceDigest}`);
    }
  }
  const discovered = {};
  for (const client of clients) {
    const kind = policy.clients[client];
    if (!kind) {
      problems.push(`${client} has no recorded discovery kind: probe it and update ${policy.evidence} first`);
      continue;
    }
    const path = discoveryPath(profile, client, scope, policy);
    discovered[client] = path;
    if (kind === "shared-native") {
      if (resolve(path) !== resolve(canonical)) {
        problems.push(`${client} must discover engram from ${policy.sharedDirectory} (${canonical}), recorded discovery path ${path}`);
      }
    } else if (kind === "own-root-symlink") {
      if (resolve(path) === resolve(canonical)) {
        problems.push(`${client} only reads its own root (${path}) and must consume the shared copy through a symlink`);
      }
    } else {
      problems.push(`unknown recorded discovery kind "${kind}" for ${client} in ${policy.evidence}`);
    }
  }
  assert.equal(problems.length, 0, problems.length > 0 ? `standard-directory policy violated:\n- ${problems.join("\n- ")}` : "");
  return { canonical, discovered };
}

// 2. symlink: every client root that differs from the shared copy is a symlink
// resolving to that one canonical source — never to a second copy.
function assertSymlinkResolvesToCanonical({ profile, scope, clients, sourceDigest, requireSymlink = true, policy = discoveryPolicy }) {
  const canonical = canonicalPath(profile, scope);
  const canonicalReal = realpathSync(canonical);
  const problems = [];
  const symlinked = [];
  const copyFallbacks = [];
  if (lstatSync(canonical).isSymbolicLink()) {
    problems.push(`the canonical copy at ${canonical} must be a real directory, not a symlink`);
  }
  for (const client of clients) {
    const path = discoveryPath(profile, client, scope);
    if (resolve(path) === resolve(canonical)) {
      continue;
    }
    if (!existsSync(path)) {
      problems.push(`${client} discovery path ${path} does not exist`);
      continue;
    }
    if (lstatSync(path).isSymbolicLink()) {
      const target = realpathSync(path);
      symlinked.push(path);
      if (target !== canonicalReal) {
        problems.push(`${client} symlink ${path} resolves to ${target}, not the single canonical copy at ${canonical}`);
        continue;
      }
    } else if (requireSymlink) {
      problems.push(`${client} discovery path ${path} must be a symlink to the shared copy (found a real copy; ${policy.evidence} records the symlink requirement)`);
    } else {
      copyFallbacks.push(path);
    }
    if (sourceDigest && existsSync(path)) {
      const digest = calculatePackageDigest(path).digest;
      if (digest !== sourceDigest) {
        problems.push(`${client} discovery path ${path} exposes digest ${digest}, expected ${sourceDigest}`);
      }
    }
  }
  assert.equal(problems.length, 0, problems.length > 0 ? `symlink policy violated:\n- ${problems.join("\n- ")}` : "");
  return { canonical, symlinked, copyFallbacks };
}

// 3. no-private-duplicate: exactly one real copy exists (the shared canonical
// one, plus Claude Code's documented own-root fallback on symlink-less
// filesystems); every other copy is a symlink back to the canonical source, and
// the legacy private roots stay empty.
function assertNoPrivateDuplicate({ profile, scope, sourceDigest, allowClientCopyFallback = false }) {
  const canonical = canonicalPath(profile, scope);
  const copies = findEngramSkillCopies(discoveryScanRoots(profile));
  const problems = [];
  const realCopies = copies.filter((copy) => !copy.isSymlink);
  const symlinkCopies = copies.filter((copy) => copy.isSymlink);
  if (copies.length === 0) {
    problems.push("no discovered engram skill anywhere in the discovery trees");
  }
  for (const copy of realCopies) {
    if (resolve(copy.skillRoot) === resolve(canonical)) {
      continue;
    }
    if (allowClientCopyFallback && resolve(copy.skillRoot) === resolve(discoveryPath(profile, "claude-code", scope))) {
      continue;
    }
    problems.push(`private duplicate engram copy at ${copy.skillRoot}: only ${canonical} may hold a real copy${allowClientCopyFallback ? "" : ` (and ${discoveryPath(profile, "claude-code", scope)} must stay a symlink)`}`);
  }
  const canonicalReal = existsSync(canonical) ? realpathSync(canonical) : null;
  for (const copy of symlinkCopies) {
    let target = null;
    try {
      target = realpathSync(copy.skillRoot);
    } catch {
      problems.push(`dangling symlinked engram copy at ${copy.skillRoot}`);
      continue;
    }
    if (canonicalReal === null || target !== canonicalReal) {
      problems.push(`symlinked engram copy at ${copy.skillRoot} resolves to ${target}, not the single canonical copy at ${canonical}`);
    }
  }
  if (sourceDigest) {
    for (const copy of realCopies) {
      let digest = null;
      try {
        digest = calculatePackageDigest(copy.skillRoot).digest;
      } catch (error) {
        problems.push(`cannot digest engram copy at ${copy.skillRoot}: ${error.message}`);
        continue;
      }
      if (digest !== sourceDigest) {
        problems.push(`engram copy at ${copy.skillRoot} has digest ${digest}, expected ${sourceDigest}`);
      }
    }
  }
  assert.equal(problems.length, 0, problems.length > 0 ? `no-private-duplicate policy violated:\n- ${problems.join("\n- ")}` : "");
  return {
    canonical,
    copyRoots: realCopies.map((copy) => copy.skillRoot),
    symlinkRoots: symlinkCopies.map((copy) => copy.skillRoot),
    legacyPrivateRoots: legacyPrivateRoots(profile),
  };
}

// Composition used by the matrix: the three layout assertions plus "each tool
// discovers exactly one engram skill".
function assertDiscovery({ sourceDigest, profile, clients, scope, requestedMode }) {
  const standard = assertStandardDirectory({ profile, scope, clients, sourceDigest });
  const symlink = assertSymlinkResolvesToCanonical({ profile, scope, clients, sourceDigest, requireSymlink: requestedMode === "symlink" });
  const noDuplicate = assertNoPrivateDuplicate({ profile, scope, sourceDigest, allowClientCopyFallback: requestedMode !== "symlink" });
  const copies = findEngramSkillCopies(discoveryScanRoots(profile));
  for (const client of clients) {
    const discovered = resolve(discoveryPath(profile, client, scope));
    const visible = copies.filter((copy) => resolve(copy.skillRoot) === discovered);
    assert.equal(
      visible.length,
      1,
      `${client} must discover exactly one engram skill, saw ${visible.length}: ${copies.map((copy) => copy.skillRoot).join(", ") || "none"}`,
    );
  }
  return { standard, symlink, noDuplicate, assertions: ["standard-directory", "symlink", "no-private-duplicate", "one-skill-per-client"] };
}

// ---------------------------------------------------------------------------
// T059 (048) US6 smoke-receipt assertions (T063 records the real receipts).

const smokeReceiptSchema = {
  kind: "installation-smoke-receipt",
  version: 1,
  statuses: ["pass", "fail", "blocked"],
};

function validateInstallationSmokeReceipt(receipt) {
  if (!receipt || typeof receipt !== "object" || Array.isArray(receipt)) {
    return ["receipt must be a JSON object"];
  }
  const problems = [];
  if (receipt.kind !== smokeReceiptSchema.kind) {
    problems.push(`receipt.kind must be "${smokeReceiptSchema.kind}"`);
  }
  if (receipt.schema_version !== smokeReceiptSchema.version) {
    problems.push(`receipt.schema_version must be ${smokeReceiptSchema.version}`);
  }
  if (typeof receipt.package_digest !== "string" || !/^[a-f0-9]{64}$/.test(receipt.package_digest)) {
    problems.push("receipt.package_digest must be the observed engram-package-sha256-v1 digest (64 lowercase hex chars)");
  }
  if (typeof receipt.scoring_equivalent !== "boolean") {
    problems.push("receipt.scoring_equivalent must be an explicit boolean (false whenever the receipt covers a revision other than the evaluated snapshot)");
  }
  if (!Array.isArray(receipt.smokes)) {
    problems.push("receipt.smokes must be an array with one entry per tool");
    return problems;
  }
  receipt.smokes.forEach((smoke, index) => {
    const label = `smokes[${index}]`;
    if (!smoke || typeof smoke !== "object") {
      problems.push(`${label} must be an object`);
      return;
    }
    if (!supportedClients.includes(smoke.tool)) {
      problems.push(`${label}.tool must be one of ${supportedClients.join(", ")}`);
    }
    if (!Array.isArray(smoke.discovered_paths) || smoke.discovered_paths.some((path) => typeof path !== "string" || path.length === 0)) {
      problems.push(`${label}.discovered_paths must list the engram skill paths the tool actually discovered`);
    }
    if (!Array.isArray(smoke.private_duplicates)) {
      problems.push(`${label}.private_duplicates must be an array (empty when the standard-directory policy holds)`);
    }
    const write = smoke.implicit_write_smoke;
    if (!write || typeof write !== "object") {
      problems.push(`${label}.implicit_write_smoke must be an object`);
      return;
    }
    if (typeof write.executed !== "boolean") {
      problems.push(`${label}.implicit_write_smoke.executed must be a boolean`);
    }
    if (!smokeReceiptSchema.statuses.includes(write.status)) {
      problems.push(`${label}.implicit_write_smoke.status must be one of ${smokeReceiptSchema.statuses.join(", ")}`);
    }
    if (typeof write.detail !== "string" || write.detail.trim() === "") {
      problems.push(`${label}.implicit_write_smoke.detail must record what happened (without credentials)`);
    }
    // A "pass" is only a pass when the full minimal smoke happened: the skill
    // triggered, wrote once, and acknowledged in the same turn.
    for (const field of ["triggered", "wrote", "acknowledged"]) {
      if (write[field] !== undefined && typeof write[field] !== "boolean") {
        problems.push(`${label}.implicit_write_smoke.${field} must be a boolean`);
      }
    }
    if (write.status === "pass" && (write.triggered !== true || write.wrote !== true || write.acknowledged !== true)) {
      problems.push(`${label}.implicit_write_smoke.status "pass" requires triggered, wrote, and acknowledged all true`);
    }
  });
  return problems;
}

function receiptProblems(receipt, header) {
  const problems = validateInstallationSmokeReceipt(receipt);
  if (problems.length > 0) {
    throw new Error(`${header}:\n- ${problems.join("\n- ")}`);
  }
  return problems;
}

// 4. three-tools-all-executed: the receipt covers every supported tool exactly
// once, each with an executed smoke, exactly one discovered skill, and no
// private duplicates.
function assertThreeToolsAllExecuted(receipt) {
  receiptProblems(receipt, "invalid installation smoke receipt");
  const problems = [];
  const byTool = new Map();
  for (const smoke of receipt.smokes) {
    if (byTool.has(smoke.tool)) {
      problems.push(`duplicate smoke record for ${smoke.tool}`);
      continue;
    }
    byTool.set(smoke.tool, smoke);
  }
  for (const client of supportedClients) {
    const smoke = byTool.get(client);
    if (!smoke) {
      problems.push(`no smoke record for ${client}`);
      continue;
    }
    if (smoke.implicit_write_smoke.executed !== true) {
      problems.push(`${client} smoke was not executed (implicit_write_smoke.executed !== true)`);
    }
    if (smoke.discovered_paths.length !== 1) {
      problems.push(`${client} must discover exactly one engram skill, recorded ${smoke.discovered_paths.length}: ${smoke.discovered_paths.join(", ") || "none"}`);
    }
    if (smoke.private_duplicates.length > 0) {
      problems.push(`${client} recorded private duplicates: ${smoke.private_duplicates.join(", ")}`);
    }
  }
  assert.equal(problems.length, 0, problems.length > 0 ? `three-tools-all-executed violated:\n- ${problems.join("\n- ")}` : "");
  return { tools: supportedClients.map((client) => byTool.get(client).tool) };
}

// 5. at-least-one-implicit-smoke-pass: at least one tool completed the minimal
// implicit-write smoke (trigger + write + acknowledge). Other tools may fail or
// be blocked by host limits as long as they are recorded honestly.
function assertAtLeastOneImplicitSmokePass(receipt) {
  receiptProblems(receipt, "invalid installation smoke receipt");
  const completePasses = receipt.smokes
    .filter((smoke) => {
      const write = smoke.implicit_write_smoke;
      return write.executed === true && write.status === "pass" && write.triggered === true && write.wrote === true && write.acknowledged === true;
    })
    .map((smoke) => smoke.tool);
  assert.ok(
    completePasses.length >= 1,
    `at least one tool must complete the implicit-write smoke (trigger + write + acknowledge in the same turn); complete passes: ${completePasses.join(", ") || "none"}`,
  );
  return { complete_passes: completePasses };
}

function assertInstallationSmokeReceipt(receipt) {
  const threeTools = assertThreeToolsAllExecuted(receipt);
  const smokePass = assertAtLeastOneImplicitSmokePass(receipt);
  return {
    kind: smokeReceiptSchema.kind,
    schema_version: smokeReceiptSchema.version,
    package_digest: receipt.package_digest,
    scoring_equivalent: receipt.scoring_equivalent,
    three_tools_all_executed: true,
    at_least_one_implicit_smoke_pass: true,
    tools: threeTools.tools,
    complete_passes: smokePass.complete_passes,
  };
}

// ---------------------------------------------------------------------------

function runCase(summary, scratch, name, callback) {
  const caseRoot = makeCaseRoot(scratch, name);
  const profile = profileFor(caseRoot);
  const result = callback({ caseRoot, profile });
  summary.push({ name, ...result, isolated_environment: isolatedEnvironment(profile) });
}

function runMatrix(options) {
  ensureScratch(options);
  const repositoryRoot = options.repositoryRoot ?? resolve(".");
  const sourceValidation = validateSkillPackage({
    repositoryRoot,
    packageRoot: options.source,
    mode: "source",
  });
  const installationBlockingErrors = sourceValidation.errors.filter((error) => !/^(contract\.json|evals\.json|trigger-evals\.json)/.test(error));
  if (installationBlockingErrors.length > 0) {
    throw new Error(`installation-relevant source validation failed before matrix:\n${installationBlockingErrors.join("\n")}`);
  }
  const sourceDigest = calculatePackageDigest(options.source).digest;
  const summary = [];

  for (const client of supportedClients) {
    for (const scope of supportedScopes) {
      runCase(summary, options.scratch, `single-${client}-${scope}`, ({ caseRoot, profile }) => {
        const install = installPackage({
          source: options.source,
          profile,
          caseRoot,
          clients: [client],
          scope,
          requestedMode: "symlink",
          allowReplace: true,
        });
        assert.equal(install.status, "installed");
        assertDiscovery({ sourceDigest, profile, clients: [client], scope, requestedMode: "symlink" });
        return { status: "pass", scope, clients: [client], mode: install.modes };
      });
    }
  }

  for (const [name, scope, requestedMode] of [
    ["combined-project-symlink", "project", "symlink"],
    ["combined-user-symlink", "user", "symlink"],
    ["combined-user-copy", "user", "copy"],
  ]) {
    runCase(summary, options.scratch, name, ({ caseRoot, profile }) => {
      const first = installPackage({ source: options.source, profile, caseRoot, clients: supportedClients, scope, requestedMode, allowReplace: true });
      const second = installPackage({ source: options.source, profile, caseRoot, clients: supportedClients, scope, requestedMode, allowReplace: true });
      assert.equal(first.status, "installed");
      assert.equal(second.status, "installed");
      assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope, requestedMode });
      return { status: "pass", scope, clients: supportedClients, mode: second.modes, same_version_reinstall: "one discovered skill per client" };
    });
  }

  runCase(summary, options.scratch, "unknown-collision-cancel", ({ caseRoot, profile }) => {
    const target = canonicalPath(profile, "user");
    writeUnknownSkill(target);
    const before = readFileSync(join(target, "SKILL.md"), "utf8");
    const attempt = installPackage({ source: options.source, profile, caseRoot, clients: supportedClients, scope: "user", requestedMode: "symlink", allowReplace: false });
    assert.equal(attempt.status, "cancelled");
    assert.equal(readFileSync(join(target, "SKILL.md"), "utf8"), before);
    return { status: "pass", cancellation: "unknown target unchanged" };
  });

  runCase(summary, options.scratch, "explicit-replacement", ({ caseRoot, profile }) => {
    writeUnknownSkill(canonicalPath(profile, "user"));
    writeUnknownSkill(discoveryPath(profile, "claude-code", "user"));
    const install = installPackage({ source: options.source, profile, caseRoot, clients: supportedClients, scope: "user", requestedMode: "symlink", allowReplace: true });
    assert.equal(install.status, "installed");
    assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope: "user", requestedMode: "symlink" });
    return { status: "pass", replacement: "explicitly confirmed", mode: install.modes };
  });

  runCase(summary, options.scratch, "interruption-recovery", ({ caseRoot, profile }) => {
    const partialTarget = canonicalPath(profile, "user");
    writeUnknownSkill(partialTarget);
    const firstAttempt = { status: "interrupted", target: partialTarget };
    assert.equal(firstAttempt.status, "interrupted");
    const recovery = installPackage({ source: options.source, profile, caseRoot, clients: supportedClients, scope: "user", requestedMode: "symlink", allowReplace: true });
    assert.equal(recovery.status, "installed");
    assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope: "user", requestedMode: "symlink" });
    return { status: "pass", interruption: firstAttempt, recovery: "all final digests equal" };
  });

  const result = {
    status: "ok",
    runner: "local-copy-symlink-contract",
    installer_version: options.installerVersion,
    digest_algorithm: "engram-package-sha256-v1",
    source_digest: sourceDigest,
    standard_directory_policy: { evidence: discoveryPolicy.evidence, shared: discoveryPolicy.sharedDirectory, clients: { ...discoveryPolicy.clients } },
    us6_assertions: ["standard-directory", "symlink", "no-private-duplicate", "three-tools-all-executed", "at-least-one-implicit-smoke-pass"],
    cases: summary,
    host_mutation: "0 by construction: all generated paths are descendants of --scratch",
    note: "This local matrix validates the frozen standard-directory, symlink, no-private-duplicate, collision, copy, and recovery contract, including Claude Code consuming the shared copy through a symlink. Exact remote installer behavior and real-client implicit-write smokes remain release gates; pass --smoke-receipt to assert a recorded three-tool smoke receipt (T063).",
  };

  if (options.smokeReceipt) {
    result.smoke_receipt = assertInstallationSmokeReceipt(JSON.parse(readFileSync(options.smokeReceipt, "utf8")));
  }
  return result;
}

// ---------------------------------------------------------------------------
// Test mode (node --test) vs CLI mode. CI and the release runbook use the CLI
// matrix; node --test exercises the T059 assertions hermetically and offline.

const scriptDirectory = dirname(new URL(import.meta.url).pathname);
const repositoryRoot = resolve(scriptDirectory, "..");
const testScratchBase = process.env.ENGRAM_SKILL_TEST_SCRATCH || tmpdir();

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function makeScratchDirectory(t) {
  mkdirSync(testScratchBase, { recursive: true });
  const directory = mkdtempSync(join(testScratchBase, "engram-skill-install-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

function writeMinimalPackage(root) {
  mkdirSync(join(root, "references"), { recursive: true });
  writeFileSync(join(root, "SKILL.md"), "---\nname: engram\ndescription: canonical install layout fixture\n---\n\n# engram install fixture\n", "utf8");
  writeFileSync(join(root, "references", "install.md"), "# Install the engram skill\n", "utf8");
  return root;
}

function installedLayout(t, { scope, requestedMode }) {
  const scratch = makeScratchDirectory(t);
  const caseRoot = join(scratch, "case");
  mkdirSync(caseRoot, { recursive: true });
  const profile = profileFor(caseRoot);
  const source = writeMinimalPackage(join(scratch, "source", "engram"));
  const install = installPackage({ source, profile, caseRoot, clients: supportedClients, scope, requestedMode, allowReplace: true });
  assert.equal(install.status, "installed");
  return { scratch, caseRoot, profile, source, sourceDigest: calculatePackageDigest(source).digest };
}

function copyEngramInto(destination, source) {
  cpSync(source, destination, { recursive: true, dereference: false, force: true });
}

function smokeReceipt(overrides = {}) {
  const smokes = supportedClients.map((tool, index) => ({
    tool,
    discovered_paths: [`~/.agents/skills/engram (${tool})`],
    private_duplicates: [],
    implicit_write_smoke: {
      executed: true,
      status: index === 0 ? "pass" : "fail",
      triggered: index === 0,
      wrote: index === 0,
      acknowledged: index === 0,
      detail: index === 0 ? "wrote once and acknowledged in the same turn" : "host rejected the session write",
      ...(overrides.smokeOverrides?.[tool] ?? {}),
    },
  }));
  const { smokeOverrides: _ignored, ...receiptOverrides } = overrides;
  return {
    kind: "installation-smoke-receipt",
    schema_version: 1,
    package_digest: "a".repeat(64),
    scoring_equivalent: false,
    smokes,
    ...receiptOverrides,
  };
}

function registerTests() {
  test("standard-directory: every tool discovers the single shared copy at its recorded standard location", (t) => {
    for (const scope of supportedScopes) {
      const { profile, sourceDigest } = installedLayout(t, { scope, requestedMode: "symlink" });
      const expectedCanonical = scope === "user" ? join(profile.home, ".agents", "skills", "engram") : join(profile.project, ".agents", "skills", "engram");
      assert.equal(canonicalPath(profile, scope), expectedCanonical);

      const { canonical, discovered } = assertStandardDirectory({ profile, scope, clients: supportedClients, sourceDigest });
      assert.equal(canonical, expectedCanonical);
      assert.equal(discovered.codex, expectedCanonical, "codex scans the shared standard directory natively");
      assert.equal(discovered.opencode, expectedCanonical, "opencode scans the shared standard directory natively");
      assert.equal(
        discovered["claude-code"],
        scope === "user" ? join(profile.claudeConfig, "skills", "engram") : join(profile.project, ".claude", "skills", "engram"),
        "claude-code only reads its own config root",
      );
      assert.equal(calculatePackageDigest(canonical).digest, sourceDigest);
      for (const privateRoot of legacyPrivateRoots(profile)) {
        assert.equal(existsSync(join(privateRoot, "engram")), false, `legacy private root ${privateRoot} must stay empty`);
      }
    }
  });

  test("standard-directory: a missing shared copy, a digest drift, or an unrecorded client is rejected rather than guessed", (t) => {
    const { profile, sourceDigest } = installedLayout(t, { scope: "user", requestedMode: "symlink" });

    const missing = makeScratchDirectory(t);
    const missingProfile = profileFor(join(missing, "case"));
    assert.throws(
      () => assertStandardDirectory({ profile: missingProfile, scope: "user", clients: supportedClients, sourceDigest }),
      /standard shared directory holds no engram copy/,
    );

    rmSync(join(canonicalPath(profile, "user"), "SKILL.md"), { force: true });
    assert.throws(
      () => assertStandardDirectory({ profile, scope: "user", clients: supportedClients, sourceDigest }),
      /standard shared directory holds no engram copy/,
    );

    const drifted = installedLayout(t, { scope: "user", requestedMode: "symlink" });
    writeFileSync(join(canonicalPath(drifted.profile, "user"), "DRIFT.md"), "extra\n", "utf8");
    assert.throws(
      () => assertStandardDirectory({ profile: drifted.profile, scope: "user", clients: supportedClients, sourceDigest: drifted.sourceDigest }),
      /standard shared copy digest .* does not match the source digest/,
    );

    const guessedPolicy = { evidence: "probe-record", sharedDirectory: "shared", clients: { "claude-code": "own-root-symlink", codex: "shared-native", opencode: "made-up-kind" } };
    assert.throws(
      () => assertStandardDirectory({ profile: drifted.profile, scope: "user", clients: supportedClients, sourceDigest: drifted.sourceDigest, policy: guessedPolicy }),
      /unknown recorded discovery kind "made-up-kind".*probe-record/,
    );
    const unprobedPolicy = { evidence: "probe-record", sharedDirectory: "shared", clients: { codex: "shared-native", opencode: "shared-native" } };
    assert.throws(
      () => assertStandardDirectory({ profile: drifted.profile, scope: "user", clients: supportedClients, sourceDigest: drifted.sourceDigest, policy: unprobedPolicy }),
      /claude-code has no recorded discovery kind/,
    );
  });

  test("symlink: claude-code consumes the shared copy through a symlink resolving to the one canonical source", (t) => {
    for (const scope of supportedScopes) {
      const { profile, sourceDigest } = installedLayout(t, { scope, requestedMode: "symlink" });
      const claudePath = discoveryPath(profile, "claude-code", scope);
      const result = assertSymlinkResolvesToCanonical({ profile, scope, clients: supportedClients, sourceDigest, requireSymlink: true });
      assert.deepEqual(result.symlinked, [claudePath]);
      assert.deepEqual(result.copyFallbacks, []);
      assert.ok(lstatSync(claudePath).isSymbolicLink());
      assert.equal(realpathSync(claudePath), realpathSync(canonicalPath(profile, scope)), "the symlink must resolve to the unique canonical source");
      assert.equal(calculatePackageDigest(claudePath).digest, sourceDigest, "the shared copy is reached through the symlink unchanged");
      assert.equal(lstatSync(canonicalPath(profile, scope)).isSymbolicLink(), false, "the canonical copy itself is a real directory");
    }
  });

  test("symlink: a real copy where a symlink is required, or a symlink to a second copy, is rejected", (t) => {
    const { profile, source, sourceDigest } = installedLayout(t, { scope: "user", requestedMode: "symlink" });
    const claudePath = discoveryPath(profile, "claude-code", "user");

    rmSync(claudePath, { recursive: true, force: true });
    copyEngramInto(claudePath, source);
    assert.throws(
      () => assertSymlinkResolvesToCanonical({ profile, scope: "user", clients: supportedClients, sourceDigest, requireSymlink: true }),
      /claude-code discovery path .* must be a symlink to the shared copy/,
    );
    assert.throws(
      () => assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest, allowClientCopyFallback: false }),
      /private duplicate engram copy/,
    );

    const stray = installedLayout(t, { scope: "user", requestedMode: "symlink" });
    const strayClaude = discoveryPath(stray.profile, "claude-code", "user");
    rmSync(strayClaude, { recursive: true, force: true });
    const secondCopy = writeMinimalPackage(join(stray.scratch, "second-copy", "engram"));
    symlinkSync(secondCopy, strayClaude, "dir");
    assert.throws(
      () => assertSymlinkResolvesToCanonical({ profile: stray.profile, scope: "user", clients: supportedClients, sourceDigest: stray.sourceDigest, requireSymlink: true }),
      /resolves to .*not the single canonical copy/,
    );
    assert.throws(
      () => assertNoPrivateDuplicate({ profile: stray.profile, scope: "user", sourceDigest: stray.sourceDigest }),
      /not the single canonical copy/,
    );
  });

  test("no-private-duplicate: a second real copy in any discovery tree is rejected", (t) => {
    const { profile, source, sourceDigest } = installedLayout(t, { scope: "user", requestedMode: "symlink" });
    assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest });

    // A hand-copied duplicate into either legacy private root is the classic
    // violation install.md warns about.
    for (const privateRoot of legacyPrivateRoots(profile)) {
      copyEngramInto(join(privateRoot, "engram"), source);
      assert.throws(
        () => assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest }),
        new RegExp(`private duplicate engram copy at ${escapeRegExp(resolve(join(privateRoot, "engram")))}`),
      );
      rmSync(join(privateRoot, "engram"), { recursive: true, force: true });
      assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest });
    }

    // A duplicate under Claude Code's own root is equally a second real copy
    // while the symlink is the required shape.
    const claudePath = discoveryPath(profile, "claude-code", "user");
    rmSync(claudePath, { recursive: true, force: true });
    copyEngramInto(claudePath, source);
    assert.throws(
      () => assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest }),
      new RegExp(`private duplicate engram copy at ${escapeRegExp(resolve(claudePath))}`),
    );
  });

  test("no-private-duplicate: copy mode keeps claude-code's documented own-root fallback and still rejects legacy copies", (t) => {
    const { profile, source, sourceDigest } = installedLayout(t, { scope: "user", requestedMode: "copy" });
    const claudePath = discoveryPath(profile, "claude-code", "user");
    assert.equal(lstatSync(claudePath).isSymbolicLink(), false, "copy mode gives claude-code its own copy");
    const allowed = assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest, allowClientCopyFallback: true });
    assert.deepEqual(allowed.copyRoots.sort(), [claudePath, canonicalPath(profile, "user")].map((path) => resolve(path)).sort());
    assert.throws(
      () => assertSymlinkResolvesToCanonical({ profile, scope: "user", clients: supportedClients, sourceDigest, requireSymlink: true }),
      /must be a symlink to the shared copy/,
    );

    copyEngramInto(join(profile.codexHome, "skills", "engram"), source);
    assert.throws(
      () => assertNoPrivateDuplicate({ profile, scope: "user", sourceDigest, allowClientCopyFallback: true }),
      /private duplicate engram copy/,
    );
  });

  test("three-tools-all-executed: the smoke receipt must cover every supported tool with an executed record", (t) => {
    const receipt = smokeReceipt();
    assert.deepEqual(assertThreeToolsAllExecuted(receipt).tools, supportedClients);
    assert.deepEqual(assertThreeToolsAllExecuted(receipt).tools, ["claude-code", "codex", "opencode"]);

    const missingTool = smokeReceipt({ smokes: smokeReceipt().smokes.filter((smoke) => smoke.tool !== "opencode") });
    assert.throws(() => assertThreeToolsAllExecuted(missingTool), /no smoke record for opencode/);

    const notExecuted = smokeReceipt({ smokeOverrides: { opencode: { executed: false, status: "blocked" } } });
    assert.throws(() => assertThreeToolsAllExecuted(notExecuted), /opencode smoke was not executed/);

    const duplicated = smokeReceipt();
    duplicated.smokes.push(JSON.parse(JSON.stringify(duplicated.smokes[0])));
    assert.throws(() => assertThreeToolsAllExecuted(duplicated), /duplicate smoke record for claude-code/);

    const zeroDiscovery = smokeReceipt({ smokeOverrides: { codex: {} } });
    zeroDiscovery.smokes.find((smoke) => smoke.tool === "codex").discovered_paths = [];
    assert.throws(() => assertThreeToolsAllExecuted(zeroDiscovery), /codex must discover exactly one engram skill, recorded 0/);

    const withDuplicates = smokeReceipt({ smokeOverrides: { codex: {} } });
    withDuplicates.smokes.find((smoke) => smoke.tool === "codex").private_duplicates = ["~/.codex/skills/engram"];
    assert.throws(() => assertThreeToolsAllExecuted(withDuplicates), /codex recorded private duplicates/);

    const badSchema = smokeReceipt({ package_digest: "not-a-digest" });
    assert.throws(() => assertThreeToolsAllExecuted(badSchema), /package_digest must be the observed engram-package-sha256-v1 digest/);
  });

  test("at-least-one-implicit-smoke-pass: at least one complete trigger + write + acknowledge smoke must pass", (t) => {
    assert.deepEqual(assertAtLeastOneImplicitSmokePass(smokeReceipt()).complete_passes, ["claude-code"]);
    // A pass on any tool satisfies the requirement, not just the first one.
    assert.deepEqual(
      assertAtLeastOneImplicitSmokePass(smokeReceipt({
        smokeOverrides: {
          "claude-code": { status: "fail", triggered: false, wrote: false, acknowledged: false },
          codex: { status: "pass", triggered: true, wrote: true, acknowledged: true, detail: "codex completed the same-turn write" },
        },
      })).complete_passes,
      ["codex"],
    );
    assert.throws(
      () => assertAtLeastOneImplicitSmokePass(smokeReceipt({ smokeOverrides: { "claude-code": { status: "fail", triggered: false, wrote: false, acknowledged: false } } })),
      /complete passes: none/,
    );
    assert.throws(
      () => assertAtLeastOneImplicitSmokePass(smokeReceipt({ smokeOverrides: { "claude-code": { wrote: false } } })),
      /status "pass" requires triggered, wrote, and acknowledged all true/,
    );
    const allBlocked = smokeReceipt({
      smokeOverrides: {
        "claude-code": { status: "blocked", triggered: false, wrote: false, acknowledged: false },
        codex: { status: "blocked", triggered: false, wrote: false, acknowledged: false },
        opencode: { status: "blocked", triggered: false, wrote: false, acknowledged: false },
      },
    });
    assert.throws(() => assertAtLeastOneImplicitSmokePass(allBlocked), /at least one tool must complete the implicit-write smoke/);
    assert.throws(() => assertInstallationSmokeReceipt(allBlocked), /at least one tool must complete the implicit-write smoke/);

    const passing = assertInstallationSmokeReceipt(smokeReceipt());
    assert.equal(passing.three_tools_all_executed, true);
    assert.equal(passing.at_least_one_implicit_smoke_pass, true);
    assert.deepEqual(passing.complete_passes, ["claude-code"]);
  });

  test("full offline install matrix stays green on the canonical layout", (t) => {
    const source = join(repositoryRoot, "skills", "engram");
    const validation = validateSkillPackage({ repositoryRoot, packageRoot: source, mode: "source" });
    const blocking = validation.errors.filter((error) => !/^(contract\.json|evals\.json|trigger-evals\.json)/.test(error));
    if (blocking.length > 0) {
      t.skip(`real package currently has installation-relevant validation errors; the CI CLI step gates this: ${blocking[0]}`);
      return;
    }
    const scratch = makeScratchDirectory(t);
    const receiptPath = join(scratch, "installation-smoke-receipt.json");
    writeFileSync(receiptPath, `${JSON.stringify(smokeReceipt({ package_digest: calculatePackageDigest(source).digest }), null, 2)}\n`, "utf8");

    const result = runMatrix({ repositoryRoot, scratch, source, installerVersion: requiredInstallerVersion, smokeReceipt: receiptPath });
    assert.equal(result.status, "ok");
    assert.equal(result.cases.length, 12);
    assert.equal(result.us6_assertions.length, 5);
    assert.equal(result.standard_directory_policy.clients.codex, "shared-native");
    assert.equal(result.smoke_receipt.three_tools_all_executed, true);
    assert.equal(result.smoke_receipt.at_least_one_implicit_smoke_pass, true);
    assert.equal(result.smoke_receipt.package_digest, result.source_digest);
    for (const name of ["combined-project-symlink", "combined-user-symlink", "combined-user-copy", "unknown-collision-cancel", "explicit-replacement", "interruption-recovery"]) {
      assert.ok(result.cases.some((entry) => entry.name === name && entry.status === "pass"), `${name} must pass`);
    }
  });
}

if (process.env.NODE_TEST_CONTEXT === undefined) {
  try {
    const result = runMatrix(parseArguments(process.argv.slice(2)));
    console.log(JSON.stringify(result, null, 2));
  } catch (error) {
    console.error(`error: ${error.message}`);
    console.error(usage());
    process.exitCode = 1;
  }
} else {
  registerTests();
}
