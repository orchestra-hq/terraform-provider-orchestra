package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/orchestra-hq/terraform-provider-orchestra/internal/provider"
)

// Set by GoReleaser with -ldflags "-X main.version=…", which only reaches package-level variables.
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.StringVar(&version, "version", version, "provider version")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/orchestra-hq/orchestra",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
