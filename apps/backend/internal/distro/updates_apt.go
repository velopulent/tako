//go:build debian || ubuntu

package distro

import (
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/updateproviders/apt"
)

func NewUpdateService() *platform.UpdateService { return platform.NewUpdateService(apt.New()) }
