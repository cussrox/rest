package chirouter_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/swaggest/rest"
	"github.com/swaggest/rest/chirouter"
	"github.com/swaggest/rest/nethttp"
)

type HandlerWithFoo struct {
	http.Handler
}

func (h HandlerWithFoo) Foo() {}

type HandlerWithBar struct {
	http.Handler
}

func (h HandlerWithFoo) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if _, err := rw.Write([]byte("foo")); err != nil {
		panic(err)
	}

	h.Handler.ServeHTTP(rw, r)
}

func (h HandlerWithBar) Bar() {}

func (h HandlerWithBar) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	h.Handler.ServeHTTP(rw, r)

	if _, err := rw.Write([]byte("bar")); err != nil {
		panic(err)
	}
}

func TestNewWrapper(t *testing.T) {
	r := chirouter.NewWrapper(chi.NewRouter()).With(func(handler http.Handler) http.Handler {
		return http.HandlerFunc(handler.ServeHTTP)
	})

	handlersCnt := 0
	totalCnt := 0

	mw := func(handler http.Handler) http.Handler {
		var (
			withRoute rest.HandlerWithRoute
			bar       interface{ Bar() }
			foo       interface{ Foo() }
		)

		totalCnt++

		if nethttp.HandlerAs(handler, &withRoute) {
			handlersCnt++

			assert.False(t, nethttp.HandlerAs(handler, &bar), "%s", handler)
			assert.True(t, nethttp.HandlerAs(handler, &foo), "%s", handler)
		}

		return HandlerWithBar{Handler: handler}
	}

	r.Use(mw)

	r.Group(func(r chi.Router) {
		r.Method(http.MethodPost,
			"/baz/{id}/",
			HandlerWithFoo{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				val, err := chirouter.PathToURLValues(request)
				assert.NoError(t, err)
				assert.Equal(t, url.Values{"id": []string{"123"}}, val)
			})},
		)
	})

	r.Mount("/mount",
		HandlerWithFoo{Handler: http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})},
	)

	r.Route("/deeper", func(r chi.Router) {
		r.Use(func(handler http.Handler) http.Handler {
			return HandlerWithFoo{Handler: handler}
		})

		r.Get("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Head("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Post("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Put("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Trace("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Connect("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Options("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Patch("/foo", func(writer http.ResponseWriter, request *http.Request) {})
		r.Delete("/foo", func(writer http.ResponseWriter, request *http.Request) {})

		r.MethodFunc(http.MethodGet, "/cuux", func(writer http.ResponseWriter, request *http.Request) {})

		r.Handle("/bar", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {}))
	})

	for _, u := range []string{"/baz/123/", "/deeper/foo", "/mount/abc"} {
		req, err := http.NewRequest(http.MethodPost, u, nil)
		require.NoError(t, err)

		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)

		assert.Equal(t, "foobar", rw.Body.String(), u)
	}

	assert.Equal(t, 14, handlersCnt)
	assert.Equal(t, 20, totalCnt)
}

func TestWrapper_Use_precedence(t *testing.T) {
	var log []string

	// Vanilla chi router.
	cr := chi.NewRouter()
	cr.Use(
		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				log = append(log, "cmw1 before")
				handler.ServeHTTP(writer, request)
				log = append(log, "cmw1 after")
			})
		},

		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				log = append(log, "cmw2 before")
				handler.ServeHTTP(writer, request)
				log = append(log, "cmw2 after")
			})
		},
	)

	// Wrapped chi router.
	wr := chirouter.NewWrapper(chi.NewRouter())
	wr.Use(
		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				log = append(log, "wmw1 before")
				handler.ServeHTTP(writer, request)
				log = append(log, "wmw1 after")
			})
		},

		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				log = append(log, "wmw2 before")
				handler.ServeHTTP(writer, request)
				log = append(log, "wmw2 after")
			})
		},
	)

	req, err := http.NewRequest(http.MethodGet, "/", nil)
	require.NoError(t, err)

	h := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		log = append(log, "h")
	})

	// Both routers should invoke middlewares in the same order.
	cr.Method(http.MethodGet, "/", h)
	wr.Method(http.MethodGet, "/", h)

	cr.ServeHTTP(nil, req)
	wr.ServeHTTP(nil, req)
	assert.Equal(t, []string{
		"cmw1 before", "cmw2 before", "h", "cmw2 after", "cmw1 after",
		"wmw1 before", "wmw2 before", "h", "wmw2 after", "wmw1 after",
	}, log)
}

// This test covers original behavior discrepancy between wrapper and router
// in how middlewares are applied.
// Router runs middlewares for every request that comes in before route matching,
// and so middlewares like StripSlashes can affect route matching in a useful way.
// Wrapper in contrast was creating a new handler by running middlewares during handler
// registration, and then adding prepared handler to router.
// In this case middlewares were "baked-in" the handler and so, were running only
// after route match.
// For the use case of StripSlashes that would result in not found, because middleware was
// invoked AFTER route matching, not BEFORE.

// Solution to this problem was passing middlewares to Router as is, the problem however is
// that Router does not allow unwrapping handlers (that is the purpose of Wrapper) to introspect
// or augment handlers.
func TestWrapper_Use_StripSlashes(t *testing.T) {
	var log []string

	r := chi.NewRouter()
	r.Use(
		middleware.StripSlashes,

		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				handler.ServeHTTP(writer, request)
			})
		},
	)

	// Wrapped chi router.
	wr := chirouter.NewWrapper(chi.NewRouter())
	wr.Use(
		middleware.StripSlashes,

		func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				handler.ServeHTTP(writer, request)
			})
		},
	)

	req, err := http.NewRequest(http.MethodGet, "/foo/", nil)
	require.NoError(t, err)

	rw := httptest.NewRecorder()

	h := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, err := writer.Write([]byte("OK")); err != nil {
			log = append(log, err.Error())
		}

		log = append(log, "h")
	})

	r.Method(http.MethodGet, "/foo", h)
	r.ServeHTTP(rw, req)

	assert.Equal(t, http.StatusOK, rw.Code)
	assert.Equal(t, "OK", rw.Body.String())

	rw = httptest.NewRecorder()

	wr.Method(http.MethodGet, "/foo", h)
	wr.ServeHTTP(rw, req)

	assert.Equal(t, http.StatusOK, rw.Code)
	assert.Equal(t, "OK", rw.Body.String())

	assert.Equal(t, []string{
		"h", "h",
	}, log)
}
