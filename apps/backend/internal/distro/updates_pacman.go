//go:build archlinux

package distro

import (
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/updateproviders/pacman"
)

func NewUpdateService() *platform.UpdateService { return platform.NewUpdateService(pacman.New()) }
