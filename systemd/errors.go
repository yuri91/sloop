package systemd

import (
	"github.com/joomcode/errorx"
)

var (
	SystemdErrors = errorx.NewNamespace("systemd")

	CreateVolumeError = SystemdErrors.NewType("create_volume")
	CreateImageError = SystemdErrors.NewType("create_image")
	CreateServiceError = SystemdErrors.NewType("create_service")

	RemoveImageError = SystemdErrors.NewType("remove_image")
	RemoveUnitError = SystemdErrors.NewType("remove_unit")

	RuntimeServiceError = SystemdErrors.NewType("runtime_service")

	FilesystemError = SystemdErrors.NewType("filesystem")
)
