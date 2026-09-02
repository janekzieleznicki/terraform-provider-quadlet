// Package provider implements the Terraform provider for Podman Quadlet units.
package provider

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/engine"
	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/transport"
)

// quadletProviderModel describes the provider data model.
type quadletProviderModel struct {
	Host                    types.String `tfsdk:"host"`
	SSHPrivateKey           types.String `tfsdk:"ssh_private_key"`
	SSHPrivateKeyPassphrase types.String `tfsdk:"ssh_private_key_passphrase"`
	SSHKnownHostsPath       types.String `tfsdk:"ssh_known_hosts_path"`
	SSHInsecure             types.Bool   `tfsdk:"ssh_insecure"`
	Sudo                    types.Bool   `tfsdk:"sudo"`
	QuadletBinary           types.String `tfsdk:"quadlet_binary"`
}

// QuadletProvider defines the provider implementation.
type QuadletProvider struct {
	version string
}

// New returns a new provider factory function.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &QuadletProvider{version: version}
	}
}

var _ provider.Provider = &QuadletProvider{}

// Metadata returns the provider type name and version.
func (p *QuadletProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "quadlet"
	resp.Version = p.version
}

// Schema defines the provider-level schema.
func (p *QuadletProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = providerschema.Schema{
		Description: "Manages Podman Quadlet unit files and their generated systemd services.",
		Attributes: map[string]providerschema.Attribute{
			"host": providerschema.StringAttribute{
				Optional:    true,
				Description: "Target host. Omit for the local host, or set an ssh://[user@]host[:port] URI for a remote host.",
			},
			"ssh_private_key": providerschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "PEM-encoded private key content for SSH authentication. When unset, the ssh-agent at $SSH_AUTH_SOCK is used.",
			},
			"ssh_private_key_passphrase": providerschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Passphrase for an encrypted ssh_private_key.",
			},
			"ssh_known_hosts_path": providerschema.StringAttribute{
				Optional:    true,
				Description: "Path to a known_hosts file. Defaults to $HOME/.ssh/known_hosts.",
			},
			"ssh_insecure": providerschema.BoolAttribute{
				Optional:    true,
				Description: "Skip SSH host key verification. Defaults to false.",
			},
			"sudo": providerschema.BoolAttribute{
				Optional:    true,
				Description: "Run systemctl and the quadlet generator via \"sudo -n --\". Defaults to false.",
			},
			"quadlet_binary": providerschema.StringAttribute{
				Optional:    true,
				Description: "Override path to the quadlet generator binary. Defaults to /usr/libexec/podman/quadlet.",
			},
		},
	}
}

// Configure prepares a data source and resource configuration.
func (p *QuadletProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data quadletProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var t transport.Transport
	host := data.Host.ValueString()
	if data.Host.IsNull() || host == "" {
		t = transport.NewLocal()
	} else {
		u, err := url.Parse(host)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("host"), "Invalid Host", fmt.Sprintf("host must be a valid URI: %s", err))
			return
		}
		if u.Scheme != "ssh" {
			resp.Diagnostics.AddAttributeError(path.Root("host"), "Unsupported Host Scheme", fmt.Sprintf("host must be omitted (local) or an ssh:// URI, got scheme %q", u.Scheme))
			return
		}
		user := u.User.Username()
		if user == "" {
			resp.Diagnostics.AddAttributeError(path.Root("host"), "Missing SSH User", "host must include a user, e.g. ssh://user@host")
			return
		}
		port := 22
		if portStr := u.Port(); portStr != "" {
			parsed, err := strconv.Atoi(portStr)
			if err != nil {
				resp.Diagnostics.AddAttributeError(path.Root("host"), "Invalid SSH Port", err.Error())
				return
			}
			port = parsed
		}

		sshT, err := transport.NewSSH(ctx, transport.SSHConfig{
			Host:           u.Hostname(),
			Port:           port,
			User:           user,
			PrivateKey:     []byte(data.SSHPrivateKey.ValueString()),
			Passphrase:     []byte(data.SSHPrivateKeyPassphrase.ValueString()),
			KnownHostsPath: data.SSHKnownHostsPath.ValueString(),
			Insecure:       data.SSHInsecure.ValueBool(),
		})
		if err != nil {
			resp.Diagnostics.AddError("Unable to Connect via SSH", err.Error())
			return
		}
		t = sshT
	}

	resp.ResourceData = engine.New(t, data.QuadletBinary.ValueString(), data.Sudo.ValueBool())
}

// DataSources defines the data sources implemented in the provider.
func (p *QuadletProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

// Resources defines the resources implemented in the provider.
func (p *QuadletProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewQuadletUnitResource,
	}
}
