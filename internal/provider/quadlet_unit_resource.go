package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/engine"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/quadlet"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/scope"
)

// quadletUnitResourceModel describes the resource data model.
type quadletUnitResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	Type          types.String `tfsdk:"type"`
	Scope         types.String `tfsdk:"scope"`
	Content       types.String `tfsdk:"content"`
	Path          types.String `tfsdk:"path"`
	UnitName      types.String `tfsdk:"unit_name"`
	GeneratedUnit types.String `tfsdk:"generated_unit"`
	ActiveState   types.String `tfsdk:"active_state"`
}

// quadletUnitResource defines the resource implementation.
type quadletUnitResource struct {
	engine *engine.Engine
}

// NewQuadletUnitResource is a helper function to simplify the provider implementation.
func NewQuadletUnitResource() resource.Resource {
	return &quadletUnitResource{}
}

var (
	_ resource.Resource                   = &quadletUnitResource{}
	_ resource.ResourceWithConfigure      = &quadletUnitResource{}
	_ resource.ResourceWithImportState    = &quadletUnitResource{}
	_ resource.ResourceWithModifyPlan     = &quadletUnitResource{}
	_ resource.ResourceWithValidateConfig = &quadletUnitResource{}
)

// Metadata returns the resource type name.
func (r *quadletUnitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_unit"
}

// Schema defines the schema for the resource.
func (r *quadletUnitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A raw Podman Quadlet unit file, validated against the real quadlet generator at plan time.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Composite identifier: \"<scope>:<name>.<type>\".",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "Base name of the unit file, without extension.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   fmt.Sprintf("Quadlet unit type. One of: %v.", quadlet.Types()),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"scope": schema.StringAttribute{
				Required:      true,
				Description:   "systemd scope: \"system\" or \"user\".",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"content": schema.StringAttribute{
				Required:    true,
				Description: "Raw INI content of the unit file.",
			},
			"path": schema.StringAttribute{
				Computed:      true,
				Description:   "Absolute path of the unit file on the target host.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"unit_name": schema.StringAttribute{
				Computed:    true,
				Description: "Systemd service unit name discovered from the generator, e.g. \"web.service\".",
			},
			"generated_unit": schema.StringAttribute{
				Computed:    true,
				Description: "The systemd unit file the generator produced from content.",
			},
			"active_state": schema.StringAttribute{
				Computed:    true,
				Description: "Current systemd ActiveState (e.g. \"active\", \"failed\").",
			},
		},
	}
}

// Configure adds the provider configured client to the resource.
func (r *quadletUnitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	eng, ok := req.ProviderData.(*engine.Engine)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *engine.Engine, got: %T. Report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.engine = eng
}

// ValidateConfig validates the resource configuration.
func (r *quadletUnitResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config quadletUnitResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Type.IsNull() && !config.Type.IsUnknown() && !quadlet.IsValidType(config.Type.ValueString()) {
		resp.Diagnostics.AddAttributeError(path.Root("type"), "Invalid Unit Type", fmt.Sprintf("type must be one of %v, got %q", quadlet.Types(), config.Type.ValueString()))
	}
	if !config.Scope.IsNull() && !config.Scope.IsUnknown() {
		if _, err := scope.Parse(config.Scope.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("scope"), "Invalid Scope", err.Error())
		}
	}
}

// ModifyPlan modifies the plan to compute path and unit information.
func (r *quadletUnitResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.engine == nil {
		return
	}

	var plan quadletUnitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Name.IsUnknown() || plan.Type.IsUnknown() || plan.Scope.IsUnknown() {
		return
	}

	s, err := scope.Parse(plan.Scope.ValueString())
	if err != nil {
		return // ValidateConfig already reported this.
	}
	filename := quadlet.Filename(plan.Name.ValueString(), plan.Type.ValueString())

	fullPath, err := r.engine.Path(ctx, s, filename)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Resolve Unit Path", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("path"), fullPath)...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s:%s", s, filename))...)

	if plan.Content.IsUnknown() {
		return
	}
	result, problems, err := r.engine.Validate(ctx, s, filename, []byte(plan.Content.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Unable to Validate Unit", err.Error())
		return
	}
	if len(problems) > 0 {
		for _, p := range problems {
			resp.Diagnostics.AddAttributeError(path.Root("content"), "Invalid Quadlet Unit", p.Message)
		}
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("unit_name"), result.UnitName)...)
}

// applyPlan is a helper function shared by Create and Update.
func (r *quadletUnitResource) applyPlan(ctx context.Context, plan *quadletUnitResourceModel, diags *diag.Diagnostics) {
	s, err := scope.Parse(plan.Scope.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root("scope"), "Invalid Scope", err.Error())
		return
	}
	filename := quadlet.Filename(plan.Name.ValueString(), plan.Type.ValueString())

	state, problems, err := r.engine.Apply(ctx, engine.ApplyRequest{
		Scope:    s,
		Filename: filename,
		Content:  []byte(plan.Content.ValueString()),
	})
	if err != nil {
		diags.AddError("Unable to Apply Quadlet Unit", err.Error())
		return
	}
	if len(problems) > 0 {
		for _, p := range problems {
			diags.AddAttributeError(path.Root("content"), "Invalid Quadlet Unit", p.Message)
		}
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", s, filename))
	plan.Path = types.StringValue(state.Path)
	plan.UnitName = types.StringValue(state.UnitName)
	plan.GeneratedUnit = types.StringValue(state.GeneratedUnit)
	plan.ActiveState = types.StringValue(state.Status.ActiveState)
}

// Create a new resource.
func (r *quadletUnitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan quadletUnitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.applyPlan(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Update an existing resource.
func (r *quadletUnitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan quadletUnitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.applyPlan(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *quadletUnitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state quadletUnitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s, err := scope.Parse(state.Scope.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("scope"), "Invalid Scope", err.Error())
		return
	}
	filename := quadlet.Filename(state.Name.ValueString(), state.Type.ValueString())

	unitState, found, err := r.engine.Read(ctx, s, filename, state.UnitName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Quadlet Unit", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(fmt.Sprintf("%s:%s", s, filename))
	state.Content = types.StringValue(string(unitState.Content))
	state.Path = types.StringValue(unitState.Path)
	state.UnitName = types.StringValue(unitState.UnitName)
	state.GeneratedUnit = types.StringValue(unitState.GeneratedUnit)
	state.ActiveState = types.StringValue(unitState.Status.ActiveState)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *quadletUnitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state quadletUnitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	s, err := scope.Parse(state.Scope.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("scope"), "Invalid Scope", err.Error())
		return
	}
	filename := quadlet.Filename(state.Name.ValueString(), state.Type.ValueString())

	if err := r.engine.Destroy(ctx, s, filename, state.UnitName.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to Delete Quadlet Unit", err.Error())
		return
	}
	// The framework automatically calls resp.State.RemoveResource() when
	// Delete returns without error diagnostics.
}

// ImportState imports a resource using the composite ID "<scope>:<name>.<type>".
func (r *quadletUnitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	scopeStr, filename, ok := strings.Cut(req.ID, ":")
	if !ok {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Expected \"<scope>:<name>.<type>\", got %q.", req.ID))
		return
	}
	name, unitType, ok := strings.Cut(filename, ".")
	if !ok {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Unit filename %q must carry an extension, e.g. \"web.container\".", filename))
		return
	}
	if _, err := scope.Parse(scopeStr); err != nil {
		resp.Diagnostics.AddError("Invalid Import ID", err.Error())
		return
	}
	if !quadlet.IsValidType(unitType) {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("unknown unit type %q; must be one of %v", unitType, quadlet.Types()))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("scope"), scopeStr)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), unitType)...)
}
