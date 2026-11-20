package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/kaufmo/terraform-provider-weblate/internal/provider"
)

//go:generate go tool tfplugindocs generate --provider-name weblate

// version is set by the release build.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "start the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/kaufmo/weblate",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
