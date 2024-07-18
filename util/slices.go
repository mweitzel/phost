package util

func Map_tt[T any](arr []T, fn func(T) T) (out []T) {
	for _, t := range arr {
		out = append(out, fn(t))
	}
	return
}

func Map_tu[T any, U any](arr []T, fn func(T) U) (out []U) {
	for _, t := range arr {
		out = append(out, fn(t))
	}
	return
}

func Filter[T any](arr []T, fn func(T) bool) (out []T) {
	for _, t := range arr {
		if fn(t) {
			out = append(out, t)
		}
	}
	return
}
