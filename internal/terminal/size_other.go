//go:build !unix

package terminal

func cellRatio(_ uintptr, _, _ int) float64 { return .5 }
