// Replays a stored plan — a checked-in JSON file mapping each line of a
// Gherkin scenario to a `sightkick call` invocation and an expectation —
// with no agent involved. An agent produces a plan once, by reading the
// manifest; this script is what runs it in CI, over and over, for free.
//
// A plan is only trustworthy if it still matches what it was planned against.
// Two hashes carry that check: `scenario.hash` (the .feature file's content)
// catches the spec changing under the plan; `irHash` (the compiled manifest)
// catches the corpus or tool layer changing under it. A freshly-authored plan
// has neither — run with --stamp once to compute and write them in; every
// run after that recomputes both and refuses to proceed on a mismatch unless
// --stale-ok is passed. This is deliberately the only thing that can fail a
// plan for reasons other than a real expectation mismatch.
//
// Usage:
//   node scripts/run-plan.mjs <plan.json> [--stamp] [--stale-ok] [--dry-run] [--no-session]
//
// Exit 0 on all steps' expectations holding, 1 otherwise.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function sha256(text) {
  return "sha256:" + createHash("sha256").update(text).digest("hex");
}

// The compiled IR is the thing a plan is really planned against — not the
// YAML source files directly, so a comment-only edit to the manifest doesn't
// spuriously invalidate every plan.
function computeIrHash(appDir) {
  const ir = execFileSync("sightkick", ["build", appDir], { encoding: "utf8" });
  return sha256(ir);
}

function computeScenarioHash(appDir, featurePath) {
  const text = readFileSync(resolve(REPO_ROOT, appDir, featurePath), "utf8");
  return sha256(text);
}

function runTool(appDir, tool, params = {}) {
  const args = ["call", appDir, tool];
  for (const [k, v] of Object.entries(params)) {
    args.push("--param", `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`);
  }
  args.push("--via", "cli");
  let stdout;
  try {
    stdout = execFileSync("sightkick", args, { encoding: "utf8" });
  } catch (err) {
    // sightkick exits non-zero on ok:false — that can be the CORRECT outcome
    // (a login expected to fail), so capture stdout from the error and let
    // the expectation, not the exit code, decide pass/fail.
    stdout = err.stdout ?? "";
  }
  try {
    return JSON.parse(stdout.trim());
  } catch {
    throw new Error(`sightkick call ${tool} did not print JSON:\n${stdout}`);
  }
}

// Deliberately small: ok, value.equals, value.contains, value.absent,
// list.length, list.contains. Anything a scenario needs beyond this is a
// sign the tool's `returns` needs a richer field, not that this vocabulary
// needs to grow to match it.
function checkExpect(result, expect) {
  if (expect == null) return { pass: true };
  if ("ok" in expect && Boolean(result.ok) !== expect.ok) {
    return { pass: false, reason: `expected ok:${expect.ok}, got ok:${Boolean(result.ok)}` };
  }
  if (expect.value) {
    const v = result.value;
    if (expect.value.absent && v !== undefined && v !== null && v !== "") {
      return { pass: false, reason: `expected value absent, got ${JSON.stringify(v)}` };
    }
    if ("equals" in expect.value && v !== expect.value.equals) {
      return { pass: false, reason: `expected value ${JSON.stringify(expect.value.equals)}, got ${JSON.stringify(v)}` };
    }
    if (expect.value.contains && !(typeof v === "string" && v.includes(expect.value.contains))) {
      return { pass: false, reason: `expected value to contain ${JSON.stringify(expect.value.contains)}, got ${JSON.stringify(v)}` };
    }
  }
  if (expect.list) {
    const items = result.items ?? [];
    if ("length" in expect.list && items.length !== expect.list.length) {
      return { pass: false, reason: `expected ${expect.list.length} row(s), got ${items.length}` };
    }
    if (expect.list.contains) {
      const found = items.some((row) => Object.entries(expect.list.contains).every(([k, v]) => row[k] === v));
      if (!found) return { pass: false, reason: `expected a row matching ${JSON.stringify(expect.list.contains)}` };
    }
  }
  return { pass: true };
}

async function main() {
  const args = process.argv.slice(2);
  const planPath = args.find((a) => !a.startsWith("--"));
  const stamp = args.includes("--stamp");
  const staleOk = args.includes("--stale-ok");
  const dryRun = args.includes("--dry-run");
  if (!planPath) {
    console.error("usage: node scripts/run-plan.mjs <plan.json> [--stamp] [--stale-ok] [--dry-run]");
    process.exit(2);
  }

  const plan = JSON.parse(readFileSync(planPath, "utf8"));
  const appDir = plan.app;

  const currentScenarioHash = computeScenarioHash(appDir, plan.scenario.feature);
  const currentIrHash = computeIrHash(appDir);

  if (stamp) {
    plan.scenario.hash = currentScenarioHash;
    plan.irHash = currentIrHash;
    plan.generatedAt = new Date().toISOString();
    writeFileSync(planPath, JSON.stringify(plan, null, 2) + "\n");
    console.log(`stamped ${planPath}`);
    return;
  }

  if (plan.scenario.hash && plan.scenario.hash !== currentScenarioHash && !staleOk) {
    console.error(`✗ ${plan.scenario.feature} has changed since this plan was stamped — re-plan (or pass --stale-ok).`);
    process.exit(1);
  }
  if (plan.irHash && plan.irHash !== currentIrHash && !staleOk) {
    console.error(`✗ ${appDir}'s compiled manifest has changed since this plan was stamped — re-plan (or pass --stale-ok).`);
    process.exit(1);
  }
  if (!plan.scenario.hash || !plan.irHash) {
    console.error(`⚠ ${planPath} is unstamped — run with --stamp to lock its hashes. Proceeding anyway.`);
  }

  console.log(`\n${plan.scenario.name}\n`);

  let failed = false;
  for (const step of plan.steps) {
    if (!step.tool) {
      console.log(`  · ${step.gherkin}`);
      continue;
    }
    if (dryRun) {
      const paramStr = step.params
        ? " " + Object.entries(step.params).map(([k, v]) => `--param ${k}="${v}"`).join(" ")
        : "";
      console.log(`  ${step.gherkin}\n    sightkick call ${appDir} ${step.tool}${paramStr} --via cli`);
      continue;
    }
    const result = runTool(appDir, step.tool, step.params);
    const { pass, reason } = checkExpect(result, step.expect);
    console.log(`  ${pass ? "✓" : "✗"} ${step.gherkin}`);
    if (!pass) {
      console.log(`      ${reason}\n      result: ${JSON.stringify(result)}`);
      failed = true;
      break;
    }
  }

  process.exit(failed ? 1 : 0);
}

main();
