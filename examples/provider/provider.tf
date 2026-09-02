terraform {
  required_providers {
    quadlet = {
      source = "janekzieleznicki/quadlet"
    }
  }
}

provider "quadlet" {
  # Local host by default. For a remote host:
  # host            = "ssh://deploy@nas.lan:22"
  # ssh_private_key = file("~/.ssh/id_ed25519")
  # sudo            = true
}
