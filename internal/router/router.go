// Package router
package router

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
	"httpfromtcp/internal/server"
)

const NotFoundMessage = `
	<html>
	  <head>
	    <title>404 Not Found</title>
	  </head>
	  <body>
	    <h1>Not Found</h1>
	    <p>The requested URL was not found on this server.</p>
	  </body>
	</html>
	`

const AllowedMessage = `
	<html>
	  <head>
	    <title>405 Method Not Allowed</title>
	  </head>
	  <body>
	    <h1>Method Not Allowed</h1>
	    <p>The requested method is not allowed on this server.</p>
	  </body>
	</html>
	`

type Handler func(w *response.Writer, req *request.Request)

var (
	ErrNotFound         = errors.New("no route matches the path")
	ErrMethodNotAllowed = errors.New("path exists, method does not")

	ErrInvalidMethod   = errors.New("invalid method")
	ErrInvalidPattern  = errors.New("invalid pattern")
	ErrDuplicateRoute  = errors.New("route already registered")
	ErrParamConflict   = errors.New("conflicting param name at the same level")
	ErrWildcardNotLast = errors.New("wildcard segment must be last")
)

type Match struct {
	Handler Handler
	Params  map[string]string
}

type node struct {
	static segmentIndex

	paramChild *node
	paramName  string

	wildcardName string
	wildcardH    Handler

	handler Handler
}

type Router struct {
	trees                    map[string]*node
	newIndex                 func() segmentIndex
	Errors                   []error
	notFoundEndpoint         Handler
	methodNotAllowedEndpoint Handler

	middlewares    []Middleware
	built          bool
	finalMwHandler Handler
}

func (r *Router) Build() *Router {
	r.finalMwHandler = r.ApplyMiddlewares(r.routeHTTP)
	return r
}

func New() *Router {
	return &Router{
		trees:    map[string]*node{},
		newIndex: func() segmentIndex { return mapIndex{} },
		Errors:   []error{},
	}
}

func NewHashed() *Router {
	return &Router{
		trees:    map[string]*node{},
		newIndex: func() segmentIndex { return &hashIndex{} },
		Errors:   []error{},
	}
}

func (r *Router) newNode() *node {
	return &node{static: r.newIndex()}
}

// Err returns whether errors were encountered
func (r *Router) Err() error {
	return errors.Join(r.Errors...)
}

func splitPath(p string) ([]string, bool) {
	if p == "" || p[0] != '/' {
		return nil, false
	}
	if p == "/" {
		return nil, true
	}
	return strings.Split(p[1:], "/"), true
}

func (r *Router) Register(method, pattern string, h Handler) error {
	if method == "" {
		return ErrInvalidMethod
	}

	segs, ok := splitPath(pattern)
	if !ok {
		return ErrInvalidPattern
	}

	root := r.trees[method]
	if root == nil {
		root = r.newNode()
		r.trees[method] = root
	}

	n := root
	for i, seg := range segs {
		switch {
		case seg == "":
			// Covers both "//" inside and a trailing slash — patterns
			// must be canonical.
			return ErrInvalidPattern

		case seg[0] == '{':
			if len(seg) < 3 || seg[len(seg)-1] != '}' {
				return ErrInvalidPattern
			}
			name := seg[1 : len(seg)-1]
			if name == "" {
				return ErrInvalidPattern
			}

			if n.paramChild == nil {
				n.paramChild = r.newNode()
				n.paramName = name
			} else if n.paramName != name {
				return ErrParamConflict
			}
			n = n.paramChild

		case seg[0] == '*':
			name := seg[1:]
			if name == "" {
				return ErrInvalidPattern
			}
			if i != len(segs)-1 {
				return ErrWildcardNotLast
			}
			if n.wildcardH != nil {
				return ErrDuplicateRoute
			}
			n.wildcardName = name
			n.wildcardH = h
			return nil

		default:
			child, found := n.static.get(seg)
			if !found {
				child = r.newNode()
				n.static.set(seg, child)
			}
			n = child
		}
	}

	if n.handler != nil {
		return ErrDuplicateRoute
	}
	n.handler = h
	return nil
}

func lookupNode(n *node, segs []string) (Handler, map[string]string, bool) {
	if len(segs) == 0 {
		if n.handler != nil {
			return n.handler, nil, true
		}
		return nil, nil, false
	}

	seg := segs[0]

	if child, found := n.static.get(seg); found {
		if h, params, matched := lookupNode(child, segs[1:]); matched {
			return h, params, true
		}
	}

	if n.paramChild != nil && seg != "" {
		if h, params, matched := lookupNode(n.paramChild, segs[1:]); matched {
			if params == nil {
				params = map[string]string{}
			}
			params[n.paramName] = seg
			return h, params, true
		}
	}

	if n.wildcardH != nil {
		return n.wildcardH, map[string]string{
			n.wildcardName: strings.Join(segs, "/"),
		}, true
	}

	return nil, nil, false
}

func (r *Router) Lookup(method, path string) (*Match, error) {
	segs, ok := splitPath(path)
	if !ok {
		return nil, ErrNotFound
	}

	if root := r.trees[method]; root != nil {
		if h, params, matched := lookupNode(root, segs); matched {
			return &Match{Handler: h, Params: params}, nil
		}
	}

	for m, root := range r.trees {
		if m == method {
			continue
		}
		if _, _, matched := lookupNode(root, segs); matched {
			return nil, ErrMethodNotAllowed
		}
	}

	return nil, ErrNotFound
}

func (r *Router) Allowed(path string) []string {
	segs, ok := splitPath(path)
	if !ok {
		return nil
	}

	var methods []string
	for m, root := range r.trees {
		if _, _, matched := lookupNode(root, segs); matched {
			methods = append(methods, m)
		}
	}
	sort.Strings(methods)
	return methods
}

func (r *Router) Get(pattern string, h Handler) *Router {
	err := r.Register("GET", strings.ToLower(pattern), h)
	if err != nil {
		r.Errors = append(r.Errors, err)
	}

	return r
}

func (r *Router) Post(pattern string, h Handler) *Router {
	err := r.Register("POST", strings.ToLower(pattern), h)
	if err != nil {
		r.Errors = append(r.Errors, err)
	}

	return r
}

func (r *Router) Delete(pattern string, h Handler) *Router {
	err := r.Register("DELETE", strings.ToLower(pattern), h)
	if err != nil {
		r.Errors = append(r.Errors, err)
	}

	return r
}

func (r *Router) Put(pattern string, h Handler) *Router {
	err := r.Register("PUT", strings.ToLower(pattern), h)
	if err != nil {
		r.Errors = append(r.Errors, err)
	}

	return r
}

func (r *Router) Patch(pattern string, h Handler) *Router {
	err := r.Register("PATCH", strings.ToLower(pattern), h)
	if err != nil {
		r.Errors = append(r.Errors, err)
	}
	return r
}

func (r *Router) NotFound(h server.HandlerFunc) {
	if r.built {
		r.Errors = append(r.Errors, errors.New("notfound can only be set before the router is built"))
	}

	r.notFoundEndpoint = Handler(h)
}

func (r *Router) MethodNotAllowed(h server.HandlerFunc) {
	if r.built {
		r.Errors = append(r.Errors, errors.New("method not allowed can only be set before the router is built"))
	}

	r.methodNotAllowedEndpoint = Handler(h)
}

func (r *Router) routeHTTP(w *response.Writer, req *request.Request) {
	match, err := r.Lookup(req.RequestLine.Method, req.RequestLine.RequestTarget)
	if err == nil {
		// in this case not found and MethodNotAllowed dont use PathValues
		// this might create problems in the future, good for now
		for k, v := range match.Params {
			req.SetPathValue(k, v)
		}

		match.Handler(w, req)
		return
	}

	if errors.Is(err, ErrNotFound) {
		if r.notFoundEndpoint != nil {
			r.notFoundEndpoint(w, req)
			return
		}
		w.Error(NotFoundMessage, response.StatusNotFound, "text/html")
		return
	}

	if errors.Is(err, ErrMethodNotAllowed) {
		w.Header().Set("Allow", strings.Join(r.Allowed(req.RequestLine.RequestTarget), ", "))
		if r.methodNotAllowedEndpoint != nil {
			r.methodNotAllowedEndpoint(w, req)
			return
		}

		err = w.WriteStatusLine(405)
		if err != nil {
			w.Error(AllowedMessage, response.StatusMethodNotAllowed, "text/html")
			return
		}

		w.Header().Replace("content-length", strconv.Itoa(len(AllowedMessage)))
		w.Header().Replace("content-type", "text/html")
		_ = w.WriteHeaders()

		_, _ = w.WriteBody([]byte(AllowedMessage))
		return
	}

	w.Error(err.Error(), response.StatusInternalServerError, "text/plain")
}

func (r *Router) ServeHTTP(w *response.Writer, req *request.Request) {
	r.finalMwHandler(w, req)
}
