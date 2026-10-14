package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/euler-gaozl/agent-os-registry/registry"
	"github.com/euler-gaozl/agent-os-sdk/servicehost"
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--model-management-profile" {
		_ = json.NewEncoder(os.Stdout).Encode(registry.ModelManagementProfile())
		return
	}
	servicehost.Main(servicehost.Options{Role: "registry", Port: "8096", Database: true, Concurrency: 16, Authorize: registry.Authorize, Readiness: registry.Readiness}, func(ctx context.Context, p *pgxpool.Pool, c servicehttp.Config) (http.Handler, func(context.Context) error, error) {
		if err := applyLocalDefaults(); err != nil {
			return nil, nil, err
		}
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
