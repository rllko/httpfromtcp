package router

import "slices"

type Middleware func(next Handler) Handler

func (r *Router) Use(m Middleware) *Router {
	r.middlewares = append(r.middlewares, m)
	return r
}

func (r *Router) ApplyMiddlewares(h Handler) Handler {
	handler := h
	for _, hh := range slices.Backward(r.middlewares) {
		handler = hh(handler)
	}

	return handler
}
