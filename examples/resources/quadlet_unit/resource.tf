resource "quadlet_unit" "web" {
  name  = "web"
  type  = "container"
  scope = "user"

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
