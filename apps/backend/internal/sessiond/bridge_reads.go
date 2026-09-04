package sessiond

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
)

func (process *userBridgeProcess) readServices(ctx context.Context, operation auth.ServiceReadOperation) ([]platform.Unit, error) {
	var result []platform.Unit
	err := process.decode(ctx, "services-read", "services.read", operation, &result)
	return result, err
}

func (process *userBridgeProcess) readUnitDetails(ctx context.Context, operation auth.ServiceReadOperation) (platform.UnitDetail, error) {
	var result platform.UnitDetail
	err := process.decode(ctx, "services-detail", "services.detail", operation, &result)
	return result, err
}

func (process *userBridgeProcess) readUnitConfiguration(ctx context.Context, operation auth.ServiceReadOperation) (platform.UnitConfiguration, error) {
	var result platform.UnitConfiguration
	err := process.decode(ctx, "services-configuration", "services.configuration", operation, &result)
	return result, err
}

func (process *userBridgeProcess) serviceAction(ctx context.Context, operation platform.ServiceOperation) error {
	_, err := process.call(ctx, "services-action", "services.action", operation)
	return err
}

func (process *userBridgeProcess) readHostInfo(ctx context.Context) (host.Info, error) {
	var result host.Info
	err := process.decode(ctx, "host-info", "host.info", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readHostConfiguration(ctx context.Context) (platform.HostConfiguration, error) {
	var result platform.HostConfiguration
	err := process.decode(ctx, "host-configuration", "host.configuration.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readPowerStatus(ctx context.Context) (platform.PowerStatus, error) {
	var result platform.PowerStatus
	err := process.decode(ctx, "host-power", "host.power.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) previewProcessSignal(ctx context.Context, operation platform.SignalOperation) (platform.SignalPreview, error) {
	var result platform.SignalPreview
	err := process.decode(ctx, "processes-signal-preview", "processes.signal-preview", operation, &result)
	return result, err
}

func (process *userBridgeProcess) signalProcesses(ctx context.Context, operation platform.SignalOperation) (platform.SignalResult, error) {
	var result platform.SignalResult
	err := process.decode(ctx, "processes-signal", "processes.signal", operation, &result)
	return result, err
}

func (process *userBridgeProcess) readIdentityInventory(ctx context.Context) (platform.IdentityInventory, error) {
	var result platform.IdentityInventory
	err := process.decode(ctx, "identities", "identities.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readFilesystems(ctx context.Context) ([]platform.Filesystem, error) {
	var result []platform.Filesystem
	err := process.decode(ctx, "storage", "storage.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readNetworkSnapshot(ctx context.Context) (platform.NetworkSnapshot, error) {
	var result platform.NetworkSnapshot
	err := process.decode(ctx, "network", "network.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readUpdateStatus(ctx context.Context) (platform.UpdateStatus, error) {
	var result platform.UpdateStatus
	err := process.decode(ctx, "updates", "updates.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readUpdateHistory(ctx context.Context) ([]platform.UpdateHistoryEntry, error) {
	var result []platform.UpdateHistoryEntry
	err := process.decode(ctx, "updates-history", "updates.history", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readUpdateObservation(ctx context.Context) (auth.UpdateObservation, error) {
	var result auth.UpdateObservation
	err := process.decode(ctx, "updates-live", "updates.live", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readKpatch(ctx context.Context) (platform.KpatchStatus, platform.KpatchSettingsStatus, error) {
	var result struct {
		Status   platform.KpatchStatus         `json:"status"`
		Settings platform.KpatchSettingsStatus `json:"settings"`
	}
	if err := process.decode(ctx, "updates-kpatch", "updates.kpatch.read", struct{}{}, &result); err != nil {
		return platform.KpatchStatus{}, platform.KpatchSettingsStatus{}, err
	}
	return result.Status, result.Settings, nil
}

func (process *userBridgeProcess) readCapabilities(ctx context.Context) ([]platform.Capability, error) {
	var result []platform.Capability
	err := process.decode(ctx, "capabilities", "capabilities.read", struct{}{}, &result)
	return result, err
}

func (process *userBridgeProcess) readLoginHistory(ctx context.Context, operation auth.LoginHistoryReadOperation) (platform.LoginHistoryPage, error) {
	var result platform.LoginHistoryPage
	err := process.decode(ctx, "login-history", "login-history.read", operation, &result)
	return result, err
}

func (process *userBridgeProcess) decode(ctx context.Context, id, method string, input, output any) error {
	payload, err := process.call(ctx, id, method, input)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("empty user bridge response")
	}
	return json.Unmarshal(payload, output)
}
