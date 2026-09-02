---
name: quadlet-dryrun-contract
description: "Use when verifying Podman Quadlet generator behavior, debugging dry-run validation failures, or implementing provider-side parsing of quadlet -dryrun stdout/stderr. Encodes the verified external contract the whole provider depends on."
---

This skill encodes the empirically verified `quadlet -dryrun` contract that the `terraform-provider-quadlet` provider depends on for pre-flight validation and unit-name discovery. Note it is a standalone generator binary with single-dash flags, not a `podman` subcommand. Every statement below is verified against Podman 5.8.2; do not deviate from it.

## Binary and invocation

- **Binary path**: `/usr/libexec/podman/quadlet`
- **Symlinks**: `/usr/lib/systemd/system-generators/podman-system-generator` and `/usr/lib/systemd/user-generators/podman-user-generator`
- **Flags are single-dash only**: `-dryrun`, `-user`, `-v`, `-no-kmsg-log`, `-version`. No double-dash equivalents.
- **`-dryrun` takes no positional argument and writes nothing to disk.** Upstream never reads or creates an output path in dryrun mode, so dryrun has **zero filesystem side effects**.
- **Canonical invocation shape**:
  ```bash
  QUADLET_UNIT_DIRS=$STAGE /usr/libexec/podman/quadlet -user -dryrun
  ```
  - `-dryrun` implies `enableDebug()` and routes all log output to stderr (lines 75-76), so stderr interleaves informational debug lines with real diagnostics and must be filtered.
  - `QUADLET_UNIT_DIRS` is **colon-separated**, each entry **must be absolute** or it is rejected, and when set it **replaces all default search directories**. `AppendSubPaths` **recurses into subdirectories** and skips any directory whose name ends `.d`.
## QUADLET_UNIT_DIRS isolation semantics

`QUADLET_UNIT_DIRS=<dir>` overrides the default search path and isolates validation to the staged files in that directory only. It is honored in dry-run mode. This is how the provider scopes validation to the single unit file under test without touching the host's live Quadlet directories.

## Exit codes

- **0** — all staged files valid.
- **1** — at least one error occurred. There is no partial-success mode; the generator aggregates all errors and exits 1.

## Stdout delimiter and unit-name discovery

On success, generated systemd units are printed to STDOUT. Each generated unit is preceded by a **delimiter line** of the form:

```
---<unitname>.service---
```

For example, a staged file named `good.container` produces the delimiter `---good.service---`. The provider MUST parse this delimiter to learn the generated unit name. **The provider MUST NOT reimplement quadlet's file-name-to-unit-name mapping rules** — the delimiter is the authoritative source of truth for the output unit name.

## Stderr diagnostic shapes

On failure, STDERR carries diagnostics. Both verified shapes are prefixed `quadlet-generator[<pid>]: `:

**Shape 1 — malformed/unknown key inside a section:**
```
converting "typo.container": unsupported key 'Imagee' in group 'Container' in /path/typo.container
```

**Shape 2 — syntax error (not a valid key-value pair, group, or comment):**
```
error loading "/path/broken.container", file contains line 1: “[Container” which is not a key-value pair, group, or comment
```

Note the curly quotes around the offending line in shape 2 — they are literal in the generator output. Match on the surrounding text, not on the quote characters.

A trailing line `processing encountered some errors` is also emitted by the generator.

## Hard errors, no permissive mode

Misspelled or unknown directive keys are **hard errors** (exit 1). Missing required keys are **hard errors** (e.g., `no Image or Rootfs key specified`). **There is no permissive mode.** The provider must treat any non-zero exit as a validation failure.

## Correction to existing documentation

**Explicit correction**: `docs/terraform-podman-quadlet-provider.md` contains a lifecycle table row stating "Generator ignores invalid files". That statement is true at boot/daemon-reload time (the generator silently skips files it cannot parse during normal systemd startup), but it is **FALSE under `-dryrun`**, which aggregates errors and exits 1. The provider's validation logic depends on the dry-run behavior, not the boot-time behavior. Do not rely on the "ignores invalid files" semantics when implementing pre-flight checks.

## Re-verification shell block

Each case MUST get its own staging directory. Staging a bad unit alongside a good one makes
the whole run exit 1, because the generator aggregates errors across every file it finds.

```bash
#!/usr/bin/env bash
# Deliberately no `set -e`: case 2 is expected to fail, and we want its exit code.
set -uo pipefail

QUADLET=/usr/libexec/podman/quadlet
GOOD=$(mktemp -d)
BAD=$(mktemp -d)
trap 'rm -rf "$GOOD" "$BAD"' EXIT

echo "=== case 1: valid unit (expect exit 0, delimiter on stdout) ==="
QUADLET_UNIT_DIRS="$GOOD" "$QUADLET" -user -dryrun >/tmp/q.out 2>/tmp/q.err
echo "exit=$?"
grep -- '^---.*\.service---$' /tmp/q.out || echo "NO DELIMITER FOUND (contract broken)"

echo "=== case 2: misspelled key (expect exit 1, diagnostic on stderr) ==="
QUADLET_UNIT_DIRS="$BAD" "$QUADLET" -user -dryrun >/tmp/q2.out 2>/tmp/q2.err
echo "exit=$?"
cat /tmp/q2.err
```

Expected: case 1 exits 0 and prints `---good.service---`; case 2 exits 1 and prints
`converting "typo.container": unsupported key 'Imagee' in group 'Container'`.

## Provider diagnostics mapping

Each stderr line parsed from the generator becomes one `Diagnostics.AddError`, attributed to
the `content` attribute path of the resource under validation. Use `AddError` only — because
there is no permissive mode, every generator complaint is fatal, and downgrading any of them
to a warning would let an un-generatable unit reach the host.

The `---<unitname>.service---` stdout delimiter populates the resource's computed
`unit_name`, and the unit body that follows it populates `generated_unit`.
