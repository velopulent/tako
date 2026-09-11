//go:build fedora || rhel || rocky || almalinux

package distro

import (
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/updateproviders/dnf"
)

func NewUpdateService() *platform.UpdateService { return platform.NewUpdateService(dnf.New()) }
