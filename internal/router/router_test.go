// Tests for M6 — trie router. See EDGE_CASES.md §M6; every pinned
// decision from the package doc has a test here.
//
// Handlers are functions, and functions are not comparable in Go — so
// the tests identify a matched handler by CALLING it: mark(name) builds
// a handler that records its name, and called(m) invokes the match and
// returns that name.
package router

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

var lastCalled string

func mark(name string) Handler {
	return func(w *response.Writer, req *request.Request) {
		lastCalled = name
	}
}

func called(t *testing.T, m *Match) string {
	t.Helper()
	require.NotNil(t, m)
	lastCalled = ""
	m.Handler(&response.Writer{}, nil)
	return lastCalled
}

// mustRegister keeps the happy-path tests readable.
func mustRegister(t *testing.T, r *Router, method, pattern, name string) {
	t.Helper()
	require.NoError(t, r.Register(method, pattern, mark(name)))
}

// --- basic matching ---

func TestRouteRoot(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/", "root")

	m, err := r.Lookup("GET", "/")
	require.NoError(t, err)
	assert.Equal(t, "root", called(t, m))
	assert.Empty(t, m.Params)
}

func TestRouteStatic(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a/b/c", "abc")

	m, err := r.Lookup("GET", "/a/b/c")
	require.NoError(t, err)
	assert.Equal(t, "abc", called(t, m))
}

func TestRouteParamExtraction(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "user")

	m, err := r.Lookup("GET", "/users/42")
	require.NoError(t, err)
	assert.Equal(t, "user", called(t, m))
	assert.Equal(t, map[string]string{"id": "42"}, m.Params)
}

func TestRouteMultipleParams(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users/{uid}/posts/{pid}", "post")

	m, err := r.Lookup("GET", "/users/7/posts/99")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"uid": "7", "pid": "99"}, m.Params)
}

func TestRouteWildcardCapturesRest(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/files/*path", "files")

	m, err := r.Lookup("GET", "/files/a/b/c.txt")
	require.NoError(t, err)
	assert.Equal(t, "files", called(t, m))
	assert.Equal(t, map[string]string{"path": "a/b/c.txt"}, m.Params)
}

func TestRouteWildcardEmptyRest(t *testing.T) {
	// Pinned: the wildcard needs the slash. "/files/" matches with rest
	// "", "/files" does not match at all.
	r := New()
	mustRegister(t, r, "GET", "/files/*path", "files")

	m, err := r.Lookup("GET", "/files/")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"path": ""}, m.Params)

	_, err = r.Lookup("GET", "/files")
	require.ErrorIs(t, err, ErrNotFound)
}

// --- precedence ---

func TestPrecedenceStaticBeatsParam(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users/me", "static")
	mustRegister(t, r, "GET", "/users/{id}", "param")

	m, err := r.Lookup("GET", "/users/me")
	require.NoError(t, err)
	assert.Equal(t, "static", called(t, m))

	m, err = r.Lookup("GET", "/users/42")
	require.NoError(t, err)
	assert.Equal(t, "param", called(t, m))
	assert.Equal(t, "42", m.Params["id"])
}

func TestPrecedenceParamBeatsWildcard(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "param")
	mustRegister(t, r, "GET", "/users/*rest", "wild")

	m, err := r.Lookup("GET", "/users/42")
	require.NoError(t, err)
	assert.Equal(t, "param", called(t, m))

	// Two segments: the param route is one level deep only, so the
	// wildcard wins here.
	m, err = r.Lookup("GET", "/users/42/posts")
	require.NoError(t, err)
	assert.Equal(t, "wild", called(t, m))
	assert.Equal(t, "42/posts", m.Params["rest"])
}

func TestBacktracking(t *testing.T) {
	// The classic trie-router bug: the static "b" node exists but
	// dead-ends at "d"; the walk must back out and retry via :x.
	r := New()
	mustRegister(t, r, "GET", "/a/{x}/c", "param-route")
	mustRegister(t, r, "GET", "/a/b/d", "static-route")

	m, err := r.Lookup("GET", "/a/b/c")
	require.NoError(t, err)
	assert.Equal(t, "param-route", called(t, m))
	assert.Equal(t, "b", m.Params["x"])
}

// --- the method dimension ---

func TestMethodsAreSeparate(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "get-a")
	mustRegister(t, r, "POST", "/a", "post-a")

	m, err := r.Lookup("GET", "/a")
	require.NoError(t, err)
	assert.Equal(t, "get-a", called(t, m))

	m, err = r.Lookup("POST", "/a")
	require.NoError(t, err)
	assert.Equal(t, "post-a", called(t, m))
}

func TestMethodNotAllowedVsNotFound(t *testing.T) {
	// Two different misses, two different errors (405 vs 404).
	r := New()
	mustRegister(t, r, "GET", "/a", "get-a")

	_, err := r.Lookup("DELETE", "/a")
	require.ErrorIs(t, err, ErrMethodNotAllowed)

	_, err = r.Lookup("GET", "/nope")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAllowedListsMethods(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "g")
	mustRegister(t, r, "POST", "/a", "p")
	mustRegister(t, r, "PUT", "/b", "u")

	assert.Equal(t, []string{"GET", "POST"}, r.Allowed("/a"))
	assert.Equal(t, []string{"PUT"}, r.Allowed("/b"))
	assert.Empty(t, r.Allowed("/nope"))
}

// --- registration validation ---

func TestRegisterDuplicateRoute(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "first")
	require.ErrorIs(t, r.Register("GET", "/a", mark("second")), ErrDuplicateRoute)
}

func TestRegisterParamNameConflict(t *testing.T) {
	// "/users/:id" then "/users/:name": same position, two names.
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "a")
	require.ErrorIs(t, r.Register("GET", "/users/{name}/posts", mark("b")), ErrParamConflict)
}

func TestRegisterSameParamNameIsFine(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "one")
	mustRegister(t, r, "GET", "/users/{id}/posts", "two")

	m, err := r.Lookup("GET", "/users/5/posts")
	require.NoError(t, err)
	assert.Equal(t, "two", called(t, m))
}

func TestRegisterWildcardNotLast(t *testing.T) {
	r := New()
	require.ErrorIs(t, r.Register("GET", "/a/*x/b", mark("h")), ErrWildcardNotLast)
}

func TestRegisterInvalidPatterns(t *testing.T) {
	r := New()
	assert.ErrorIs(t, r.Register("GET", "", mark("h")), ErrInvalidPattern, "empty")
	assert.ErrorIs(t, r.Register("GET", "a/b", mark("h")), ErrInvalidPattern, "no leading slash")
	assert.ErrorIs(t, r.Register("GET", "/a//b", mark("h")), ErrInvalidPattern, "empty segment")
	assert.ErrorIs(t, r.Register("GET", "/a/", mark("h")), ErrInvalidPattern, "trailing slash")
	assert.ErrorIs(t, r.Register("GET", "/a/{}", mark("h")), ErrInvalidPattern, "nameless param")
	assert.ErrorIs(t, r.Register("GET", "/a/*", mark("h")), ErrInvalidPattern, "nameless wildcard")
	assert.ErrorIs(t, r.Register("", "/a", mark("h")), ErrInvalidMethod, "empty method")
}

// --- path edge cases (requests, not patterns) ---

func TestLookupTrailingSlashIsDistinct(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "a")

	_, err := r.Lookup("GET", "/a/")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLookupDoubledSlash(t *testing.T) {
	// Empty segments are kept, never normalized — and a param never
	// matches an empty segment.
	r := New()
	mustRegister(t, r, "GET", "/a/b", "ab")
	mustRegister(t, r, "GET", "/users/{id}/posts", "posts")

	_, err := r.Lookup("GET", "/a//b")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = r.Lookup("GET", "/users//posts")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLookupCaseSensitive(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/users", "u")

	_, err := r.Lookup("GET", "/Users")
	require.NoError(t, err)
}

func TestLookupLiteralColonInRequest(t *testing.T) {
	// ':' is only special at registration. As request DATA it is an
	// ordinary character and lands in the param like anything else.
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "u")

	m, err := r.Lookup("GET", "/users/{id}")
	require.NoError(t, err)
	assert.Equal(t, "{id}", m.Params["id"])
}

func TestLookupPathWithoutLeadingSlash(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "a")

	// Includes the asterisk-form target "*": not the router's problem.
	_, err := r.Lookup("GET", "*")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLookupLongerAndShorterPaths(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a/b", "ab")

	_, err := r.Lookup("GET", "/a")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = r.Lookup("GET", "/a/b/c")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRootWildcardFallback(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/*rest", "fallback")
	mustRegister(t, r, "GET", "/api/users", "users")

	m, err := r.Lookup("GET", "/api/users")
	require.NoError(t, err)
	assert.Equal(t, "users", called(t, m))

	m, err = r.Lookup("GET", "/anything/else")
	require.NoError(t, err)
	assert.Equal(t, "fallback", called(t, m))
	assert.Equal(t, "anything/else", m.Params["rest"])
}

// --- concurrency ---

func TestConcurrentLookups(t *testing.T) {
	// Register-then-serve: lookups are read-only and must be safe in
	// parallel. Run with -race. (No called() here — lastCalled is a
	// plain global and this test only exercises Lookup itself.)
	r := New()
	mustRegister(t, r, "GET", "/users/{id}", "u")
	mustRegister(t, r, "GET", "/files/*path", "f")

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				m, err := r.Lookup("GET", "/users/42")
				if err != nil || m.Params["id"] != "42" {
					t.Error("concurrent lookup returned wrong result")
					return
				}
			}
		}()
	}
	wg.Wait()
}

// --- dispatch (routeHTTP/ServeHTTP) ---

func newReq(method, target string) *request.Request {
	req := request.NewRequest()
	req.RequestLine = request.RequestLine{
		Method:        method,
		RequestTarget: target,
		HTTPVersion:   "1.1",
	}
	return req
}

func TestRouteHTTPSetsPathValues(t *testing.T) {
	r := New()
	var val string
	var ok bool
	r.Get("/users/{id}", func(w *response.Writer, req *request.Request) {
		val, ok = req.PathValue("id")
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/users/42"))

	assert.True(t, ok)
	assert.Equal(t, "42", val)
}

func TestRouteHTTPSetsWildcardPathValue(t *testing.T) {
	r := New()
	var val string
	var ok bool
	r.Get("/files/*path", func(w *response.Writer, req *request.Request) {
		val, ok = req.PathValue("path")
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/files/a/b/c.txt"))

	assert.True(t, ok)
	assert.Equal(t, "a/b/c.txt", val)
}

func TestRouteHTTPRouteWithoutParams(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "a")
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/a"))

	assert.Equal(t, "a", lastCalled)
}

func TestRouteHTTPDefaultNotFound(t *testing.T) {
	r := New()
	r.Get("/a", mark("a"))
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/nope"))

	assert.True(t, strings.HasPrefix(buf.String(), "HTTP/1.1 404 Not Found\r\n"),
		"unmatched path must get the default 404, got:\n%q", buf.String())
}

func TestRouteHTTPCustomNotFound(t *testing.T) {
	r := New()
	r.Get("/a", mark("a"))
	var called bool
	r.NotFound(func(w *response.Writer, req *request.Request) { called = true })
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/nope"))

	assert.True(t, called, "custom NotFound endpoint must run")
}

func TestRouteHTTPDefaultMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/a", mark("a"))
	r.Post("/a", mark("p"))
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("DELETE", "/a"))

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "HTTP/1.1 405 Method Not Allowed\r\n"),
		"wrong method must get a 405, got:\n%q", out)
	assert.Contains(t, out, "allow:GET, POST\r\n")
}

func TestRouteHTTPCustomMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/a", mark("a"))
	var called bool
	r.MethodNotAllowed(func(w *response.Writer, req *request.Request) { called = true })
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("DELETE", "/a"))

	assert.True(t, called, "custom MethodNotAllowed endpoint must run")
}

// --- middleware ---

func TestMiddlewareOrder(t *testing.T) {
	var order []string
	first := func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			order = append(order, "first")
			next(w, req)
		}
	}
	second := func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			order = append(order, "second")
			next(w, req)
		}
	}

	r := New()
	r.Use(first).Use(second)
	r.Get("/", func(w *response.Writer, req *request.Request) {
		order = append(order, "handler")
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/"))

	assert.Equal(t, []string{"first", "second", "handler"}, order)
}

func TestMiddlewareShortCircuits(t *testing.T) {
	reject := func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			w.Error("nope", response.StatusBadRequest, "text/plain")
		}
	}

	r := New()
	r.Use(reject)
	r.Get("/", mark("handler"))
	r.Build()

	var buf bytes.Buffer
	lastCalled = ""
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/"))

	assert.Equal(t, "", lastCalled, "route handler must not run after a rejection")
	assert.True(t, strings.HasPrefix(buf.String(), "HTTP/1.1 400 Bad Request\r\n"))
}

func TestMiddlewareAddsResponseHeader(t *testing.T) {
	add := func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			w.Header().Set("X-Trace", "mw")
			next(w, req)
		}
	}

	r := New()
	r.Use(add)
	r.Get("/", func(w *response.Writer, req *request.Request) {
		_, _ = w.WriteBody([]byte("ok"))
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/"))

	assert.Contains(t, buf.String(), "x-trace:mw\r\n")
}

func TestMiddlewareDoubleWriteGuard(t *testing.T) {
	guard := func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			next(w, req)
			w.Error("late", response.StatusInternalServerError, "text/plain")
		}
	}

	r := New()
	r.Use(guard)
	r.Get("/", func(w *response.Writer, req *request.Request) {
		_, _ = w.WriteBody([]byte("hello"))
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/"))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "HTTP/1.1 "),
		"a response after the handler ran must not produce a second status line, got:\n%q", out)
	assert.True(t, strings.HasSuffix(out, "hello"))
}

func TestUseAfterBuildRecordsError(t *testing.T) {
	r := New()
	r.Get("/a", mark("a"))
	r.Build()

	r.Use(func(next Handler) Handler { return next })

	assert.NotEmpty(t, r.Err())
}

func TestNotFoundAfterBuildRecordsError(t *testing.T) {
	r := New()
	r.Build()

	r.NotFound(func(w *response.Writer, req *request.Request) {})

	assert.NotEmpty(t, r.Err())
}

func TestMethodNotAllowedAfterBuildRecordsError(t *testing.T) {
	r := New()
	r.Build()

	r.MethodNotAllowed(func(w *response.Writer, req *request.Request) {})

	assert.NotEmpty(t, r.Err())
}

func TestApplyMiddlewaresWrapsHandler(t *testing.T) {
	var order []string
	r := New()
	r.Use(func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			order = append(order, "mw")
			next(w, req)
		}
	})

	h := r.ApplyMiddlewares(func(w *response.Writer, req *request.Request) {
		order = append(order, "inner")
	})
	h(&response.Writer{}, nil)

	assert.Equal(t, []string{"mw", "inner"}, order)
}

func TestMiddlewareRunsAroundNotFound(t *testing.T) {
	var mwRan, nfRan bool
	r := New()
	r.Use(func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			mwRan = true
			next(w, req)
		}
	})
	r.Get("/a", mark("a"))
	r.NotFound(func(w *response.Writer, req *request.Request) { nfRan = true })
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/nope"))

	assert.True(t, mwRan)
	assert.True(t, nfRan)
}

func TestMiddlewareRunsAroundMethodNotAllowed(t *testing.T) {
	var mwRan, mnaRan bool
	r := New()
	r.Use(func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			mwRan = true
			next(w, req)
		}
	})
	r.Get("/a", mark("a"))
	r.MethodNotAllowed(func(w *response.Writer, req *request.Request) { mnaRan = true })
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("DELETE", "/a"))

	assert.True(t, mwRan)
	assert.True(t, mnaRan)
}

func TestMiddlewareRunsBeforePathValues(t *testing.T) {
	var mwSawParam, handlerSawParam bool
	r := New()
	r.Use(func(next Handler) Handler {
		return func(w *response.Writer, req *request.Request) {
			_, mwSawParam = req.PathValue("id")
			next(w, req)
		}
	})
	r.Get("/users/{id}", func(w *response.Writer, req *request.Request) {
		_, handlerSawParam = req.PathValue("id")
	})
	r.Build()

	var buf bytes.Buffer
	r.ServeHTTP(response.NewWriter(&buf), newReq("GET", "/users/42"))

	assert.False(t, mwSawParam)
	assert.True(t, handlerSawParam)
}

func TestRouteErrorMessage(t *testing.T) {
	err := RouteError{
		Err:     ErrWildcardNotLast,
		Method:  "GET",
		Pattern: "/files/*path/edit",
	}
	assert.Equal(t, "router: GET /files/*path/edit: wildcard segment must be last", err.Error())
}

func TestRouteErrorUnwrap(t *testing.T) {
	err := RouteError{
		Err:     ErrDuplicateRoute,
		Method:  "GET",
		Pattern: "/a",
	}
	assert.ErrorIs(t, err, ErrDuplicateRoute)
}

func TestRegisterWildcardNotLastPositions(t *testing.T) {
	r := New()
	err := r.Register("GET", "/files/*path/edit", mark("h"))
	require.ErrorIs(t, err, ErrWildcardNotLast)

	var re RouteError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "GET", re.Method)
	assert.Equal(t, "/files/*path/edit", re.Pattern)
	assert.Equal(t, 7, re.Start)
	assert.Equal(t, 12, re.End)
	assert.NotEmpty(t, re.File)
	assert.True(t, re.Line > 0)
}

func TestRegisterNamelessParamPositions(t *testing.T) {
	r := New()
	err := r.Register("GET", "/a/{}", mark("h"))
	require.ErrorIs(t, err, ErrInvalidPattern)

	var re RouteError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 3, re.Start)
	assert.Equal(t, 5, re.End)
	assert.NotEmpty(t, re.Help)
}

func TestRegisterTrailingSlashRouteError(t *testing.T) {
	r := New()
	err := r.Register("GET", "/a/", mark("h"))
	require.ErrorIs(t, err, ErrInvalidPattern)

	var re RouteError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, 0, re.Start)
	assert.Equal(t, len("/a/"), re.End)
	assert.NotEmpty(t, re.Help)
}

func TestLookupInvalidPathReturnsRouteError(t *testing.T) {
	r := New()
	mustRegister(t, r, "GET", "/a", "a")

	_, err := r.Lookup("GET", "no-slash")
	require.ErrorIs(t, err, ErrNotFound)

	var re RouteError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "GET", re.Method)
	assert.Equal(t, "no-slash", re.Pattern)
}

func TestGetHelperRecordsRouteErrorInErr(t *testing.T) {
	r := New()
	r.Get("/a//b", mark("h"))

	errs := r.Err()
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], ErrInvalidPattern)

	var re RouteError
	require.ErrorAs(t, errs[0], &re)
	assert.Equal(t, "GET", re.Method)
	assert.Equal(t, "/a//b", re.Pattern)
}
