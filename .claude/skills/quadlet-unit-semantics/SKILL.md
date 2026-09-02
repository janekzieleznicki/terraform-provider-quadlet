---
name: quadlet-unit-semantics
description: "Use when implementing or debugging Podman Quadlet unit lifecycle, systemd generator behavior, or provider resource semantics. Captures the domain gotchas that an implementer must internalize before writing CRUD logic."
---

The Podman/systemd domain gotchas an implementer must internalize before writing CRUD logic. These are not optional conventions — they are hard requirements derived from how Quadlet and systemd actually behave.

## Lifecycle: file deposition -> daemon-reload -> generator -> start

The host lifecycle requires an explicit multi-step execution sequence:

1. **File Deposition**: The `.container` unit file is written to the appropriate directory (e.g., `~/.config/containers/systemd/app.container`).
2. **Generator Invocation**: `systemctl --user daemon-reload` triggers `podman-system-generator`. The generator parses the file, validates syntax, and writes a generated service file (e.g., `app.service`) into the transient runtime unit directory `/run/user/$UID/systemd/generator/`.
3. **Service Activation**: `systemctl --user start app.service` instructs systemd to execute the generated `ExecStart` directive, launching `podman run`.
4. **Process Tracking**: Podman uses `sd_notify` (`Notify=true`) or process matching to communicate container health back to systemd, transitioning the service state to `active (running)`.

**Writing a file alone does nothing.** Without `daemon-reload`, the generator never runs and the unit is invisible to systemd.

## Seven unit types and their section headers

| Extension | Section Header | Description |
|-----------|---------------|-------------|
| `.container` | `[Container]` | Runs `podman run` |
| `.volume` | `[Volume]` | Creates a named volume |
| `.network` | `[Network]` | Creates a CNI/Netavark network |
| `.pod` | `[Pod]` | Manages a Podman Pod |
| `.kube` | `[Kube]` | Runs `podman kube play` |
| `.build` | `[Build]` | Builds an image via `podman build` |
| `.image` | `[Image]` | Pulls/caches an image |

## Rootful vs rootless directories

- **Rootful**: `/etc/containers/systemd/` or `/usr/share/containers/systemd/`. Runs as root.
- **Rootless preferred**: `~/.config/containers/systemd/`. Requires `loginctl enable-linger <user>` for boot-without-login. Without linger, the user's systemd instance does not start at boot and units do not activate automatically.

## Never systemctl enable a generated unit

Generated Quadlet units live in a transient generator directory and **CANNOT be `systemctl enable`d**. Boot activation is expressed as `[Install] WantedBy=default.target` inside the unit file content, which the generator materializes. **Therefore the provider exposes NO `enable` attribute.**

This is established house doctrine: the sibling repo's `proxmox-specialist` agent independently states the same never-enable rule ("generated Quadlet units must never be `systemctl enable`d; use `start` on first install and `restart` (not `start`) on every redeploy"). Treat this as non-negotiable.

## Use restart, not start, on content change

On redeploy of changed content, use `restart`, not `start`. A changed unit that is merely started will not take effect because systemd considers the unit already active; `restart` forces a full stop-start cycle with the new configuration.

## Terraform parallelism / daemon-reload race

Terraform applies resources in parallel by default (`-parallelism=10`). Concurrent `daemon-reload` + `start` against one systemd instance is racy and can cause `Unit is busy` or lost state transitions. The remedy is a per-scope mutex in `internal/engine` that serializes the reload-then-start critical section. The provider must not issue `daemon-reload` and `start` concurrently for units in the same scope.

## Unit-name discovery from dryrun stdout

Generated unit names must be discovered from `quadlet -dryrun` stdout by parsing the `---<unitname>.service---` delimiter. Do not derive the unit name from the filename using your own mapping rules — quadlet's internal name-mapping logic is the source of truth and may change across Podman versions. The delimiter is the authoritative contract.
