package provider

import (
	"os/exec"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"quadlet": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck fails fast with an actionable message when the local
// podman + quadlet generator stack the acceptance suite exercises is absent,
// instead of letting every step fail individually with an opaque error.
func testAccPreCheck(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Fatalf("acceptance tests require podman on PATH: %v", err)
	}
}
