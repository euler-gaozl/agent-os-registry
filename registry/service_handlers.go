package registry

import (
	"github.com/euler-gaozl/agent-os-sdk/servicehttp"
	"net/http"
)

func (s *Server) RegistryHandler(c servicehttp.Config) http.Handler {
	return servicehttp.New(c, s.Store.DB.Ping, func(m *http.ServeMux) { s.registryRoutes(m); s.publishedRoutes(m) })
}
