package util

func Abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
