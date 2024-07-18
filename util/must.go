package util

func Must[T any](t T, e error) T {
	if e != nil {
		panic(e)
	}
	return t
}
