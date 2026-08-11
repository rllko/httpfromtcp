package router

import (
	"errors"
	"slices"
)

type Middleware func(next Handler) Handler

func (r *Router) Use(m Middleware) *Router {
	if r.built {
		r.Errors = append(r.Errors, errors.New("middlewares can only be added before the router is built"))
	}
	r.middlewares = append(r.middlewares, m)
	return r
}

func (r *Router) ApplyMiddlewares(h Handler) Handler {
	handler := h
	for _, hh := range slices.Backward(r.middlewares) {
		handler = hh(handler)
	}

	r.built = true
	return handler
}
