package router

import (
	"errors"
	"sort"
	"strings"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
)

type Handler func(w response.Writer, req *request.Request)

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
	trees    map[string]*node
	newIndex func() segmentIndex
}

func New() *Router {
	return &Router{
		trees:    map[string]*node{},
		newIndex: func() segmentIndex { return mapIndex{} },
	}
}

func NewHashed() *Router {
	return &Router{
		trees:    map[string]*node{},
		newIndex: func() segmentIndex { return &hashIndex{} },
	}
}

func (r *Router) newNode() *node {
	return &node{static: r.newIndex()}
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

		case seg[0] == ':':
			name := seg[1:]
			if name == "" {
				return ErrInvalidPattern
			}
			if n.paramChild == nil {
				n.paramChild = r.newNode()
				n.paramName = name
			} else if n.paramName != name {
				// "/users/:id" then "/users/:name" — same position,
				// two names. One of them is a bug; refuse.
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
