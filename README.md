# terraform-provider-quadlet

Manage [Podman Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit files and their generated systemd services with Terraform or OpenTofu.

> ## Status: pre-alpha, not yet functional
>
> The repository currently contains scaffolding, design documents, and agent/skill
> definitions only. There is no provider binary to install and no resource you can
> apply yet. Watch the [roadmap](#roadmap) for the first usable release.

## Why a Quadlet provider

Existing Podman providers drive the Podman REST API socket directly. That creates
containers, but the host operating system never learns about them: they are absent from
systemd's dependency graph, they get no ordering guarantees against mounts or networks, and
their survival across a reboot depends on container-level restart policies rather than the
init system.

Quadlet inverts this. You declare a unit file, and a systemd generator turns it into a real
service unit at boot or on `daemon-reload`. This provider manages that declaration.

|  | REST API providers | This provider |
| --- | --- | --- |
| Host model | container engine endpoint | systemd-managed node |
| Reboot survival | container restart policy | native systemd unit activation |
| Ordering vs mounts/networks | none | `After=` / `Requires=` / `RequiresMountsFor=` |
| Supervision | Podman | systemd, via `sd_notify` |
| Pre-apply validation | none | Podman's own generator, at plan time |

That last row is the one worth dwelling on. Because the Podman generator supports a dry-run
mode, this provider validates your unit **during `terraform plan`** using Podman's real
parser — catching a misspelled `Imagee=` before anything touches the host — and surfaces the
generated systemd unit as a computed attribute, so the plan shows the actual unit that will
be installed.

## Requirements

| Component | Version |
| --- | --- |
| Terraform | >= 1.13 |
| OpenTofu | >= 1.9 (supported and tested alongside Terraform) |
| Podman on the target host | >= 5.0 |
| Target host init | systemd, with the Podman systemd generator installed |
| Go (to build from source) | >= 1.25 |

Rootless usage additionally requires `loginctl enable-linger <user>` so units start at boot
without an interactive login.

## Planned usage

The shape below is the milestone 1 design target. It is documentation of intent, not a
working example yet.

```hcl
terraform {
  required_providers {
    quadlet = {
      source = "janekzieleznicki/quadlet"
    }
  }
}

provider "quadlet" {
  scope = "user" # or "system" for /etc/containers/systemd
}

resource "quadlet_unit" "web" {
  name = "tf-web"
  type = "container"

  content = <<-EOT
    [Unit]
    Description=Demo web container

    [Container]
    Image=quay.io/libpod/alpine:latest
    PublishPort=8080:80

    [Install]
    WantedBy=default.target
  EOT
}

output "generated_service" {
  # The real systemd unit Podman's generator produced, known at plan time.
  value = quadlet_unit.web.generated_unit
}
```

Managing a remote host over SSH:

```hcl
provider "quadlet" {
  scope           = "system"
  host            = "ssh://deploy@nas.lan:22"
  ssh_private_key = file("~/.ssh/id_ed25519")
  sudo            = true
}
```

### A note on `enable`

There is deliberately **no `enable` attribute**. Quadlet-generated units live in a transient
generator directory and cannot be `systemctl enable`d. Boot activation is expressed with
`[Install] WantedBy=default.target` inside the unit content, which the generator
materialises for you. An `enable` attribute would imply a capability systemd does not offer
here.

## Development

Build and install into `GOBIN`:

```bash
make install
```

Point Terraform at your local build with a `dev_overrides` block in `~/.terraformrc`
(`%APPDATA%\terraform.rc` on Windows). With an override active, `terraform plan` and
`terraform apply` run the local binary directly and `terraform init` is bypassed entirely:

```hcl
provider_installation {
  dev_overrides {
    "janekzieleznicki/quadlet" = "/home/you/go/bin"
  }
  direct {}
}
```

Regenerate documentation after any schema change — CI fails on drift:

```bash
make docs
```

`make help` lists every target.

## Testing

Three tiers, because a provider that writes files and drives systemd cannot be honestly
covered by any single one.

```bash
make test              # tier 1: hermetic, in-process fake host, no side effects
make testacc-container # tier 2: full CRUD in a privileged systemd container
make testacc-tofu      # tier 2, executed against OpenTofu instead of Terraform
```

- **Tier 1** covers unit rendering, generator-output parsing, and command construction with
  no host interaction at all.
- **Tier 2** runs real `plan`/`apply`/`refresh`/`import`/`destroy` cycles against systemd
  running as PID 1 inside a container, including the SSH transport.
- **Tier 3** is a Vagrant/libvirt VM smoke run for the things containers cannot honestly
  prove: reboot persistence, lingering, and rootful system scope.

Acceptance tests require `TF_ACC=1` and silently skip without it. Units they create are
prefixed `tf-acc-` so `make sweep` can reap anything an interrupted run leaked.

## Roadmap

- **Milestone 1** — `quadlet_unit` raw resource: plan-time generator validation, local and
  SSH transports, rootful and rootless scopes, import support.
- **Milestone 2** — typed resources (`quadlet_container`, `quadlet_volume`,
  `quadlet_network`, `quadlet_pod`, `quadlet_kube`, `quadlet_build`, `quadlet_image`) with
  escape-hatch maps for directives the schema does not yet model.
- **Milestone 3** — D-Bus supervision for local hosts, replacing polled `systemctl show`
  with signal-driven job completion.
- **Milestone 4** — data sources for unit and service introspection.

## Design documents

- [`docs/terraform-podman-quadlet-provider.md`](docs/terraform-podman-quadlet-provider.md) —
  provider architecture research
- [`docs/developing-and-testing-terraform-rpoviders.md`](docs/developing-and-testing-terraform-rpoviders.md) —
  provider development and testing strategy

## License

MPL-2.0 — see [LICENSE](LICENSE).

