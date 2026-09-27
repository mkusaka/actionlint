//go:build gofuzz

package yactionlint_fuzz

import (
	"github.com/mkusaka/yactionlint"
)

func FuzzGlobGitRef(data []byte) int {
	errs := actionlint.ValidateRefGlob(string(data))
	if len(errs) > 0 {
		return 0
	}
	return 1
}

func FuzzGlobFilePath(data []byte) int {
	errs := actionlint.ValidatePathGlob(string(data))
	if len(errs) > 0 {
		return 0
	}
	return 1
}
