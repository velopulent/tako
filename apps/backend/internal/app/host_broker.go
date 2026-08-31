package app

import (
	"context"
	"errors"
	"net"
	"runtime"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/packagekit"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

// HostBroker is the gateway's only host-access boundary. Production uses the
// typed sessiond socket implementation; development and unit tests use the
// deterministic fake below. No implementation exposes a generic host call.
type HostBroker interface {
	ReadServices(context.Context, auth.HostReadCredentials, auth.ServiceReadOperation) ([]platform.Unit, error)
	ReadUnitDetails(context.Context, auth.HostReadCredentials, string, string) (platform.UnitDetail, error)
	ReadUnitConfiguration(context.Context, auth.HostReadCredentials, string, string) (platform.UnitConfiguration, error)
	ServiceAction(context.Context, auth.HostReadCredentials, platform.ServiceOperation) error
	ReadHostInfo(context.Context, auth.HostReadCredentials) (host.Info, error)
	ReadHostConfiguration(context.Context, auth.HostReadCredentials) (platform.HostConfiguration, error)
	ApplyHostConfiguration(context.Context, auth.HostConfigurationRequest) error
	ReadCertificate(context.Context, auth.HostReadCredentials) (platform.CertificateStatus, error)
	ReadPowerStatus(context.Context, auth.HostReadCredentials) (platform.PowerStatus, error)
	RequestPower(context.Context, auth.PowerRequest) error
	ApplyTimer(context.Context, auth.TimerRequest) (platform.TimerState, error)
	ApplyOverride(context.Context, auth.OverrideRequest) (platform.OverrideState, error)
	ReadProcesses(context.Context, auth.HostReadCredentials) ([]platform.Process, error)
	ReadProcessDetails(context.Context, auth.HostReadCredentials, int, uint64) (platform.ProcessDetails, error)
	PreviewProcessSignal(context.Context, auth.HostReadCredentials, platform.SignalOperation) (platform.SignalPreview, error)
	SignalProcesses(context.Context, auth.SignalRequest) (platform.SignalResult, error)
	ReadIdentityInventory(context.Context, auth.HostReadCredentials) (platform.IdentityInventory, error)
	ReadFilesystems(context.Context, auth.HostReadCredentials) ([]platform.Filesystem, error)
	ReadNetworkSnapshot(context.Context, auth.HostReadCredentials) (platform.NetworkSnapshot, error)
	PreviewNetwork(context.Context, auth.NetworkRequest) (platform.NetworkState, error)
	ApplyNetwork(context.Context, auth.NetworkRequest) (platform.NetworkState, error)
	PreviewFirewall(context.Context, auth.FirewallRequest) (platform.FirewallState, error)
	ApplyFirewall(context.Context, auth.FirewallRequest) (platform.FirewallState, error)
	PreviewSecurity(context.Context, auth.SecurityRequest) (platform.SecurityStatus, error)
	ApplySecurity(context.Context, auth.SecurityRequest) (platform.SecurityStatus, error)
	ReadUpdateStatus(context.Context, auth.HostReadCredentials) (platform.UpdateStatus, error)
	PreviewUpdates(context.Context, auth.HostReadCredentials, platform.UpdateOperation) (platform.UpdatePreview, error)
	ApplyUpdates(context.Context, auth.UpdateRequest) (platform.UpdateResult, error)
	RefreshUpdates(context.Context, string, bool) (platform.UpdateStatus, error)
	ReadUpdateHistory(context.Context, auth.HostReadCredentials) ([]platform.UpdateHistoryEntry, error)
	ReadUpdateObservation(context.Context, auth.HostReadCredentials) (auth.UpdateObservation, error)
	CancelUpdate(context.Context, string) (bool, error)
	ReadAutoUpdatesStatus(context.Context, auth.HostReadCredentials) (platform.AutoUpdatesConfig, error)
	ApplyAutoUpdates(context.Context, auth.AutoUpdatesRequest) (platform.AutoUpdatesConfig, error)
	ReadKpatch(context.Context, auth.HostReadCredentials) (platform.KpatchStatus, platform.KpatchSettingsStatus, error)
	ApplyKpatch(context.Context, auth.KpatchRequest) (platform.KpatchSettingsStatus, error)
	ReadCapabilities(context.Context, auth.HostReadCredentials) ([]platform.Capability, error)
	ReadLoginHistory(context.Context, auth.HostReadCredentials, platform.LoginHistoryQuery) (platform.LoginHistoryPage, error)
	PreviewLocalGroup(context.Context, string, platform.LocalGroupOperation) (platform.LocalGroupPreview, error)
	ApplyLocalGroup(context.Context, string, platform.LocalGroupOperation) (platform.LocalGroupState, error)
	PreviewLocalAccount(context.Context, auth.LocalAccountRequest) (platform.LocalAccountPreview, error)
	ApplyLocalAccount(context.Context, auth.LocalAccountRequest) (platform.LocalAccountState, error)
	PreviewGroupMembership(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error)
	ApplyGroupMembership(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipState, error)
	PreviewAdministrativeRole(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error)
	ApplyAdministrativeRole(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error)
	ChangePassword(context.Context, auth.PasswordChangeRequest) error
	PreviewSSHKeys(context.Context, auth.SSHKeyRequest) (platform.SSHKeyPreview, error)
	ApplySSHKeys(context.Context, auth.SSHKeyRequest) (platform.SSHKeyState, error)
	ApplyFileOperation(context.Context, auth.FileRequest) (platform.FileResult, error)
	CollectSupportReport(context.Context, auth.SupportReportRequest) (platform.SupportReport, error)
	ReadMetricHistory(context.Context, auth.HostReadCredentials, time.Time, int) ([]metrics.Sample, error)
	FollowMetrics(context.Context, auth.HostReadCredentials, time.Duration, func(metrics.Sample) error) error
	QueryJournal(context.Context, auth.JournalRequest) (platform.JournalPage, error)
	FollowJournal(context.Context, auth.JournalRequest, func(platform.LogEntry) error) error
	OpenTerminal(context.Context, string, uint16, uint16) (net.Conn, error)
	ResizeTerminal(context.Context, string, uint16, uint16) error
}

func (server *Server) hostBroker() HostBroker {
	if server != nil && server.broker != nil {
		return server.broker
	}
	return fakeHostBroker{}
}

func credentialsFromContext(ctx context.Context) auth.HostReadCredentials {
	current, ok := ctx.Value(sessionKey{}).(session.Session)
	if ok {
		if hasAdministrativeAccess(current) {
			return auth.HostReadCredentials{AdminToken: current.Identity.AdminToken}
		}
		return auth.HostReadCredentials{Token: current.Identity.BridgeToken}
	}
	if credentials, ok := ctx.Value(hostCredentialsKey{}).(auth.HostReadCredentials); ok {
		return credentials
	}
	return auth.HostReadCredentials{}
}

// hostCredentialsKey carries a short-lived job grant without putting an
// authentication token in the durable job row or in the worker's generic API.
type hostCredentialsKey struct{}

func credentialsForScope(ctx context.Context, scope string) auth.HostReadCredentials {
	if scope == "user" {
		current, ok := ctx.Value(sessionKey{}).(session.Session)
		if !ok {
			return auth.HostReadCredentials{}
		}
		// Administrative elevation never changes the user identity. User units
		// must continue through the PAM-derived bridge even while the same
		// browser session has a root grant.
		return auth.HostReadCredentials{Token: current.Identity.BridgeToken}
	}
	return credentialsFromContext(ctx)
}

type socketHostBroker struct {
	path string
}

func (broker socketHostBroker) ReadServices(ctx context.Context, credentials auth.HostReadCredentials, operation auth.ServiceReadOperation) ([]platform.Unit, error) {
	return auth.ReadServices(ctx, broker.path, credentials, operation)
}

func (broker socketHostBroker) ReadUnitDetails(ctx context.Context, credentials auth.HostReadCredentials, scope, unit string) (platform.UnitDetail, error) {
	return auth.ReadUnitDetails(ctx, broker.path, credentials, scope, unit)
}

func (broker socketHostBroker) ReadUnitConfiguration(ctx context.Context, credentials auth.HostReadCredentials, scope, unit string) (platform.UnitConfiguration, error) {
	return auth.ReadUnitConfiguration(ctx, broker.path, credentials, scope, unit)
}

func (broker socketHostBroker) ServiceAction(ctx context.Context, credentials auth.HostReadCredentials, operation platform.ServiceOperation) error {
	return auth.ServiceActionTyped(ctx, broker.path, credentials, operation)
}

func (broker socketHostBroker) ReadHostInfo(ctx context.Context, credentials auth.HostReadCredentials) (host.Info, error) {
	return auth.ReadHostInfo(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadHostConfiguration(ctx context.Context, credentials auth.HostReadCredentials) (platform.HostConfiguration, error) {
	return auth.ReadHostConfiguration(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ApplyHostConfiguration(ctx context.Context, request auth.HostConfigurationRequest) error {
	return auth.ApplyHostConfiguration(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadCertificate(ctx context.Context, credentials auth.HostReadCredentials) (platform.CertificateStatus, error) {
	return auth.ReadCertificate(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadPowerStatus(ctx context.Context, credentials auth.HostReadCredentials) (platform.PowerStatus, error) {
	return auth.ReadPowerStatus(ctx, broker.path, credentials)
}

func (broker socketHostBroker) RequestPower(ctx context.Context, request auth.PowerRequest) error {
	return auth.RequestPower(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyTimer(ctx context.Context, request auth.TimerRequest) (platform.TimerState, error) {
	return auth.ApplyTimer(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyOverride(ctx context.Context, request auth.OverrideRequest) (platform.OverrideState, error) {
	return auth.ApplyOverride(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadProcesses(ctx context.Context, credentials auth.HostReadCredentials) ([]platform.Process, error) {
	return auth.ReadProcesses(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadProcessDetails(ctx context.Context, credentials auth.HostReadCredentials, pid int, started uint64) (platform.ProcessDetails, error) {
	return auth.ReadProcessDetails(ctx, broker.path, credentials, pid, started)
}

func (broker socketHostBroker) PreviewProcessSignal(ctx context.Context, credentials auth.HostReadCredentials, operation platform.SignalOperation) (platform.SignalPreview, error) {
	return auth.PreviewProcessSignal(ctx, broker.path, credentials, operation)
}

func (broker socketHostBroker) SignalProcesses(ctx context.Context, request auth.SignalRequest) (platform.SignalResult, error) {
	if request.Token != "" && request.AdminToken == "" {
		return auth.SignalProcessesTyped(ctx, broker.path, auth.HostReadCredentials{Token: request.Token}, request.Operation)
	}
	return auth.SignalProcesses(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadIdentityInventory(ctx context.Context, credentials auth.HostReadCredentials) (platform.IdentityInventory, error) {
	return auth.ReadIdentityInventory(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadFilesystems(ctx context.Context, credentials auth.HostReadCredentials) ([]platform.Filesystem, error) {
	return auth.ReadFilesystems(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadNetworkSnapshot(ctx context.Context, credentials auth.HostReadCredentials) (platform.NetworkSnapshot, error) {
	return auth.ReadNetworkSnapshot(ctx, broker.path, credentials)
}

func (broker socketHostBroker) PreviewNetwork(ctx context.Context, request auth.NetworkRequest) (platform.NetworkState, error) {
	return auth.PreviewNetwork(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyNetwork(ctx context.Context, request auth.NetworkRequest) (platform.NetworkState, error) {
	return auth.ApplyNetwork(ctx, broker.path, request)
}

func (broker socketHostBroker) PreviewFirewall(ctx context.Context, request auth.FirewallRequest) (platform.FirewallState, error) {
	return auth.PreviewFirewall(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyFirewall(ctx context.Context, request auth.FirewallRequest) (platform.FirewallState, error) {
	return auth.ApplyFirewall(ctx, broker.path, request)
}

func (broker socketHostBroker) PreviewSecurity(ctx context.Context, request auth.SecurityRequest) (platform.SecurityStatus, error) {
	return auth.PreviewSecurity(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplySecurity(ctx context.Context, request auth.SecurityRequest) (platform.SecurityStatus, error) {
	return auth.ApplySecurity(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadUpdateStatus(ctx context.Context, credentials auth.HostReadCredentials) (platform.UpdateStatus, error) {
	return auth.ReadUpdateStatus(ctx, broker.path, credentials)
}

func (broker socketHostBroker) PreviewUpdates(ctx context.Context, credentials auth.HostReadCredentials, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
	status, err := broker.ReadUpdateStatus(ctx, credentials)
	if err != nil {
		return platform.UpdatePreview{}, err
	}
	return platform.PreviewUpdates(ctx, operation, func(context.Context) platform.UpdateStatus { return status })
}

func (broker socketHostBroker) ApplyUpdates(ctx context.Context, request auth.UpdateRequest) (platform.UpdateResult, error) {
	return auth.ApplyUpdates(ctx, broker.path, request)
}

func (broker socketHostBroker) RefreshUpdates(ctx context.Context, adminToken string, force bool) (platform.UpdateStatus, error) {
	return auth.RefreshUpdates(ctx, broker.path, adminToken, force)
}

func (broker socketHostBroker) ReadUpdateHistory(ctx context.Context, credentials auth.HostReadCredentials) ([]platform.UpdateHistoryEntry, error) {
	return auth.ReadUpdateHistory(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadUpdateObservation(ctx context.Context, credentials auth.HostReadCredentials) (auth.UpdateObservation, error) {
	return auth.ReadUpdateObservation(ctx, broker.path, credentials)
}

func (broker socketHostBroker) CancelUpdate(ctx context.Context, adminToken string) (bool, error) {
	return auth.CancelUpdate(ctx, broker.path, adminToken)
}

func (broker socketHostBroker) ReadAutoUpdatesStatus(ctx context.Context, credentials auth.HostReadCredentials) (platform.AutoUpdatesConfig, error) {
	return auth.ReadAutoUpdatesStatus(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ApplyAutoUpdates(ctx context.Context, request auth.AutoUpdatesRequest) (platform.AutoUpdatesConfig, error) {
	return auth.ApplyAutoUpdatesConfig(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadKpatch(ctx context.Context, credentials auth.HostReadCredentials) (platform.KpatchStatus, platform.KpatchSettingsStatus, error) {
	return auth.ReadKpatch(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ApplyKpatch(ctx context.Context, request auth.KpatchRequest) (platform.KpatchSettingsStatus, error) {
	return auth.ApplyKpatchSettings(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadCapabilities(ctx context.Context, credentials auth.HostReadCredentials) ([]platform.Capability, error) {
	return auth.ReadCapabilities(ctx, broker.path, credentials)
}

func (broker socketHostBroker) ReadLoginHistory(ctx context.Context, credentials auth.HostReadCredentials, query platform.LoginHistoryQuery) (platform.LoginHistoryPage, error) {
	return auth.ReadLoginHistory(ctx, broker.path, credentials, query)
}

func (broker socketHostBroker) PreviewLocalGroup(ctx context.Context, adminToken string, operation platform.LocalGroupOperation) (platform.LocalGroupPreview, error) {
	return auth.PreviewLocalGroup(ctx, broker.path, adminToken, operation)
}

func (broker socketHostBroker) ApplyLocalGroup(ctx context.Context, adminToken string, operation platform.LocalGroupOperation) (platform.LocalGroupState, error) {
	return auth.ApplyLocalGroup(ctx, broker.path, adminToken, operation)
}

func (broker socketHostBroker) PreviewLocalAccount(ctx context.Context, request auth.LocalAccountRequest) (platform.LocalAccountPreview, error) {
	return auth.PreviewLocalAccount(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyLocalAccount(ctx context.Context, request auth.LocalAccountRequest) (platform.LocalAccountState, error) {
	return auth.ApplyLocalAccount(ctx, broker.path, request)
}

func (broker socketHostBroker) PreviewGroupMembership(ctx context.Context, request auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error) {
	return auth.PreviewGroupMembership(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyGroupMembership(ctx context.Context, request auth.GroupMembershipRequest) (platform.GroupMembershipState, error) {
	return auth.ApplyGroupMembership(ctx, broker.path, request)
}

func (broker socketHostBroker) PreviewAdministrativeRole(ctx context.Context, request auth.AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error) {
	return auth.PreviewAdministrativeRole(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyAdministrativeRole(ctx context.Context, request auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error) {
	return auth.ApplyAdministrativeRole(ctx, broker.path, request)
}

func (broker socketHostBroker) ChangePassword(ctx context.Context, request auth.PasswordChangeRequest) error {
	return auth.ChangePassword(ctx, broker.path, request)
}

func (broker socketHostBroker) PreviewSSHKeys(ctx context.Context, request auth.SSHKeyRequest) (platform.SSHKeyPreview, error) {
	return auth.PreviewSSHKeys(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplySSHKeys(ctx context.Context, request auth.SSHKeyRequest) (platform.SSHKeyState, error) {
	return auth.ApplySSHKeys(ctx, broker.path, request)
}

func (broker socketHostBroker) ApplyFileOperation(ctx context.Context, request auth.FileRequest) (platform.FileResult, error) {
	return auth.ApplyFileOperation(ctx, broker.path, request)
}

func (broker socketHostBroker) CollectSupportReport(ctx context.Context, request auth.SupportReportRequest) (platform.SupportReport, error) {
	return auth.CollectSupportReport(ctx, broker.path, request)
}

func (broker socketHostBroker) ReadMetricHistory(ctx context.Context, credentials auth.HostReadCredentials, since time.Time, maximum int) ([]metrics.Sample, error) {
	return auth.ReadMetricHistory(ctx, broker.path, credentials, since, maximum)
}

func (broker socketHostBroker) FollowMetrics(ctx context.Context, credentials auth.HostReadCredentials, interval time.Duration, emit func(metrics.Sample) error) error {
	return auth.FollowMetrics(ctx, broker.path, credentials, interval, emit)
}

func (broker socketHostBroker) QueryJournal(ctx context.Context, request auth.JournalRequest) (platform.JournalPage, error) {
	return auth.QueryJournal(ctx, broker.path, request)
}

func (broker socketHostBroker) FollowJournal(ctx context.Context, request auth.JournalRequest, emit func(platform.LogEntry) error) error {
	return auth.FollowJournal(ctx, broker.path, request, emit)
}

func (broker socketHostBroker) OpenTerminal(ctx context.Context, token string, columns, rows uint16) (net.Conn, error) {
	return auth.OpenTerminal(ctx, broker.path, token, columns, rows)
}

func (broker socketHostBroker) ResizeTerminal(ctx context.Context, token string, columns, rows uint16) error {
	return auth.ResizeTerminal(ctx, broker.path, token, columns, rows)
}

// fakeHostBroker keeps development and unit tests deterministic and, more
// importantly, prevents a test-created gateway from accidentally probing the
// host it happens to run on.
type fakeHostBroker struct{}

func (fakeHostBroker) ReadServices(context.Context, auth.HostReadCredentials, auth.ServiceReadOperation) ([]platform.Unit, error) {
	return []platform.Unit{}, nil
}

func (fakeHostBroker) ReadUnitDetails(_ context.Context, _ auth.HostReadCredentials, scope, unit string) (platform.UnitDetail, error) {
	return platform.UnitDetail{Unit: platform.Unit{Name: unit, Scope: scope, LoadState: "loaded", ActiveState: "inactive", SubState: "dead"}}, nil
}

func (fakeHostBroker) ReadUnitConfiguration(_ context.Context, _ auth.HostReadCredentials, scope, unit string) (platform.UnitConfiguration, error) {
	return platform.UnitConfiguration{Path: unit, Content: "", Truncated: false}, nil
}

func (fakeHostBroker) ServiceAction(context.Context, auth.HostReadCredentials, platform.ServiceOperation) error {
	return nil
}

func (fakeHostBroker) ReadHostInfo(context.Context, auth.HostReadCredentials) (host.Info, error) {
	return host.Info{Hostname: "development", OperatingSystem: "Tako development host", Kernel: "development", Architecture: runtime.GOARCH, Hardware: host.Hardware{Available: true, CPUCores: runtime.NumCPU()}}, nil
}

func (fakeHostBroker) ReadHostConfiguration(context.Context, auth.HostReadCredentials) (platform.HostConfiguration, error) {
	return platform.NewHostConfiguration("development", "UTC", true), nil
}

func (fakeHostBroker) ApplyHostConfiguration(context.Context, auth.HostConfigurationRequest) error {
	return nil
}

func (fakeHostBroker) ReadCertificate(context.Context, auth.HostReadCredentials) (platform.CertificateStatus, error) {
	return platform.CertificateStatus{Warning: "HTTPS certificate status is not available in development mode."}, nil
}

func (fakeHostBroker) ApplyTimer(context.Context, auth.TimerRequest) (platform.TimerState, error) {
	return platform.TimerState{}, nil
}

func (fakeHostBroker) ApplyOverride(context.Context, auth.OverrideRequest) (platform.OverrideState, error) {
	return platform.OverrideState{}, nil
}

func (fakeHostBroker) ReadPowerStatus(context.Context, auth.HostReadCredentials) (platform.PowerStatus, error) {
	return platform.PowerStatus{Available: true, Reboot: platform.PowerActionStatus{State: "available", Available: true}, Shutdown: platform.PowerActionStatus{State: "available", Available: true}, Inhibitors: []platform.PowerInhibitor{}}, nil
}

func (fakeHostBroker) RequestPower(context.Context, auth.PowerRequest) error { return nil }

func (fakeHostBroker) ReadProcesses(context.Context, auth.HostReadCredentials) ([]platform.Process, error) {
	return []platform.Process{}, nil
}

func (fakeHostBroker) ReadProcessDetails(context.Context, auth.HostReadCredentials, int, uint64) (platform.ProcessDetails, error) {
	return platform.ProcessDetails{}, errors.New("process-not-found")
}

func (fakeHostBroker) PreviewProcessSignal(context.Context, auth.HostReadCredentials, platform.SignalOperation) (platform.SignalPreview, error) {
	return platform.SignalPreview{}, errors.New("process-not-found")
}

func (fakeHostBroker) SignalProcesses(context.Context, auth.SignalRequest) (platform.SignalResult, error) {
	return platform.SignalResult{}, errors.New("permission-denied")
}

func (fakeHostBroker) ReadIdentityInventory(context.Context, auth.HostReadCredentials) (platform.IdentityInventory, error) {
	return platform.IdentityInventory{Users: []platform.User{}, Groups: []platform.IdentityGroup{}}, nil
}

func (fakeHostBroker) ReadFilesystems(context.Context, auth.HostReadCredentials) ([]platform.Filesystem, error) {
	return []platform.Filesystem{}, nil
}

func (fakeHostBroker) ReadNetworkSnapshot(context.Context, auth.HostReadCredentials) (platform.NetworkSnapshot, error) {
	return platform.NetworkSnapshot{Interfaces: []platform.Interface{}, Addresses: []platform.NetworkAddress{}, Routes: []platform.NetworkRoute{}, DNS: []string{}}, nil
}

func (fakeHostBroker) PreviewNetwork(context.Context, auth.NetworkRequest) (platform.NetworkState, error) {
	return platform.NetworkState{}, nil
}

func (fakeHostBroker) ApplyNetwork(context.Context, auth.NetworkRequest) (platform.NetworkState, error) {
	return platform.NetworkState{}, nil
}

func (fakeHostBroker) PreviewFirewall(context.Context, auth.FirewallRequest) (platform.FirewallState, error) {
	return platform.FirewallState{}, nil
}

func (fakeHostBroker) ApplyFirewall(context.Context, auth.FirewallRequest) (platform.FirewallState, error) {
	return platform.FirewallState{}, nil
}

func (fakeHostBroker) PreviewSecurity(context.Context, auth.SecurityRequest) (platform.SecurityStatus, error) {
	return platform.SecurityStatus{}, nil
}

func (fakeHostBroker) ApplySecurity(context.Context, auth.SecurityRequest) (platform.SecurityStatus, error) {
	return platform.SecurityStatus{}, nil
}

func (fakeHostBroker) ReadUpdateStatus(context.Context, auth.HostReadCredentials) (platform.UpdateStatus, error) {
	return platform.UpdateStatus{Available: false, Packages: []platform.UpdatePackage{}}, nil
}

func (broker fakeHostBroker) PreviewUpdates(ctx context.Context, credentials auth.HostReadCredentials, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
	status, err := broker.ReadUpdateStatus(ctx, credentials)
	if err != nil {
		return platform.UpdatePreview{}, err
	}
	return platform.PreviewUpdates(ctx, operation, func(context.Context) platform.UpdateStatus { return status })
}

func (fakeHostBroker) ApplyUpdates(context.Context, auth.UpdateRequest) (platform.UpdateResult, error) {
	return platform.UpdateResult{}, errors.New("system-backend-unavailable")
}

func (broker fakeHostBroker) RefreshUpdates(ctx context.Context, _ string, _ bool) (platform.UpdateStatus, error) {
	return broker.ReadUpdateStatus(ctx, auth.HostReadCredentials{})
}

func (fakeHostBroker) ReadUpdateHistory(context.Context, auth.HostReadCredentials) ([]platform.UpdateHistoryEntry, error) {
	return []platform.UpdateHistoryEntry{}, nil
}

func (fakeHostBroker) ReadUpdateObservation(context.Context, auth.HostReadCredentials) (auth.UpdateObservation, error) {
	return auth.UpdateObservation{Live: platform.InactiveUpdateLive(), Log: []packagekit.ActionLogEntry{}}, nil
}

func (fakeHostBroker) CancelUpdate(context.Context, string) (bool, error) { return false, nil }

func (fakeHostBroker) ReadAutoUpdatesStatus(context.Context, auth.HostReadCredentials) (platform.AutoUpdatesConfig, error) {
	return platform.AutoUpdatesConfig{}, nil
}

func (fakeHostBroker) ApplyAutoUpdates(context.Context, auth.AutoUpdatesRequest) (platform.AutoUpdatesConfig, error) {
	return platform.AutoUpdatesConfig{}, nil
}

func (fakeHostBroker) ReadKpatch(context.Context, auth.HostReadCredentials) (platform.KpatchStatus, platform.KpatchSettingsStatus, error) {
	return platform.KpatchStatus{}, platform.KpatchSettingsStatus{}, nil
}

func (fakeHostBroker) ApplyKpatch(context.Context, auth.KpatchRequest) (platform.KpatchSettingsStatus, error) {
	return platform.KpatchSettingsStatus{}, nil
}

func (fakeHostBroker) ReadCapabilities(context.Context, auth.HostReadCredentials) ([]platform.Capability, error) {
	return []platform.Capability{{ID: "dashboard", State: platform.StateReady, Backend: "built-in", Readable: true, Contract: "built-in", ReadAuthority: "gateway", MutationAuthority: "none"}}, nil
}

func (fakeHostBroker) ReadLoginHistory(context.Context, auth.HostReadCredentials, platform.LoginHistoryQuery) (platform.LoginHistoryPage, error) {
	return platform.LoginHistoryPage{Items: []platform.LoginHistoryEntry{}}, nil
}

func (fakeHostBroker) PreviewLocalGroup(context.Context, string, platform.LocalGroupOperation) (platform.LocalGroupPreview, error) {
	return platform.LocalGroupPreview{}, nil
}

func (fakeHostBroker) ApplyLocalGroup(context.Context, string, platform.LocalGroupOperation) (platform.LocalGroupState, error) {
	return platform.LocalGroupState{}, nil
}

func (fakeHostBroker) PreviewLocalAccount(context.Context, auth.LocalAccountRequest) (platform.LocalAccountPreview, error) {
	return platform.LocalAccountPreview{}, nil
}

func (fakeHostBroker) ApplyLocalAccount(context.Context, auth.LocalAccountRequest) (platform.LocalAccountState, error) {
	return platform.LocalAccountState{}, nil
}

func (fakeHostBroker) PreviewGroupMembership(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipPreview, error) {
	return platform.GroupMembershipPreview{}, nil
}

func (fakeHostBroker) ApplyGroupMembership(context.Context, auth.GroupMembershipRequest) (platform.GroupMembershipState, error) {
	return platform.GroupMembershipState{}, nil
}

func (fakeHostBroker) PreviewAdministrativeRole(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRolePreview, error) {
	return platform.AdministrativeRolePreview{}, nil
}

func (fakeHostBroker) ApplyAdministrativeRole(context.Context, auth.AdministrativeRoleRequest) (platform.AdministrativeRoleState, error) {
	return platform.AdministrativeRoleState{}, nil
}

func (fakeHostBroker) ChangePassword(context.Context, auth.PasswordChangeRequest) error {
	return errors.New("system-backend-unavailable")
}

func (fakeHostBroker) PreviewSSHKeys(context.Context, auth.SSHKeyRequest) (platform.SSHKeyPreview, error) {
	return platform.SSHKeyPreview{}, nil
}

func (fakeHostBroker) ApplySSHKeys(context.Context, auth.SSHKeyRequest) (platform.SSHKeyState, error) {
	return platform.SSHKeyState{}, nil
}

func (fakeHostBroker) ApplyFileOperation(context.Context, auth.FileRequest) (platform.FileResult, error) {
	return platform.FileResult{}, errors.New("system-backend-unavailable")
}

func (fakeHostBroker) CollectSupportReport(context.Context, auth.SupportReportRequest) (platform.SupportReport, error) {
	return platform.SupportReport{}, errors.New("system-backend-unavailable")
}

func (fakeHostBroker) ReadMetricHistory(context.Context, auth.HostReadCredentials, time.Time, int) ([]metrics.Sample, error) {
	return []metrics.Sample{}, nil
}

func (fakeHostBroker) FollowMetrics(ctx context.Context, _ auth.HostReadCredentials, interval time.Duration, emit func(metrics.Sample) error) error {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := emit(metrics.Sample{Timestamp: now.UTC()}); err != nil {
				return nil
			}
		}
	}
}

func (fakeHostBroker) QueryJournal(context.Context, auth.JournalRequest) (platform.JournalPage, error) {
	return platform.JournalPage{Items: []platform.LogEntry{}}, nil
}

func (fakeHostBroker) FollowJournal(ctx context.Context, _ auth.JournalRequest, _ func(platform.LogEntry) error) error {
	<-ctx.Done()
	return nil
}

func (fakeHostBroker) OpenTerminal(context.Context, string, uint16, uint16) (net.Conn, error) {
	return nil, errors.New("user-manager-unavailable")
}

func (fakeHostBroker) ResizeTerminal(context.Context, string, uint16, uint16) error {
	return errors.New("user-manager-unavailable")
}

var _ HostBroker = socketHostBroker{}
var _ HostBroker = fakeHostBroker{}
