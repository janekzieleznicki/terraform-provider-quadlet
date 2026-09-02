---
name: tf-framework-resource
description: "Use when adding, modifying, or reviewing a terraform-plugin-framework v1.19.0 resource implementation, including schema design, CRUD lifecycle methods, and state upgrade paths."
---

Procedure for adding or modifying a resource with `terraform-plugin-framework` v1.19.0. Every snippet is a compile-time pattern; do not fabricate API symbols.

## Compile-time interface assertions

Assert the required interfaces at compile time using the variable-shadowing idiom. This catches interface-missing errors at compile time rather than at runtime:

```go
var (
    _ resource.Resource                = &quadletUnitResource{}
    _ resource.ResourceWithConfigure   = &quadletUnitResource{}
    _ resource.ResourceWithImportState = &quadletUnitResource{}
)
```

Additional optional interfaces to assert when needed: `ResourceWithModifyPlan`, `ResourceWithUpgradeState`. The idiom matters because it guarantees the struct satisfies the contract without a test needing to exercise every method — if the struct drifts, the build fails immediately.

## Resource struct and Metadata

The resource struct holds provider-attached client state (e.g., a transport or HostClient). `Metadata` is implemented on the struct to return the resource type name and version:

```go
func (r *quadletUnitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
    resp.Name = "quadlet_unit"
}
```

## Schema

`Schema` declares the HCL schema for the resource. Use `schema.Schema` with `Attributes`, `Blocks`, and optionally `Description`. Computed attributes that are determined by the generator must be declared `Computed: true`. Required attributes declare `Required: true`. Optional attributes declare `Optional: true`.

## Configure

`Configure` receives the provider data and extracts the typed client:

```go
func (r *quadletUnitResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
    if req.ProviderData == nil {
        return
    }
    client, ok := req.ProviderData.(*HostClient)
    if !ok {
        resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *HostClient")
        return
    }
    r.client = client
}
```

## CRUD responsibilities

- **Create**: render HCL attributes into INI unit text, stage it, run `quadlet -dryrun` for validation, write the file, call `daemon-reload`, then `StartUnit`.
- **Read**: check the unit file exists on disk; if missing, call `resp.State.RemoveResource()` so Terraform re-creates the resource. Query systemd for `ActiveState`/`SubState` and sync into state.
- **Update**: write the updated INI file, run `daemon-reload`, issue `RestartUnit` (not `StartUnit` — a changed unit will not take effect with `start`).
- **Delete**: `StopUnit`, remove the `.container` file, run `daemon-reload`, clear the resource from state.

## Null vs Unknown vs zero-value

The framework distinguishes three states:

- **Null** — attribute has not been configured (zero-value of its type, e.g., `""` for string, `0` for int).
- **Unknown** — value will be determined later, typically at apply time (a `Computed` attribute not yet known at plan time).
- **Zero-value** — the default Go value; conflating this with null caused spurious diffs in legacy SDKv2.

Use the framework's typed helpers (`types.String`, `types.Bool`, etc.) to preserve exact semantics. When copying plan values to state, copy them directly; do not normalize or round-trip through Go zero-values.

## Computed attribute rule

Every Computed attribute **must** end up Known after apply, or Terraform errors `Provider produced inconsistent result after apply`. If a Computed value cannot be known at plan time, it **must** be left Unknown rather than guessed or populated with a placeholder. Setting a guessed value is the most common cause of this error.

## RequiresReplace vs UseStateForUnknown

Both are plan modifiers, but they answer different questions. `RequiresReplace` decides
*whether the resource is recreated*; `UseStateForUnknown` decides *what a Computed attribute
plans as*. They are unrelated to `DeprecationMessage`, which is purely a schema-level
practitioner warning.

- **`stringplanmodifier.RequiresReplace()`** — attach to a configurable attribute whose
  change alters the object's identity. In this provider that is `name`, `type`, and `scope`,
  because each one changes which file on disk the resource *is*. Editing `content` does not
  qualify: the same file is rewritten in place.
- **`stringplanmodifier.UseStateForUnknown()`** — attach to a Computed attribute that is
  stable across applies so the plan shows the prior value instead of `(known after apply)`.
  Correct for `path`, which is derived purely from `name`/`type`/`scope`. **Wrong** for
  `active_state`, which genuinely changes out of band and must refresh.

`generated_unit` takes neither. It is computed at plan time from the generator when
`content` is known, and left Unknown when `content` is not — see the Computed attribute rule
above.

## ImportState conventions

Implement `ResourceWithImportState` and its `ImportState` method. Terraform passes a single string ID; for composite IDs, split from that string inside the method:

```go
func (r *quadletUnitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
    // Composite ID is "<scope>:<name>.<type>", e.g. "user:web.container".
    scope, filename, ok := strings.Cut(req.ID, ":")
    if !ok {
        resp.Diagnostics.AddError(
            "Invalid Import ID",
            fmt.Sprintf("Expected \"<scope>:<name>.<type>\", got %q.", req.ID),
        )
        return
    }
    name, unitType, ok := strings.Cut(filename, ".")
    if !ok {
        resp.Diagnostics.AddError(
            "Invalid Import ID",
            fmt.Sprintf("Unit filename %q must carry an extension, e.g. \"web.container\".", filename),
        )
        return
    }
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("scope"), scope)...)
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
    resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), unitType)...)
}
```

## Schema version and StateUpgraders

`StateUpgraders` is **not** a field of `schema.Schema`. Only the integer `Version` lives
there; the upgraders come from the separate `ResourceWithUpgradeState` interface:

```go
func (r *quadletUnitResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
    return map[int64]resource.StateUpgrader{
        // Key is the PRIOR state version being upgraded FROM.
        0: {
            PriorSchema: &schema.Schema{ /* the v0 schema, verbatim */ },
            StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
                var old quadletUnitModelV0
                resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
                if resp.Diagnostics.HasError() {
                    return
                }
                resp.Diagnostics.Append(resp.State.Set(ctx, upgradeV0(old))...)
            },
        },
    }
}
```

Setting `PriorSchema` is optional but strongly preferred: with it, `req.State.Get` works
normally. Without it you must hand-decode `req.RawState`.

Do not invent upgrade schemas. Write an upgrader only when a real schema change requires
one, bump `schema.Schema.Version` in the same change, and cover it with a test that starts
from genuine prior-version state.
