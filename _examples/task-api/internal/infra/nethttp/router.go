package nethttp

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/swaggest/openapi-go/openapi3"
	"github.com/swaggest/rest"
	"github.com/swaggest/rest/_examples/task-api/internal/infra/schema"
	"github.com/swaggest/rest/_examples/task-api/internal/infra/service"
	"github.com/swaggest/rest/_examples/task-api/internal/usecase"
	"github.com/swaggest/rest/nethttp"
	"github.com/swaggest/rest/web"
	swgui "github.com/swaggest/swgui/v4emb"
)

// NewRouter creates HTTP router.
func NewRouter(locator *service.Locator) http.Handler {
	s := web.DefaultService()

	schema.SetupOpenAPICollector(s.OpenAPICollector)

	adminAuth := middleware.BasicAuth("Admin Access", map[string]string{"admin": "admin"})
	userAuth := middleware.BasicAuth("User Access", map[string]string{"user": "user"})

	s.Wrap(
		middleware.NoCache,
		middleware.Timeout(time.Second),
	)

	ff := func(h *nethttp.Handler) {
		h.ReqMapping = rest.RequestMapping{rest.ParamInPath: map[string]string{"ID": "id"}}
	}

	// Unrestricted access.
	s.Route("/dev", func(r chi.Router) {
		r.Use(nethttp.AnnotateOpenAPI(s.OpenAPICollector, func(op *openapi3.Operation) error {
			op.Tags = []string{"Dev Mode"}

			return nil
		}))
		r.Group(func(r chi.Router) {
			r.Method(http.MethodPost, "/tasks", nethttp.NewHandler(usecase.CreateTask(locator),
				nethttp.SuccessStatus(http.StatusCreated)))
			r.Method(http.MethodPut, "/tasks/{id}", nethttp.NewHandler(usecase.UpdateTask(locator), ff))
			r.Method(http.MethodGet, "/tasks/{id}", nethttp.NewHandler(usecase.FindTask(locator), ff))
			r.Method(http.MethodGet, "/tasks", nethttp.NewHandler(usecase.FindTasks(locator)))
			r.Method(http.MethodDelete, "/tasks/{id}", nethttp.NewHandler(usecase.FinishTask(locator), ff))
		})
	})

	// Endpoints with admin access.
	s.Route("/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(nethttp.AnnotateOpenAPI(s.OpenAPICollector, func(op *openapi3.Operation) error {
				op.Tags = []string{"Admin Mode"}

				return nil
			}))
			r.Use(adminAuth, nethttp.HTTPBasicSecurityMiddleware(s.OpenAPICollector, "Admin", "Admin access"))
			r.Method(http.MethodPut, "/tasks/{id}", nethttp.NewHandler(usecase.UpdateTask(locator), ff))
		})
	})

	// Endpoints with user access.
	s.Route("/user", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(userAuth, nethttp.HTTPBasicSecurityMiddleware(s.OpenAPICollector, "User", "User access"))
			r.Method(http.MethodPost, "/tasks", nethttp.NewHandler(usecase.CreateTask(locator),
				nethttp.SuccessStatus(http.StatusCreated)))
		})
	})

	// Swagger UI endpoint at /docs.
	s.Docs("/docs", swgui.New)

	s.Mount("/debug", middleware.Profiler())

	return s
}
