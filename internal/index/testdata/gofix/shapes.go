package gofix

import "net/http"

// Register wires an HTTP route -> should be detected as a Route.
func Register(mux *http.ServeMux) { mux.HandleFunc("/health", health) }

func health(w http.ResponseWriter, r *http.Request) {}

type Shape interface {
	Area() float64
}

type Circle struct{ R float64 }

// NewCircle is a constructor -> should produce a `constructs` edge to Circle.
func NewCircle(r float64) *Circle { return &Circle{R: r} }

func (c Circle) Area() float64 { return 3.14 * c.R * c.R }

// Rect uses a pointer receiver, so only *Rect satisfies Shape.
type Rect struct{ W, H float64 }

func (r *Rect) Area() float64 { return r.W * r.H }
