package gofix

type Shape interface {
	Area() float64
}

type Circle struct{ R float64 }

func (c Circle) Area() float64 { return 3.14 * c.R * c.R }

// Rect uses a pointer receiver, so only *Rect satisfies Shape.
type Rect struct{ W, H float64 }

func (r *Rect) Area() float64 { return r.W * r.H }
