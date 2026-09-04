package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"

	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
)

// HostReadCredentials select the already-authenticated execution lane. The
// token values are never forwarded to the user bridge; sessiond resolves them
// to the grant and its real UNIX identity.
type HostReadCredentials struct {
	Token      string
	AdminToken string
}

type ServiceReadOperation struct {
	Scope string `json:"scope,omitempty"`
	Type  string `json:"type,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

type HostInfoReadOperation struct{}
type HostConfigReadOperation struct{}
type CertificateReadOperation struct{}
type PowerReadOperation struct{}
type IdentityReadOperation struct{}
type StorageReadOperation struct{}
type NetworkReadOperation struct{}
type UpdateReadOperation struct{}
type UpdatePreviewOperation struct{ Operation platform.UpdateOperation }
type UpdateHistoryReadOperation struct{}
type UpdateLiveReadOperation struct{}
type KpatchReadOperation struct{}
type CapabilitiesReadOperation struct{}

type ProcessReadOperation struct {
	PID     int    `json:"pid,omitempty"`
	Started uint64 `json:"started,omitempty"`
}

type UpdateRefreshOperation struct {
	Force bool `json:"force"`
}

type LoginHistoryReadOperation struct {
	Query platform.LoginHistoryQuery `json:"query"`
}

type MetricsHistoryOperation struct {
	Since   time.Time `json:"since,omitempty"`
	Maximum int       `json:"maximum,omitempty"`
}

type MetricsFollowOperation struct {
	Interval time.Duration `json:"interval,omitempty"`
}

type UpdateObservation = platform.UpdateObservation

func readHostResponse(ctx context.Context, path string, credentials HostReadCredentials, operation string, payload any, timeout time.Duration, limit int64) (Response, error) {
	request := Request{Operation: operation, Token: credentials.Token, AdminToken: credentials.AdminToken}
	switch value := payload.(type) {
	case *ServiceReadOperation:
		request.ServiceRead = value
	case *platform.ServiceOperation:
		request.ServiceAction = value
	case *HostInfoReadOperation:
		request.HostInfoRead = value
	case *HostConfigReadOperation:
		request.HostConfigRead = value
	case *CertificateReadOperation:
		request.CertificateRead = value
	case *PowerReadOperation:
		request.PowerRead = value
	case *ProcessReadOperation:
		request.ProcessRead = value
	case *IdentityReadOperation:
		request.IdentityRead = value
	case *StorageReadOperation:
		request.StorageRead = value
	case *NetworkReadOperation:
		request.NetworkRead = value
	case *UpdateReadOperation:
		request.UpdateRead = value
	case *UpdatePreviewOperation:
		request.UpdatePreview = &value.Operation
	case *UpdateRefreshOperation:
		request.UpdateRefresh = value
	case *UpdateHistoryReadOperation:
		request.UpdateHistoryRead = value
	case *UpdateLiveReadOperation:
		request.UpdateLiveRead = value
	case *KpatchReadOperation:
		request.KpatchRead = value
	case *CapabilitiesReadOperation:
		request.CapabilitiesRead = value
	case *LoginHistoryReadOperation:
		request.LoginHistoryRead = value
	case *platform.SignalOperation:
		request.SignalPreview = value
	case *platform.LocalGroupOperation:
		request.LocalGroup = value
	case *MetricsHistoryOperation:
		request.MetricsHistory = value
	default:
		return Response{}, errors.New("invalid host read operation")
	}
	response, err := socketRequestWithLimit(ctx, path, request, timeout, limit)
	if err != nil {
		return Response{}, normalizeHostTransportError(ctx, err)
	}
	if response.Error != "" {
		return Response{}, hostResponseError(response.Error)
	}
	return response, nil
}

// ServiceActionTyped sends the allowlisted service action through the typed
// host-operation lane. The older ServiceAction helpers remain for wire
// compatibility with existing session clients and tests.
func ServiceActionTyped(ctx context.Context, path string, credentials HostReadCredentials, operation platform.ServiceOperation) error {
	_, err := readHostResponse(ctx, path, credentials, "services.action", &operation, 20*time.Second, 128<<10)
	return err
}

func hostResponseError(code string) error {
	if isStableHostResponseCode(code) {
		return errors.New(code)
	}
	return errors.New("system-backend-unavailable")
}

func isStableHostResponseCode(code string) bool {
	switch code {
	case "invalid-request", "invalid-bridge-token", "invalid-admin-token", "user-bridge-unavailable", "user-manager-unavailable", "permission-denied", "system-backend-unavailable", "host-read-unavailable", "timeout", "bounded-output", "conflict", "invalid-service-operation", "invalid-signal-operation", "process-not-found", "process-reused", "process-signal-conflict", "invalid-login-history-query":
		return true
	default:
		return false
	}
}

func normalizeHostTransportError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timeout")
	}
	return errors.New("system-backend-unavailable")
}

func ReadServices(ctx context.Context, path string, credentials HostReadCredentials, operation ServiceReadOperation) ([]platform.Unit, error) {
	response, err := readHostResponse(ctx, path, credentials, "services.list", &operation, 20*time.Second, 8<<20)
	if err != nil {
		return nil, err
	}
	if response.Units == nil {
		return []platform.Unit{}, nil
	}
	return response.Units, nil
}

func ReadUnitDetails(ctx context.Context, path string, credentials HostReadCredentials, scope, unit string) (platform.UnitDetail, error) {
	operation := ServiceReadOperation{Scope: scope, Unit: unit}
	response, err := readHostResponse(ctx, path, credentials, "services.detail", &operation, 20*time.Second, 2<<20)
	if err != nil {
		return platform.UnitDetail{}, err
	}
	if response.UnitDetail == nil {
		return platform.UnitDetail{}, ErrServiceUnavailable
	}
	return *response.UnitDetail, nil
}

func ReadUnitConfiguration(ctx context.Context, path string, credentials HostReadCredentials, scope, unit string) (platform.UnitConfiguration, error) {
	operation := ServiceReadOperation{Scope: scope, Unit: unit}
	response, err := readHostResponse(ctx, path, credentials, "services.configuration", &operation, 20*time.Second, 2<<20)
	if err != nil {
		return platform.UnitConfiguration{}, err
	}
	if response.UnitConfiguration == nil {
		return platform.UnitConfiguration{}, ErrServiceUnavailable
	}
	return *response.UnitConfiguration, nil
}

func ReadHostInfo(ctx context.Context, path string, credentials HostReadCredentials) (host.Info, error) {
	response, err := readHostResponse(ctx, path, credentials, "host.info", &HostInfoReadOperation{}, 10*time.Second, 512<<10)
	if err != nil {
		return host.Info{}, err
	}
	if response.HostInfo == nil {
		return host.Info{}, ErrServiceUnavailable
	}
	return *response.HostInfo, nil
}

func ReadHostConfiguration(ctx context.Context, path string, credentials HostReadCredentials) (platform.HostConfiguration, error) {
	response, err := readHostResponse(ctx, path, credentials, "host.configuration.read", &HostConfigReadOperation{}, 20*time.Second, 128<<10)
	if err != nil {
		return platform.HostConfiguration{}, err
	}
	if response.HostConfiguration == nil {
		return platform.HostConfiguration{}, ErrServiceUnavailable
	}
	return *response.HostConfiguration, nil
}

func ReadCertificate(ctx context.Context, path string, credentials HostReadCredentials) (platform.CertificateStatus, error) {
	response, err := readHostResponse(ctx, path, credentials, "certificate.read", &CertificateReadOperation{}, 10*time.Second, 2<<20)
	if err != nil {
		return platform.CertificateStatus{}, err
	}
	if response.CertificateStatus == nil {
		return platform.CertificateStatus{}, ErrServiceUnavailable
	}
	return *response.CertificateStatus, nil
}

func ReadPowerStatus(ctx context.Context, path string, credentials HostReadCredentials) (platform.PowerStatus, error) {
	response, err := readHostResponse(ctx, path, credentials, "host.power.read", &PowerReadOperation{}, 20*time.Second, 512<<10)
	if err != nil {
		return platform.PowerStatus{}, err
	}
	if response.PowerStatus == nil {
		return platform.PowerStatus{}, ErrServiceUnavailable
	}
	return *response.PowerStatus, nil
}

func ReadProcesses(ctx context.Context, path string, credentials HostReadCredentials) ([]platform.Process, error) {
	response, err := readHostResponse(ctx, path, credentials, "processes.list", &ProcessReadOperation{}, 20*time.Second, 8<<20)
	if err != nil {
		return nil, err
	}
	if response.Processes == nil {
		return []platform.Process{}, nil
	}
	return response.Processes, nil
}

func ReadProcessDetails(ctx context.Context, path string, credentials HostReadCredentials, pid int, started uint64) (platform.ProcessDetails, error) {
	operation := ProcessReadOperation{PID: pid, Started: started}
	response, err := readHostResponse(ctx, path, credentials, "processes.detail", &operation, 30*time.Second, 2<<20)
	if err != nil {
		return platform.ProcessDetails{}, err
	}
	if response.ProcessDetails == nil {
		return platform.ProcessDetails{}, ErrServiceUnavailable
	}
	return *response.ProcessDetails, nil
}

func PreviewProcessSignal(ctx context.Context, path string, credentials HostReadCredentials, operation platform.SignalOperation) (platform.SignalPreview, error) {
	response, err := readHostResponse(ctx, path, credentials, "processes.signal-preview", &operation, 20*time.Second, 2<<20)
	if err != nil {
		return platform.SignalPreview{}, err
	}
	if response.SignalPreview == nil {
		return platform.SignalPreview{}, ErrServiceUnavailable
	}
	return *response.SignalPreview, nil
}

// SignalProcessesTyped routes an already validated user signal through the
// matching PAM-derived bridge. Administrative callers continue to use the
// legacy root sessiond operation, which has a separate authorization lane.
func SignalProcessesTyped(ctx context.Context, path string, credentials HostReadCredentials, operation platform.SignalOperation) (platform.SignalResult, error) {
	request := Request{Operation: "processes.signal", Token: credentials.Token, AdminToken: credentials.AdminToken, SignalApply: &operation}
	response, err := socketRequestWithLimit(ctx, path, request, 20*time.Second, 2<<20)
	if err != nil {
		return platform.SignalResult{}, normalizeHostTransportError(ctx, err)
	}
	if response.Error != "" {
		return platform.SignalResult{}, hostResponseError(response.Error)
	}
	if response.SignalResult == nil {
		return platform.SignalResult{}, ErrServiceUnavailable
	}
	return *response.SignalResult, nil
}

func ReadIdentityInventory(ctx context.Context, path string, credentials HostReadCredentials) (platform.IdentityInventory, error) {
	response, err := readHostResponse(ctx, path, credentials, "identities.read", &IdentityReadOperation{}, 30*time.Second, 8<<20)
	if err != nil {
		return platform.IdentityInventory{}, err
	}
	if response.IdentityInventory == nil {
		return platform.IdentityInventory{}, ErrServiceUnavailable
	}
	return *response.IdentityInventory, nil
}

func ReadFilesystems(ctx context.Context, path string, credentials HostReadCredentials) ([]platform.Filesystem, error) {
	response, err := readHostResponse(ctx, path, credentials, "storage.read", &StorageReadOperation{}, 20*time.Second, 4<<20)
	if err != nil {
		return nil, err
	}
	if response.Filesystems == nil {
		return []platform.Filesystem{}, nil
	}
	return response.Filesystems, nil
}

func ReadNetworkSnapshot(ctx context.Context, path string, credentials HostReadCredentials) (platform.NetworkSnapshot, error) {
	response, err := readHostResponse(ctx, path, credentials, "network.read", &NetworkReadOperation{}, 20*time.Second, 4<<20)
	if err != nil {
		return platform.NetworkSnapshot{}, err
	}
	if response.NetworkSnapshot == nil {
		return platform.NetworkSnapshot{}, ErrServiceUnavailable
	}
	return *response.NetworkSnapshot, nil
}

func ReadUpdateStatus(ctx context.Context, path string, credentials HostReadCredentials) (platform.UpdateStatus, error) {
	response, err := readHostResponse(ctx, path, credentials, "updates.status", &UpdateReadOperation{}, 30*time.Second, 8<<20)
	if err != nil {
		return platform.UpdateStatus{}, err
	}
	if response.UpdateStatus == nil {
		return platform.UpdateStatus{}, ErrServiceUnavailable
	}
	return *response.UpdateStatus, nil
}

func PreviewUpdates(ctx context.Context, path string, credentials HostReadCredentials, operation platform.UpdateOperation) (platform.UpdatePreview, error) {
	response, err := readHostResponse(ctx, path, credentials, "updates.preview", &UpdatePreviewOperation{Operation: operation}, 2*time.Minute, 8<<20)
	if err != nil {
		return platform.UpdatePreview{}, err
	}
	if response.UpdatePreview == nil {
		return platform.UpdatePreview{}, ErrServiceUnavailable
	}
	return *response.UpdatePreview, nil
}

func RefreshUpdates(ctx context.Context, path string, adminToken string, force bool) (platform.UpdateStatus, error) {
	operation := UpdateRefreshOperation{Force: force}
	response, err := readHostResponse(ctx, path, HostReadCredentials{AdminToken: adminToken}, "updates.refresh", &operation, 5*time.Minute, 8<<20)
	if err != nil {
		return platform.UpdateStatus{}, err
	}
	if response.UpdateStatus == nil {
		return platform.UpdateStatus{}, ErrServiceUnavailable
	}
	return *response.UpdateStatus, nil
}

func ReadUpdateHistory(ctx context.Context, path string, credentials HostReadCredentials) ([]platform.UpdateHistoryEntry, error) {
	response, err := readHostResponse(ctx, path, credentials, "updates.history", &UpdateHistoryReadOperation{}, 30*time.Second, 2<<20)
	if err != nil {
		return nil, err
	}
	if response.UpdateHistory == nil {
		return []platform.UpdateHistoryEntry{}, nil
	}
	return response.UpdateHistory, nil
}

func ReadUpdateObservation(ctx context.Context, path string, credentials HostReadCredentials) (UpdateObservation, error) {
	response, err := readHostResponse(ctx, path, credentials, "updates.live", &UpdateLiveReadOperation{}, 10*time.Second, 2<<20)
	if err != nil {
		return UpdateObservation{}, err
	}
	if response.UpdateObservation == nil {
		return UpdateObservation{}, ErrServiceUnavailable
	}
	return *response.UpdateObservation, nil
}

func ReadKpatch(ctx context.Context, path string, credentials HostReadCredentials) (platform.KpatchStatus, platform.KpatchSettingsStatus, error) {
	response, err := readHostResponse(ctx, path, credentials, "updates.kpatch.read", &KpatchReadOperation{}, 30*time.Second, 512<<10)
	if err != nil {
		return platform.KpatchStatus{}, platform.KpatchSettingsStatus{}, err
	}
	if response.KpatchStatus == nil || response.KpatchSettings == nil {
		return platform.KpatchStatus{}, platform.KpatchSettingsStatus{}, ErrServiceUnavailable
	}
	return *response.KpatchStatus, *response.KpatchSettings, nil
}

func ReadCapabilities(ctx context.Context, path string, credentials HostReadCredentials) ([]platform.Capability, error) {
	response, err := readHostResponse(ctx, path, credentials, "capabilities.read", &CapabilitiesReadOperation{}, 30*time.Second, 2<<20)
	if err != nil {
		return nil, err
	}
	if response.Capabilities == nil {
		return []platform.Capability{}, nil
	}
	return response.Capabilities, nil
}

func ReadLoginHistory(ctx context.Context, path string, credentials HostReadCredentials, query platform.LoginHistoryQuery) (platform.LoginHistoryPage, error) {
	operation := LoginHistoryReadOperation{Query: query}
	response, err := readHostResponse(ctx, path, credentials, "login-history.read", &operation, 30*time.Second, 2<<20)
	if err != nil {
		return platform.LoginHistoryPage{}, err
	}
	if response.LoginHistoryPage == nil {
		return platform.LoginHistoryPage{}, ErrServiceUnavailable
	}
	return *response.LoginHistoryPage, nil
}

func PreviewLocalGroup(ctx context.Context, path, adminToken string, operation platform.LocalGroupOperation) (platform.LocalGroupPreview, error) {
	operation.Preview = true
	response, err := readHostResponse(ctx, path, HostReadCredentials{AdminToken: adminToken}, "local-group.preview", &operation, 30*time.Second, 512<<10)
	if err != nil {
		return platform.LocalGroupPreview{}, err
	}
	if response.LocalGroupPreview == nil {
		return platform.LocalGroupPreview{}, ErrServiceUnavailable
	}
	return *response.LocalGroupPreview, nil
}

func ApplyLocalGroup(ctx context.Context, path, adminToken string, operation platform.LocalGroupOperation) (platform.LocalGroupState, error) {
	operation.Preview = false
	response, err := readHostResponse(ctx, path, HostReadCredentials{AdminToken: adminToken}, "local-group.apply", &operation, 30*time.Second, 512<<10)
	if err != nil {
		return platform.LocalGroupState{}, err
	}
	if response.LocalGroupState == nil {
		return platform.LocalGroupState{}, ErrServiceUnavailable
	}
	return *response.LocalGroupState, nil
}

func ReadMetricHistory(ctx context.Context, path string, credentials HostReadCredentials, since time.Time, maximum int) ([]metrics.Sample, error) {
	operation := MetricsHistoryOperation{Since: since, Maximum: maximum}
	response, err := readHostResponse(ctx, path, credentials, "metrics.history", &operation, 20*time.Second, 8<<20)
	if err != nil {
		return nil, err
	}
	if response.MetricSamples == nil {
		return []metrics.Sample{}, nil
	}
	return response.MetricSamples, nil
}

func FollowMetrics(ctx context.Context, path string, credentials HostReadCredentials, interval time.Duration, emit func(metrics.Sample) error) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return normalizeHostTransportError(ctx, err)
	}
	defer conn.Close()
	stopCancelWatch := make(chan struct{})
	defer close(stopCancelWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCancelWatch:
		}
	}()
	_ = conn.SetDeadline(time.Time{})
	request := Request{Operation: "metrics.follow", Token: credentials.Token, AdminToken: credentials.AdminToken, MetricsFollow: &MetricsFollowOperation{Interval: interval}}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return normalizeHostTransportError(ctx, err)
	}
	decoder := json.NewDecoder(conn)
	for {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return normalizeHostTransportError(ctx, err)
		}
		if response.Error != "" {
			return hostResponseError(response.Error)
		}
		if response.MetricSample == nil {
			continue
		}
		if err := emit(*response.MetricSample); err != nil {
			return nil
		}
	}
}
