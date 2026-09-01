# terraform-provider-quadlet

A Terraform/OpenTofu provider that manages **Podman Quadlet unit files** and the systemd
services generated from them. Unlike REST-API-backed Podman providers, this one targets
systemd's native init layer: Terraform writes declarative unit files, triggers the Podman
systemd generator, and supervises the resulting services. Containers therefore participate
in the host's real service dependency graph and survive reboots without container-level
restart hacks.

- Go module: `github.com/janekzieleznicki/terraform-provider-quadlet`
- Provider binary: `terraform-provider-quadlet`
- Registry address: `janekzieleznicki/quadlet` — HCL block name `quadlet`
- Resource naming: `quadlet_unit`, later `quadlet_container` etc. **Never** `podman_quadlet_*`.

> The Terraform Registry requires the GitHub repository to be named exactly
> `terraform-provider-quadlet`. The local directory name is irrelevant, but the remote
> must match or publishing fails.

## Status

Phase 0 (repository scaffolding) only. There is no `main.go` and no `internal/` code yet;
the provider does not build into anything usable. Do not describe it as working software.

## Commands

| Command | Purpose |
| --- | --- |
| `make build` | Compile the provider binary |
| `make install` | `go install` into `GOBIN` for use with `dev_overrides` |
| `make lint` | `golangci-lint run` (config schema v2) |
| `make test` | Tier 1 hermetic tests, no host interaction |
| `make testacc` | Tier 2 acceptance against Terraform |
| `make testacc-tofu` | Tier 2 acceptance against OpenTofu |
| `make testacc-container` | Tier 2 inside the privileged systemd container |
| `make docs` | `go tool tfplugindocs generate` |
| `make sweep` | Reap leaked `tf-acc-*` units |

`tfplugindocs` is pinned via a `tool` directive in `go.mod`, so `make docs` needs no
separate install. Terraform CLI is **not** installed on this workstation; OpenTofu 1.12.6
is. Targets that need `terraform` must degrade gracefully.

## Architecture

The load-bearing invariant:

> **Transport is *where* a command runs. systemd is *what* command runs.**

`internal/systemd` composes a `transport.Transport` and never learns whether it is local or
remote. This is the entire reason SSH support costs one file instead of a parallel code
path, and it is why the systemctl-CLI approach was chosen over D-Bus for milestone 1.

```
main.go                 providerserver.Serve + -debug flag
internal/provider/      framework plumbing ONLY; quadlet_unit resource
internal/engine/        reconcile orchestration + per-scope daemon-reload mutex
internal/ini/           unit-file rendering            (pure, no I/O)
internal/quadlet/       dryrun invocation + output parsing (pure parsing, no I/O)
internal/systemd/       Manager interface + systemctl implementation
internal/scope/         rootful/rootless path + systemctl flag resolution
internal/transport/     Transport interface + local, ssh, fake
test/container/         tier 2 harness
test/vm/                tier 3 Vagrant smoke
```

`ini`, `quadlet`, and `scope` hold most of the logic and must stay I/O-free. That is what
makes tier 1 able to cover them exhaustively against `transport/fake`.

## The quadlet dryrun contract

Empirically verified on Podman 5.8.2. Full detail in `.claude/skills/quadlet-dryrun-contract`.

```
QUADLET_UNIT_DIRS=$STAGE /usr/libexec/podman/quadlet -user -dryrun
```

- Single-dash flags. -dryrun takes no positional argument and writes nothing to disk.
- `QUADLET_UNIT_DIRS` isolates validation to staged files and is honoured in dryrun mode.
- Exit `0` = every staged file valid. Exit `1` = at least one error.
- Success prints generated units to **stdout**, each preceded by `---<name>.service---`.
- Failure prints to **stderr** in two shapes, both prefixed `quadlet-generator[<pid>]: `:
  - `converting "<file>": <message>`
  - `error loading "<path>", <message>`

Two consequences that must not be re-litigated:

1. **Unknown directive keys are hard errors** (`unsupported key 'Imagee' in group
   'Container'`). There is no permissive mode. The raw `quadlet_unit` resource therefore
   inherits Podman's complete validation without a typed schema.
2. **Never reimplement quadlet's file-name-to-unit-name mapping.** Parse the
   `---<name>.service---` delimiter from stdout instead. Derived rules rot across Podman
   versions; the generator's own answer does not.

`docs/terraform-podman-quadlet-provider.md` contains an error: its lifecycle table says
"Generator ignores invalid files." That holds at boot, **not** under `-dryrun`, which
aggregates errors and exits 1. Provider logic depends on the dryrun behaviour.

## Non-negotiable domain rules

- **No `enable` attribute, ever.** Quadlet-generated units live in a transient generator
  directory and cannot be `systemctl enable`d. Boot activation is `[Install]
  WantedBy=default.target` *inside the unit content*, which the generator materialises. An
  `enable` attribute would be a lie. (`provision-home-nas/.omp/agents/proxmox-specialist.md`
  states the same rule independently — this is established house doctrine.)
- **`restart`, not `start`, when content changes.** A `start` on an already-active unit is a
  no-op, so a changed unit silently fails to take effect.
- **Writing a unit file does nothing.** Nothing happens until
  `systemctl [--user] daemon-reload` runs the generator.
- **Serialize daemon-reload.** Terraform applies resources in parallel (default
  `-parallelism=10`). Concurrent reload+start against one systemd instance is racy;
  `internal/engine` holds a per-scope mutex over the reload→start critical section. Absent
  this, the acceptance suite flakes nondeterministically. Flakes here are a concurrency
  defect — never paper over them with sleeps or retries.
- **Inject env explicitly over SSH.** `sshd` refuses `SendEnv` by default, so
  `QUADLET_UNIT_DIRS` must be passed as `env K=V -- cmd`, not via the SSH environment.
- Unit directories: rootful `/etc/containers/systemd/`, rootless
  `$XDG_CONFIG_HOME/containers/systemd/` (falling back to `$HOME/.config/containers/systemd/`), mirroring Go's `os.UserConfigDir()`. Rootless boot-without-login needs
  `loginctl enable-linger <user>`.

## Terraform framework rules

Using `terraform-plugin-framework` v1.19.0. Detail in `.claude/skills/tf-framework-resource`.

- Respect the `Null` / `Unknown` / zero-value distinction. Collapsing them is the SDKv2
  failure mode this framework exists to prevent.
- Every `Computed` attribute must be **Known** after apply or Terraform fails with
  `Provider produced inconsistent result after apply`.
- Corollary: when `content` is unknown at plan time (interpolated), `generated_unit` must
  be left **Unknown**. Do not guess a value — validation is impossible and a concrete guess
  produces the inconsistent-result error.
- `name`, `type`, and `scope` are `RequiresReplace`: each changes the on-disk file identity.
- `Read` must call `resp.State.RemoveResource` when the unit file is gone, so Terraform
  plans a recreate.
- `generated_unit` and `active_state` must NOT be computed in `ModifyPlan`. Set only in `applyPlan` (Create/Update) and `Read`, else Terraform reports inconsistent state when temp paths differ between plan and apply.

## Testing

| Tier | Where | Gate | Proves |
| --- | --- | --- | --- |
| 1 hermetic | in-process, fake transport | `make test` | rendering, output parsing, argv construction, path resolution |
| 2 acceptance | privileged `quay.io/podman/stable`, systemd as PID 1 | `TF_ACC=1` | full CRUD + import + SSH transport |
| 3 VM smoke | Vagrant/libvirt | manual/nightly | reboot persistence, linger, rootful scope |

Tier 1 golden files are seeded from **real captured generator output**, not hand-written
guesses. Acceptance-created units use the `tf-acc-` prefix so sweepers can reap them.

OpenTofu acceptance runs work through documented env vars — there is no OpenTofu fork of
`terraform-plugin-testing`:

```bash
TF_ACC=1 TF_ACC_TERRAFORM_PATH=$(command -v tofu) \
TF_ACC_PROVIDER_HOST=registry.opentofu.org \
TF_ACC_PROVIDER_NAMESPACE=janekzieleznicki go test ./internal/provider/ -v
```

CLI resolution precedence: `TF_ACC_TERRAFORM_PATH` → `TF_ACC_TERRAFORM_VERSION` (downloads)
→ `PATH` → download latest. No version check is applied to a binary at
`TF_ACC_TERRAFORM_PATH`, which is why pointing it at `tofu` works.

Assertion APIs are `statecheck` / `plancheck` / `knownvalue` / `tfjsonpath`, not legacy
`TestCheckResourceAttr` string checks. Verified symbols include
`statecheck.ExpectKnownValue`, `plancheck.ExpectResourceAction`, `knownvalue.StringExact`,
`tfjsonpath.New`. Do not invent others — check the module source.


Acceptance test config builder tips:
- HCL strings must be multiline backtick format: `` Config: `resource "..."\n{ ... }` `` (single-line HCL breaks parsing).
- Container image tags: prefer `busybox:latest` over specific alpine versions; not all tags are locally available during pull.
- Sweeper cleanup requires `TestMain(m *testing.M)` calling `resource.AddTestSweepers` and `m.Run()`.
- Skip optional tests gracefully: `if os.Getenv("ENV_VAR") == "" { t.Skip("reason") }` — do not fail hard.
## Subagents

| Agent | Owns | Hard boundary |
| --- | --- | --- |
| `quadlet-provider-implementer` | `main.go`, `internal/**` non-test | No stubs, shims, or weakened tests |
| `provider-qa-e2e` | `test/**`, `*_test.go`, CI | **Must not edit non-test production code** — report defects instead |
| `quadlet-schema-cartographer` | typed schemas, milestone 2+ | Every directive proven via real `-dryrun`, never recalled from memory |

## Implementation patterns

- **Package comments:** Go linter requires comment directly before `package` declaration with no blank line: `// Package foo <description>\npackage foo`.
- **systemd.Manager:** Construct directly as a struct (`&systemd.Manager{Transport: t, Scope: s}`); no factory function exists.
- **Engine read-only methods:** `Engine.Path(ctx, scope, filename)` resolves paths without I/O (plan-safe); `Engine.Validate(ctx, scope, filename, content)` validates without side effects (plan-safe).
- **Import composite ID:** Extract from import request as `scope:name.type` (e.g., `user:web.container`); split and validate before setting state.

## Traps

- A prior open-ended attempt to map Podman's full directive surface ran over an hour and
  produced nothing. Bound that work per unit type and persist findings incrementally.
- Quadlet supports **eight** unit types, not seven: `.container`, `.volume`, `.network`, `.pod`, `.kube`, `.build`, `.image`, and `.artifact` (service suffix `-artifact`).
- `golangci-lint` here is 2.13.2 → config **schema version 2**. v1 keys such as
  `linters-settings` or top-level `run.skip-dirs` are rejected.
- The direct dependencies in `go.mod` are still marked `// indirect` because no source
  imports them yet. `go mod tidy` corrects this once Phase 1 lands — it is not a defect.
