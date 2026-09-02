---
name: provider-test-tiers
description: "Use when running or authoring tests for the terraform-provider-quadlet provider, including hermetic unit tests, TF_ACC acceptance tests, and Vagrant VM smoke tests."
---

Operational runbook for the three-tier test model. Each tier has a distinct scope, command, speed, and set of guarantees.

## Tier 1 — Hermetic unit tests

- **What it covers**: `ini` rendering, quadlet stdout/stderr parsing (golden files), systemctl argv construction, scope path resolution. In-process fake transport; no Terraform CLI, no host systemd.
- **Command**: `go test ./...` (no TF_ACC).
- **Speed**: Seconds.
- **Does not prove**: Real Terraform plan/apply, real systemd daemon-reload, real Podman container lifecycle.
- **Canonical target**: `make test`

## Tier 2 — Acceptance tests

- **What it covers**: Full plan/apply/refresh/import/destroy against a privileged `quay.io/podman/stable` container running systemd as PID 1. Includes SSH transport via sshd inside the container. Test unit names are prefixed `tf-acc-`.
- **Command**: `TF_ACC=1 go test ./internal/provider/ -v`
- **Speed**: Minutes (container boot + full Terraform lifecycle).
- **Does not prove**: Reboot persistence, loginctl linger behavior, rootful system scope.
- **Canonical target**: `make testacc`
- **TF_ACC gating**: Tests skip without `TF_ACC=1`. There is no fallback; unsetting TF_ACC silently skips all acceptance steps.

## Tier 3 — VM smoke tests

- **What it covers**: Vagrant/libvirt VMs. Reboot persistence, loginctl linger, rootful system scope. Manual or nightly.
- **Command**: `vagrant up && vagrant provision` (or the project-specific Vagrant target).
- **Speed**: Ten-to-thirty minutes.
- **Does not prove**: Terraform plugin correctness in isolation; validates the full stack.
- **Canonical target**: `make testacc-container` (or the project-specific VM target).

## Terraform CLI resolution precedence

terraform-plugin-testing resolves the Terraform CLI in this order:

1. `TF_ACC_TERRAFORM_PATH` (exact binary path)
2. `TF_ACC_TERRAFORM_VERSION` (downloads from HashiCorp releases)
3. PATH lookup
4. Downloads latest

No version check is performed against an existing binary at `TF_ACC_TERRAFORM_PATH`. If `TF_ACC_TERRAFORM_PATH` is set to a non-executable, tests fail — there is no validation of the path.

## OpenTofu matrix invocation

The verified OpenTofu invocation (no fork of terraform-plugin-testing exists):

```bash
TF_ACC=1 TF_ACC_TERRAFORM_PATH=$(command -v tofu) TF_ACC_PROVIDER_HOST=registry.opentofu.org TF_ACC_PROVIDER_NAMESPACE=janekzieleznicki go test ./internal/provider/ -v
```

## Sweeper convention

Acceptance-created units use the `tf-acc-` prefix to distinguish them from developer fixtures. Sweepers are registered via `resource.AddTestSweepers` in `internal/provider/sweeper_test.go`. They reap leaked unit files and stop leaked systemd units. Invoked via `make sweep`.

## Real assertion API symbols

From the `terraform-plugin-testing` and `terraform-plugin-framework` packages:

- **State checks**: `statecheck.ExpectKnownValue`, `statecheck.ExpectSensitiveValue`, `statecheck.ExpectKnownOutputValue`, `statecheck.ExpectIdentity`
- **Plan checks**: `plancheck.ExpectEmptyPlan`, `plancheck.ExpectNonEmptyPlan`, `plancheck.ExpectResourceAction`, `plancheck.ExpectKnownValue`
- **Known value matchers**: `knownvalue.StringExact`, `Bool`, `Int64`, `MapExact`, `ListExact`, `NotNull`
- **TF JSON path**: `tfjsonpath.New(...).AtMapKey(...)`
- **Version guards**: `tfversion.SkipBelow`, `tfversion.RequireAbove`

## Representative acceptance test skeleton

Use `ConfigStateChecks`, **not** `Check` with `resource.ComposeAggregateTestCheckFunc`.
`statecheck.ExpectKnownValue` returns a `statecheck.StateCheck`, which is a different type
from the legacy `resource.TestCheckFunc` — mixing them does not compile.

```go
func TestAccQuadletUnit_basic(t *testing.T) {
    resource.Test(t, resource.TestCase{
        PreCheck:                 func() { testAccPreCheck(t) },
        ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
        Steps: []resource.TestStep{
            {
                Config: testAccQuadletUnitConfig("tf-acc-web", "quay.io/libpod/alpine:latest"),
                ConfigStateChecks: []statecheck.StateCheck{
                    statecheck.ExpectKnownValue(
                        "quadlet_unit.test",
                        tfjsonpath.New("active_state"),
                        knownvalue.StringExact("active"),
                    ),
                    statecheck.ExpectKnownValue(
                        "quadlet_unit.test",
                        tfjsonpath.New("unit_name"),
                        knownvalue.StringExact("tf-acc-web.service"),
                    ),
                },
            },
            {
                // No-op replan: proves the resource is stable and that no
                // computed attribute is being recomputed on every plan.
                Config: testAccQuadletUnitConfig("tf-acc-web", "quay.io/libpod/alpine:latest"),
                ConfigPlanChecks: resource.ConfigPlanChecks{
                    PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
                },
            },
            {
                ResourceName:      "quadlet_unit.test",
                ImportState:       true,
                ImportStateVerify: true,
                ImportStateId:     "user:tf-acc-web.container",
            },
        },
    })
}
```

The middle step is the one that catches Null/Unknown mistakes: if a computed attribute is
recomputed rather than held, `ExpectEmptyPlan` fails and reveals a perpetual diff.

## Troubleshooting

- **Acceptance tests silently skipping**: `TF_ACC` is unset. Set `TF_ACC=1` before running. Verify with `echo $TF_ACC` in the same shell as the test command.
- **Flaky parallel failures**: Terraform applies resources in parallel (default `-parallelism=10`). Concurrent `daemon-reload` + `start` against one systemd instance is racy. Cross-reference the per-scope mutex in `internal/engine` — the critical section serializing reload-then-start must not be bypassed.
- **Units left behind after an interrupted run**: Sweepers do not run if the test process is killed mid-run. Manually inspect for `tf-acc-*` unit files in the target Quadlet directory and stop them with `systemctl --user stop tf-acc-<name>.service` before re-running. Use `make sweep` to invoke the registered sweepers cleanly.
