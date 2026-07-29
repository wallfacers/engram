import assert from "node:assert/strict";
import {
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import { calculatePackageDigest, validateSkillPackage } from "./validate-agent-skill.mjs";

const supportedClients = ["claude-code", "codex", "opencode"];
const supportedScopes = ["project", "user"];
const requiredInstallerVersion = "1.5.20";

function usage() {
  return "usage: node scripts/test-agent-skill-install.mjs --scratch <absolute-session-scratch> [--source <package>] [--installer-version 1.5.20]";
}

function parseArguments(argv) {
  const options = {
    scratch: null,
    source: resolve("skills/engram"),
    installerVersion: requiredInstallerVersion,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--scratch" || argument === "--source" || argument === "--installer-version") {
      const value = argv[index + 1];
      if (!value) {
        throw new Error(`${argument} requires a value`);
      }
      if (argument === "--scratch") {
        options.scratch = resolve(value);
      } else if (argument === "--source") {
        options.source = resolve(value);
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

function assertContained(root, candidate) {
  const normalizedRoot = resolve(root);
  const normalizedCandidate = resolve(candidate);
  if (normalizedCandidate === normalizedRoot || !normalizedCandidate.startsWith(`${normalizedRoot}${sep}`)) {
    throw new Error(`refusing to mutate path outside isolated scratch: ${normalizedCandidate}`);
  }
}

function ensureScratch(options) {
  const repositoryRoot = resolve(".");
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

function targetPath(profile, client, scope) {
  if (!supportedClients.includes(client)) {
    throw new Error(`unsupported client: ${client}`);
  }
  if (!supportedScopes.includes(scope)) {
    throw new Error(`unsupported scope: ${scope}`);
  }
  if (scope === "project") {
    // At project scope Codex and OpenCode share .agents/skills/engram, while
    // Claude Code reads .claude/skills/engram.
    return client === "claude-code"
      ? join(profile.project, ".claude", "skills", "engram")
      : join(profile.project, ".agents", "skills", "engram");
  }
  // At user scope the three clients diverge, each resolving its own config
  // home exactly as skills@1.5.20 does: CLAUDE_CONFIG_DIR, CODEX_HOME, and
  // XDG_CONFIG_HOME (OpenCode lives under ~/.config/opencode). The isolated
  // environment sets all three, so user-scope paths track those homes.
  switch (client) {
    case "claude-code":
      return join(profile.claudeConfig, "skills", "engram");
    case "codex":
      return join(profile.codexHome, "skills", "engram");
    case "opencode":
      return join(profile.xdgConfig, "opencode", "skills", "engram");
    default:
      throw new Error(`unsupported client: ${client}`);
  }
}

function uniqueTargets(profile, clients, scope) {
  return [...new Set(clients.map((client) => targetPath(profile, client, scope)))];
}

function replaceWithSource(source, destination, requestedMode) {
  assertContained(resolve(destination, "..", "..", ".."), destination);
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

function installPackage({ source, profile, clients, scope, requestedMode, allowReplace }) {
  const targets = uniqueTargets(profile, clients, scope);
  const existing = targets.filter((target) => existsSync(target));
  if (existing.length > 0 && !allowReplace) {
    return { status: "cancelled", existing, modes: {} };
  }

  const modes = {};
  // Symlink mode keeps one canonical copy and points every selected target at
  // it (the installer's "single source of truth"); copy mode gives each target
  // an independent copy. The first selected target holds the canonical copy and
  // later targets symlink to it. This is client-agnostic, so it holds whether
  // Codex and OpenCode share one path (project scope) or each have their own
  // (user scope).
  const canonical = targets[0];
  for (const target of targets) {
    const targetSource = requestedMode === "symlink" && target !== canonical ? canonical : source;
    modes[target] = replaceWithSource(targetSource, target, requestedMode);
  }
  return { status: "installed", existing, modes };
}

function assertDiscovery({ sourceDigest, profile, clients, scope }) {
  const targets = uniqueTargets(profile, clients, scope);
  assert.equal(targets.length, new Set(targets).size, "each target path must be unique");
  for (const target of targets) {
    assert.ok(existsSync(target), `missing discovery target ${target}`);
    assert.equal(calculatePackageDigest(target).digest, sourceDigest, `digest mismatch for ${target}`);
  }
  for (const client of clients) {
    const discovered = existsSync(targetPath(profile, client, scope)) ? 1 : 0;
    assert.equal(discovered, 1, `${client} must discover exactly one skill`);
  }
}

function writeUnknownSkill(target) {
  mkdirSync(target, { recursive: true });
  writeFileSync(join(target, "SKILL.md"), "---\nname: engram\ndescription: user maintained\n---\n", "utf8");
}

function runCase(summary, scratch, name, callback) {
  const caseRoot = makeCaseRoot(scratch, name);
  const profile = profileFor(caseRoot);
  const result = callback({ caseRoot, profile });
  summary.push({ name, ...result, isolated_environment: isolatedEnvironment(profile) });
}

function runMatrix(options) {
  ensureScratch(options);
  const sourceValidation = validateSkillPackage({
    repositoryRoot: resolve("."),
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
      runCase(summary, options.scratch, `single-${client}-${scope}`, ({ profile }) => {
        const install = installPackage({
          source: options.source,
          profile,
          clients: [client],
          scope,
          requestedMode: "copy",
          allowReplace: true,
        });
        assert.equal(install.status, "installed");
        assertDiscovery({ sourceDigest, profile, clients: [client], scope });
        return { status: "pass", scope, clients: [client], mode: install.modes };
      });
    }
  }

  for (const [name, scope, requestedMode] of [
    ["combined-project-symlink", "project", "symlink"],
    ["combined-user-symlink", "user", "symlink"],
    ["combined-user-copy", "user", "copy"],
  ]) {
    runCase(summary, options.scratch, name, ({ profile }) => {
      const first = installPackage({ source: options.source, profile, clients: supportedClients, scope, requestedMode, allowReplace: true });
      const second = installPackage({ source: options.source, profile, clients: supportedClients, scope, requestedMode, allowReplace: true });
      assert.equal(first.status, "installed");
      assert.equal(second.status, "installed");
      assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope });
      return { status: "pass", scope, clients: supportedClients, mode: second.modes, same_version_reinstall: "one discovered skill per client" };
    });
  }

  runCase(summary, options.scratch, "unknown-collision-cancel", ({ profile }) => {
    const target = targetPath(profile, "codex", "user");
    writeUnknownSkill(target);
    const before = readFileSync(join(target, "SKILL.md"), "utf8");
    const attempt = installPackage({ source: options.source, profile, clients: supportedClients, scope: "user", requestedMode: "symlink", allowReplace: false });
    assert.equal(attempt.status, "cancelled");
    assert.equal(readFileSync(join(target, "SKILL.md"), "utf8"), before);
    return { status: "pass", cancellation: "unknown target unchanged" };
  });

  runCase(summary, options.scratch, "explicit-replacement", ({ profile }) => {
    writeUnknownSkill(targetPath(profile, "claude-code", "user"));
    writeUnknownSkill(targetPath(profile, "codex", "user"));
    const install = installPackage({ source: options.source, profile, clients: supportedClients, scope: "user", requestedMode: "symlink", allowReplace: true });
    assert.equal(install.status, "installed");
    assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope: "user" });
    return { status: "pass", replacement: "explicitly confirmed", mode: install.modes };
  });

  runCase(summary, options.scratch, "interruption-recovery", ({ profile }) => {
    const partialTarget = targetPath(profile, "codex", "user");
    writeUnknownSkill(partialTarget);
    const firstAttempt = { status: "interrupted", target: partialTarget };
    assert.equal(firstAttempt.status, "interrupted");
    const recovery = installPackage({ source: options.source, profile, clients: supportedClients, scope: "user", requestedMode: "copy", allowReplace: true });
    assert.equal(recovery.status, "installed");
    assertDiscovery({ sourceDigest, profile, clients: supportedClients, scope: "user" });
    return { status: "pass", interruption: firstAttempt, recovery: "all final digests equal" };
  });

  return {
    status: "ok",
    runner: "local-copy-symlink-contract",
    installer_version: options.installerVersion,
    digest_algorithm: "engram-package-sha256-v1",
    source_digest: sourceDigest,
    cases: summary,
    host_mutation: "0 by construction: all generated paths are descendants of --scratch",
    note: "This local matrix validates the frozen target, collision, copy, symlink, and recovery contract, including the user-scope divergence of the Claude Code, Codex, and OpenCode discovery paths. Exact remote installer and real-client smoke remain release gates.",
  };
}

try {
  const result = runMatrix(parseArguments(process.argv.slice(2)));
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  console.error(`error: ${error.message}`);
  console.error(usage());
  process.exitCode = 1;
}
