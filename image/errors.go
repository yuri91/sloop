package image

import (
	"github.com/joomcode/errorx"
)

var (
	ImageErrors = errorx.NewNamespace("image")

	MetadataError = ImageErrors.NewType("metadata")
)
