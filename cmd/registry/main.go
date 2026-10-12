package main

import (
	"context"
	"errors"
	"github.com/euler-gaozl/agent-os-registry/registry"
	"github.com/euler-gaozl/agent-os-sdk/servicehost"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

func main() {
	servicehost.Main(servicehost.Options{Role: "registry", Port: "8096", Database: true, Concurrency: 16, Authorize: registry.Authorize, Readiness: registry.Readiness}, func(ctx context.Context, p *pgxpool.Pool, c servicehttp.Config) (http.Handler, func(context.Context) error, error) {
		client := servicehost.Client()
		if client.CatalogURL == "" {
			return nil, nil, errors.New("TOOLHUB_URL required; Registry cannot own the tool catalog")
		}
		store := &registry.Store{DB: p, Remote: &client, CatalogURL: client.CatalogURL}
		client.RegistryURL = ""
		s := &registry.Server{Store: store}
		return s.RegistryHandler(c), nil, nil
	})
}
