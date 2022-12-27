package cue

import (
	"github.com/joomcode/errorx"
)

var (
	CueErrors = errorx.NewNamespace("cue")

	LoadError = CueErrors.NewType("load")
	BuildError = CueErrors.NewType("build")
	ConstraintError = CueErrors.NewType("constrain")
	ConvertError = CueErrors.NewType("convert")
	ValidateError = CueErrors.NewType("validate")
	DecodeError = CueErrors.NewType("decode")
)
