//go:build opensuse

package distro

import (
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/updateproviders/zypper"
)

func NewUpdateService() *platform.UpdateService { return platform.NewUpdateService(zypper.New()) }
