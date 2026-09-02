// Package main defines the terraform-provider-quadlet provider binary.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/janekzieleznicki/terraform-provider-quadlet/internal/provider"
)

// version is overwritten at release build time via -ldflags "-X main.version=x.y.z".
// Left at its default here; wiring the release ldflags is a packaging concern outside
// milestone 1.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/janekzieleznicki/quadlet",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
