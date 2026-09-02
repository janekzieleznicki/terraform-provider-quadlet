package provider

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func testAccQuadletUnitConfig(name, image string) string {
	return fmt.Sprintf(`
resource "quadlet_unit" "test" {
  name  = %q
  type  = "container"
  scope = "user"

  content = <<-EOT
    [Unit]
    Description=Acceptance test container

    [Container]
    Image=%s
    Exec=sleep infinity

    [Install]
    WantedBy=default.target
  EOT
}
`, name, image)
}

func TestAccQuadletUnit_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccQuadletUnitConfig("tf-acc-web", "quay.io/libpod/alpine:latest"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("active_state"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("unit_name"), knownvalue.StringExact("tf-acc-web.service")),
				},
			},
			{
				Config: testAccQuadletUnitConfig("tf-acc-web", "quay.io/libpod/alpine:latest"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
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

func TestAccQuadletUnit_update(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccQuadletUnitConfig("tf-acc-update", "quay.io/libpod/alpine:latest"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("active_state"), knownvalue.StringExact("active")),
				},
			},
			{
				Config: testAccQuadletUnitConfig("tf-acc-update", "quay.io/libpod/busybox:latest"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("quadlet_unit.test", plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("active_state"), knownvalue.StringExact("active")),
				},
			},
		},
	})
}

func TestAccQuadletUnit_invalidContent(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "quadlet_unit" "test" {
  name  = "tf-acc-invalid"
  type  = "container"
  scope = "user"
  content = "[Container]\nImagee=alpine\n"
}
`,
				ExpectError: regexp.MustCompile("Invalid Quadlet Unit"),
			},
		},
	})
}

func TestAccQuadletUnit_systemScope(t *testing.T) {
	if os.Getenv("QUADLET_ACC_SYSTEM_SCOPE") == "" {
		t.Skip("QUADLET_ACC_SYSTEM_SCOPE not set; skipping system scope test (requires passwordless sudo -n)")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "quadlet" {
  sudo = true
}

resource "quadlet_unit" "test" {
  name  = "tf-acc-system"
  type  = "container"
  scope = "system"

  content = <<-EOT
    [Unit]
    Description=Acceptance test system container

    [Container]
    Image=quay.io/libpod/alpine:latest
    Exec=sleep infinity

    [Install]
    WantedBy=multi-user.target
  EOT
}
`,
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("active_state"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("unit_name"), knownvalue.StringExact("tf-acc-system.service")),
				},
			},
		},
	})
}

func TestAccQuadletUnit_ssh(t *testing.T) {
	host := os.Getenv("QUADLET_ACC_SSH_HOST")
	if host == "" {
		t.Skip("QUADLET_ACC_SSH_HOST not set; skipping SSH test")
	}

	user := os.Getenv("QUADLET_ACC_SSH_USER")
	keyPath := os.Getenv("QUADLET_ACC_SSH_KEY_PATH")

	if user == "" {
		t.Fatal("QUADLET_ACC_SSH_USER required when QUADLET_ACC_SSH_HOST is set")
	}
	if keyPath == "" {
		t.Fatal("QUADLET_ACC_SSH_KEY_PATH required when QUADLET_ACC_SSH_HOST is set")
	}

	keyContent, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read SSH key: %v", err)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "quadlet" {
  host            = "ssh://%s@%s"
  ssh_private_key = %q
}

resource "quadlet_unit" "test" {
  name  = "tf-acc-ssh"
  type  = "container"
  scope = "user"

  content = <<-EOT
    [Unit]
    Description=Acceptance test SSH container

    [Container]
    Image=quay.io/libpod/alpine:latest
    Exec=sleep infinity

    [Install]
    WantedBy=default.target
  EOT
}
`, user, host, string(keyContent)),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("active_state"), knownvalue.StringExact("active")),
					statecheck.ExpectKnownValue("quadlet_unit.test", tfjsonpath.New("unit_name"), knownvalue.StringExact("tf-acc-ssh.service")),
				},
			},
		},
	})
}
