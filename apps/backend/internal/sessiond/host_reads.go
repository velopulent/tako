package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

// userHostReader is implemented by the per-login bridge. Keeping this
// interface separate from the root adapters makes it impossible for a user
// operation to accidentally fall through to a root systemctl or D-Bus call.
type userHostReader interface {
	readServices(context.Context, auth.ServiceReadOperation) ([]platform.Unit, error)
	readUnitDetails(context.Context, auth.ServiceReadOperation) (platform.UnitDetail, error)
	readUnitConfiguration(context.Context, auth.ServiceReadOperation) (platform.UnitConfiguration, error)
	serviceAction(context.Context, platform.ServiceOperation) error
	readHostInfo(context.Context) (host.Info, error)
	readHostConfiguration(context.Context) (platform.HostConfiguration, error)
	readPowerStatus(context.Context) (platform.PowerStatus, error)
	previewProcessSignal(context.Context, platform.SignalOperation) (platform.SignalPreview, error)
	signalProcesses(context.Context, platform.SignalOperation) (platform.SignalResult, error)
	readIdentityInventory(context.Context) (platform.IdentityInventory, error)
	readFilesystems(context.Context) ([]platform.Filesystem, error)
	readNetworkSnapshot(context.Context) (platform.NetworkSnapshot, error)
	readUpdateStatus(context.Context) (platform.UpdateStatus, error)
	readUpdateHistory(context.Context) ([]platform.UpdateHistoryEntry, error)
	readUpdateObservation(context.Context) (auth.UpdateObservation, error)
	readAutoUpdatesStatus(context.Context) (platform.AutoUpdatesConfig, error)
	readKpatch(context.Context) (platform.KpatchStatus, platform.KpatchSettingsStatus, error)
	readCapabilities(context.Context) ([]platform.Capability, error)
	readLoginHistory(context.Context, auth.LoginHistoryReadOperation) (platform.LoginHistoryPage, error)
}

type hostDispatchResult struct {
	response auth.Response
	lane     string
}

type userServiceActioner interface {
	serviceAction(context.Context, platform.ServiceOperation) error
}

func handleHostOperation(conn net.Conn, encoder *json.Encoder, request auth.Request, grants *grantStore, runtime *hostRuntime, logger *zap.Logger) bool {
	if !isHostOperation(request.Operation) {
		return false
	}
	if !validHostRequest(request) {
		logger.Info("host operation rejected",
			zap.String("operation", request.Operation),
			zap.String("module", hostOperationModule(request.Operation)),
			zap.String("scope", hostOperationScope(request)),
			zap.String("type", request.Operation),
			zap.String("lane", "unresolved"),
			zap.String("error_code", "invalid-request"),
		)
		_ = encoder.Encode(auth.Response{Error: "invalid-request"})
		return true
	}

	if runtime == nil {
		runtime = newHostRuntime(hostMetricInterval, hostMetricRetention)
		runtimeContext, cancel := context.WithCancel(context.Background())
		go runtime.run(runtimeContext)
		defer func() {
			cancel()
			runtime.close()
		}()
	}

	if request.Operation == "metrics.follow" {
		handleMetricsFollow(conn, encoder, request, grants, runtime, logger)
		return true
	}

	operationContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := dispatchHostOperation(operationContext, request, grants, runtime)
	if err != nil {
		code := hostOperationErrorCode(err)
		logger.Info("host operation completed",
			zap.String("operation", request.Operation),
			zap.String("module", hostOperationModule(request.Operation)),
			zap.String("scope", hostOperationScope(request)),
			zap.String("type", request.Operation),
			zap.String("lane", result.lane),
			zap.String("error_code", code),
		)
		_ = encoder.Encode(auth.Response{Error: code})
		return true
	}
	logger.Info("host operation completed",
		zap.String("operation", request.Operation),
		zap.String("module", hostOperationModule(request.Operation)),
		zap.String("scope", hostOperationScope(request)),
		zap.String("type", request.Operation),
		zap.String("lane", result.lane),
		zap.String("error_code", ""),
	)
	_ = encoder.Encode(result.response)
	return true
}

func isHostOperation(operation string) bool {
	switch operation {
	case "services.read", "services.list", "services.detail", "services.configuration", "services.action", "host.info", "host.configuration.read", "certificate.read", "host.power.read", "processes.list", "processes.detail", "processes.signal-preview", "processes.signal", "identities.read", "storage.read", "network.read", "updates.read", "updates.status", "updates.refresh", "updates.history", "updates.live", "updates.cancel", "updates.automatic.read", "updates.kpatch.read", "capabilities.read", "login-history.read", "local-group.preview", "local-group.apply", "metrics.history", "metrics.follow":
		return true
	default:
		return false
	}
}

func validHostRequest(request auth.Request) bool {
	var payloads int
	switch request.Operation {
	case "services.read", "services.list", "services.detail", "services.configuration":
		if request.ServiceRead != nil {
			payloads++
		}
	case "services.action":
		if request.ServiceAction != nil {
			payloads++
		}
	case "host.info":
		if request.HostInfoRead != nil {
			payloads++
		}
	case "host.configuration.read":
		if request.HostConfigRead != nil {
			payloads++
		}
	case "certificate.read":
		if request.CertificateRead != nil {
			payloads++
		}
	case "host.power.read":
		if request.PowerRead != nil {
			payloads++
		}
	case "processes.list", "processes.detail":
		if request.ProcessRead != nil {
			payloads++
		}
	case "processes.signal-preview":
		if request.SignalPreview != nil {
			payloads++
		}
	case "processes.signal":
		if request.SignalApply != nil {
			payloads++
		}
	case "identities.read":
		if request.IdentityRead != nil {
			payloads++
		}
	case "storage.read":
		if request.StorageRead != nil {
			payloads++
		}
	case "network.read":
		if request.NetworkRead != nil {
			payloads++
		}
	case "updates.read", "updates.status":
		if request.UpdateRead != nil {
			payloads++
		}
	case "updates.refresh":
		if request.UpdateRefresh != nil {
			payloads++
		}
	case "updates.history":
		if request.UpdateHistoryRead != nil {
			payloads++
		}
	case "updates.live":
		if request.UpdateLiveRead != nil {
			payloads++
		}
	case "updates.cancel":
		if request.UpdateCancel != nil {
			payloads++
		}
	case "updates.automatic.read":
		if request.AutoUpdatesRead != nil {
			payloads++
		}
	case "updates.kpatch.read":
		if request.KpatchRead != nil {
			payloads++
		}
	case "capabilities.read":
		if request.CapabilitiesRead != nil {
			payloads++
		}
	case "login-history.read":
		if request.LoginHistoryRead != nil {
			payloads++
		}
	case "local-group.preview", "local-group.apply":
		if request.LocalGroup != nil {
			payloads++
		}
	case "metrics.history":
		if request.MetricsHistory != nil {
			payloads++
		}
	case "metrics.follow":
		if request.MetricsFollow != nil {
			payloads++
		}
	}
	if request.AdminTTL != 0 || request.Token != "" && request.AdminToken != "" {
		return false
	}
	totalTypedPayloads := 0
	for _, present := range []bool{
		request.ServiceRead != nil, request.HostInfoRead != nil, request.HostConfigRead != nil,
		request.ServiceAction != nil,
		request.CertificateRead != nil, request.PowerRead != nil, request.ProcessRead != nil,
		request.IdentityRead != nil, request.StorageRead != nil, request.NetworkRead != nil,
		request.UpdateRead != nil, request.UpdateRefresh != nil, request.UpdateHistoryRead != nil,
		request.UpdateLiveRead != nil, request.UpdateCancel != nil, request.AutoUpdatesRead != nil,
		request.KpatchRead != nil, request.CapabilitiesRead != nil, request.LoginHistoryRead != nil,
		request.SignalPreview != nil, request.SignalApply != nil, request.LocalGroup != nil, request.MetricsHistory != nil,
		request.MetricsFollow != nil,
	} {
		if present {
			totalTypedPayloads++
		}
	}
	if totalTypedPayloads != 1 || totalTypedPayloads != payloads {
		return false
	}
	return request.Username == "" && request.Password == "" && request.ConversationID == "" && len(request.Responses) == 0 && request.Columns == 0 && request.Rows == 0 && request.Action == "" && request.Unit == "" && request.Scope == "" && request.Hostname == "" && request.Timezone == "" && !request.NTPEnabled && request.ExpectedFingerprint == "" && request.PowerAction == "" && request.PowerConfirmation == "" && request.Timer == nil && request.Override == nil && request.Signal == nil && request.Account == nil && request.GroupMembership == nil && request.AdminRole == nil && request.PasswordChange == nil && request.SSHKeys == nil && request.Updates == nil && request.AutoUpdates == nil && request.Kpatch == nil && request.File == nil && request.Journal == nil && request.Network == nil && request.Firewall == nil && request.Security == nil && request.SupportReport == nil
}

func dispatchHostOperation(ctx context.Context, request auth.Request, grants *grantStore, runtime *hostRuntime) (hostDispatchResult, error) {
	result := hostDispatchResult{lane: "unresolved"}
	if request.Operation == "certificate.read" {
		result.lane = "root-sessiond"
		if _, _, err := authenticatedHostIdentity(request, grants); err != nil {
			return result, err
		}
		status, err := platform.InspectCertificate(runtime.certificatePath)
		result.response.CertificateStatus = &status
		return result, err
	}
	// Process inventory and inspection are read-only host observations. They
	// belong to sessiond's shared root-owned collector, not to the per-login
	// bridge. A valid ordinary session is sufficient; administrative elevation
	// is reserved for process mutations and other privileged operations.
	if request.Operation == "processes.list" || request.Operation == "processes.detail" {
		if _, _, err := authenticatedHostIdentity(request, grants); err != nil {
			return result, err
		}
		result.lane = "root-sessiond"
		if runtime.processTracker == nil {
			runtime.processTracker = platform.NewProcessTracker()
		}
		var err error
		if request.Operation == "processes.list" {
			result.response.Processes, err = runtime.processTracker.Snapshot()
		} else {
			operation := *request.ProcessRead
			item, inspectErr := runtime.processTracker.Inspect(ctx, operation.PID, operation.Started)
			result.response.ProcessDetails = &item
			err = inspectErr
		}
		if err != nil {
			return result, err
		}
		return result, nil
	}
	identity, reader, administrative, err := hostGrant(request, grants)
	_ = identity
	if err != nil {
		return result, err
	}
	if administrative {
		result.lane = "root-sessiond"
	} else {
		result.lane = "user-bridge"
	}

	switch request.Operation {
	case "services.read", "services.list":
		operation := *request.ServiceRead
		if err := validateServiceRead(operation); err != nil {
			return result, err
		}
		if operation.Scope == "user" && administrative {
			return result, errors.New("permission-denied")
		}
		var items []platform.Unit
		if administrative {
			items, err = platform.Units(ctx, operation.Scope, operation.Type)
		} else {
			items, err = reader.readServices(ctx, operation)
		}
		result.response.Units = items
	case "services.action":
		operation := *request.ServiceAction
		if _, operationErr := platform.ParseServiceOperation(operation.Scope, operation.Unit, operation.Action); operationErr != nil {
			return result, operationErr
		}
		if operation.Scope == "user" {
			if administrative {
				return result, errors.New("permission-denied")
			}
			err = reader.serviceAction(ctx, operation)
			break
		}
		if !administrative {
			return result, errors.New("permission-denied")
		}
		err = systemServiceBackend{}.Run(ctx, operation)
	case "services.detail":
		operation := *request.ServiceRead
		if err := platform.ValidateServiceTarget(operation.Scope, operation.Unit); err != nil {
			return result, err
		}
		if operation.Scope == "user" && administrative {
			return result, errors.New("permission-denied")
		}
		var item platform.UnitDetail
		if administrative {
			item, err = platform.UnitDetails(ctx, operation.Scope, operation.Unit)
		} else {
			item, err = reader.readUnitDetails(ctx, operation)
		}
		result.response.UnitDetail = &item
	case "services.configuration":
		operation := *request.ServiceRead
		if err := platform.ValidateServiceTarget(operation.Scope, operation.Unit); err != nil {
			return result, err
		}
		if operation.Scope == "user" && administrative {
			return result, errors.New("permission-denied")
		}
		var item platform.UnitConfiguration
		if administrative {
			item, err = platform.ReadUnitConfiguration(ctx, operation.Scope, operation.Unit)
		} else {
			item, err = reader.readUnitConfiguration(ctx, operation)
		}
		result.response.UnitConfiguration = &item
	case "host.info":
		var item host.Info
		if administrative {
			item = host.ReadContext(ctx)
		} else {
			item, err = reader.readHostInfo(ctx)
		}
		result.response.HostInfo = &item
	case "host.configuration.read":
		var item platform.HostConfiguration
		if administrative {
			item, err = platform.ReadHostConfiguration(ctx)
		} else {
			item, err = reader.readHostConfiguration(ctx)
		}
		result.response.HostConfiguration = &item
	case "host.power.read":
		var item platform.PowerStatus
		if administrative {
			item, err = platform.ReadPowerStatus(ctx)
		} else {
			item, err = reader.readPowerStatus(ctx)
		}
		result.response.PowerStatus = &item
	case "processes.signal-preview":
		operation := *request.SignalPreview
		var item platform.SignalPreview
		if administrative {
			item, err = previewSignal(ctx, operation)
		} else {
			item, err = reader.previewProcessSignal(ctx, operation)
		}
		result.response.SignalPreview = &item
	case "processes.signal":
		if administrative {
			return result, errors.New("permission-denied")
		}
		operation := *request.SignalApply
		item, signalErr := reader.signalProcesses(ctx, operation)
		result.response.SignalResult = &item
		err = signalErr
	case "identities.read":
		var item platform.IdentityInventory
		if administrative {
			item, err = platform.ListIdentityInventory(ctx)
		} else {
			item, err = reader.readIdentityInventory(ctx)
		}
		result.response.IdentityInventory = &item
	case "storage.read":
		if administrative {
			result.response.Filesystems, err = platform.Filesystems()
		} else {
			result.response.Filesystems, err = reader.readFilesystems(ctx)
		}
	case "network.read":
		var item platform.NetworkSnapshot
		if administrative {
			item, err = platform.NetworkSnapshotRead(ctx)
		} else {
			item, err = reader.readNetworkSnapshot(ctx)
		}
		result.response.NetworkSnapshot = &item
	case "updates.read":
		if administrative {
			status := platform.Updates(ctx)
			result.response.UpdateStatus = &status
		} else {
			status, readErr := reader.readUpdateStatus(ctx)
			result.response.UpdateStatus, err = &status, readErr
		}
	case "updates.refresh":
		if !administrative {
			return result, errors.New("permission-denied")
		}
		operation := *request.UpdateRefresh
		err = platform.RefreshUpdatesCache(ctx, operation.Force)
		if err == nil {
			status := platform.Updates(ctx)
			result.response.UpdateStatus = &status
		}
	case "updates.history":
		if administrative {
			result.response.UpdateHistory, err = platform.UpdateHistory(ctx)
		} else {
			result.response.UpdateHistory, err = reader.readUpdateHistory(ctx)
		}
	case "updates.live":
		// PackageKit observation and the transaction watcher are sessiond-owned
		// even for an ordinary authenticated reader. This avoids one D-Bus
		// client per HTTP request and gives all clients the same live view.
		result.response.UpdateObservation = observationPointer(runtime.updateObservation(ctx))
	case "updates.cancel":
		if !administrative {
			return result, errors.New("permission-denied")
		}
		found, cancelErr := cancelUpdate(ctx, runtime)
		err = cancelErr
		result.response.UpdateCanceled = &found
	case "updates.automatic.read":
		if administrative {
			status := platform.AutoUpdatesStatus(ctx)
			result.response.AutoUpdatesConfig = &status
		} else {
			status, readErr := reader.readAutoUpdatesStatus(ctx)
			result.response.AutoUpdatesConfig, err = &status, readErr
		}
	case "updates.kpatch.read":
		if administrative {
			status := platform.InspectKpatchStatus(ctx)
			settings := platform.InspectKpatchSettings(ctx)
			result.response.KpatchStatus = &status
			result.response.KpatchSettings = &settings
		} else {
			status, settings, readErr := reader.readKpatch(ctx)
			result.response.KpatchStatus = &status
			result.response.KpatchSettings = &settings
			err = readErr
		}
	case "capabilities.read":
		if administrative {
			result.response.Capabilities = platform.Detect(ctx)
		} else {
			result.response.Capabilities, err = reader.readCapabilities(ctx)
		}
	case "login-history.read":
		operation := *request.LoginHistoryRead
		if !administrative && operation.Query.Username != identity.Username {
			return result, errors.New("permission-denied")
		}
		var item platform.LoginHistoryPage
		if administrative {
			item, err = platform.QueryLoginHistory(ctx, operation.Query)
		} else {
			item, err = reader.readLoginHistory(ctx, operation)
		}
		result.response.LoginHistoryPage = &item
	case "local-group.preview", "local-group.apply":
		if !administrative {
			return result, errors.New("permission-denied")
		}
		operation := *request.LocalGroup
		if request.Operation == "local-group.preview" {
			operation.Preview = true
			item, previewErr := platform.PreviewLocalGroup(ctx, operation)
			result.response.LocalGroupPreview, err = &item, previewErr
		} else {
			operation.Preview = false
			item, applyErr := platform.ApplyLocalGroup(ctx, operation)
			result.response.LocalGroupState, err = &item, applyErr
		}
	case "metrics.history":
		operation := *request.MetricsHistory
		if operation.Maximum < 0 || operation.Maximum > 10000 {
			return result, errors.New("bounded-output")
		}
		if operation.Since.IsZero() {
			operation.Since = time.Now().Add(-24 * time.Hour)
		}
		result.response.MetricSamples = runtime.sampler.HistorySince(operation.Since, operation.Maximum)
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func hostGrant(request auth.Request, grants *grantStore) (auth.Identity, userHostReader, bool, error) {
	identity, administrative, err := authenticatedHostIdentity(request, grants)
	if err != nil {
		return auth.Identity{}, nil, false, err
	}
	if administrative {
		return identity, nil, true, nil
	}

	bridge, ok := grants.bridgeFor(request.Token)
	if !ok {
		return identity, nil, false, errors.New("user-manager-unavailable")
	}
	reader, ok := bridge.(userHostReader)
	if !ok {
		return identity, nil, false, errors.New("user-manager-unavailable")
	}
	return identity, reader, false, nil
}

func authenticatedHostIdentity(request auth.Request, grants *grantStore) (auth.Identity, bool, error) {
	if request.AdminToken != "" {
		identity, ok := grants.adminIdentity(request.AdminToken)
		if !ok {
			return auth.Identity{}, false, errors.New("invalid-admin-token")
		}
		return identity, true, nil
	}
	if request.Token == "" {
		return auth.Identity{}, false, errors.New("invalid-bridge-token")
	}
	identity, ok := grants.get(request.Token)
	if !ok {
		return auth.Identity{}, false, errors.New("invalid-bridge-token")
	}
	return identity, false, nil
}

func validateServiceRead(operation auth.ServiceReadOperation) error {
	if operation.Scope != "system" && operation.Scope != "user" {
		return platform.ErrInvalidServiceScope
	}
	if operation.Type != "" && operation.Type != "service" && operation.Type != "target" && operation.Type != "socket" && operation.Type != "timer" && operation.Type != "path" {
		return platform.ErrInvalidServiceUnit
	}
	if operation.Unit != "" {
		return platform.ValidateServiceTarget(operation.Scope, operation.Unit)
	}
	return nil
}

func previewSignal(ctx context.Context, operation platform.SignalOperation) (platform.SignalPreview, error) {
	return platform.PreviewSignal(ctx, operation)
}

func observationPointer(item auth.UpdateObservation) *auth.UpdateObservation {
	return &item
}

func runUserServiceAction(backend userServiceActioner, operation platform.ServiceOperation) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := backend.serviceAction(ctx, operation); err != nil {
		if ctx.Err() != nil {
			return "service-job-timeout"
		}
		return hostOperationErrorCode(err)
	}
	return ""
}

func cancelUpdate(ctx context.Context, runtime *hostRuntime) (bool, error) {
	if client := runtime.updateClientFor(ctx); client != nil {
		cancelContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return client.CancelActiveUpdate(cancelContext)
	}
	return false, platform.ErrUpdateUnavailable
}

func handleMetricsFollow(conn net.Conn, encoder *json.Encoder, request auth.Request, grants *grantStore, runtime *hostRuntime, logger *zap.Logger) {
	lane := hostOperationRequestedLane(request)
	_, _, _, err := hostGrant(request, grants)
	if err != nil {
		logHostOperation(logger, request, lane, hostOperationErrorCode(err))
		_ = encoder.Encode(auth.Response{Error: hostOperationErrorCode(err)})
		return
	}
	interval := request.MetricsFollow.Interval
	if interval == 0 {
		interval = hostMetricInterval
	}
	if interval < time.Second || interval > 5*time.Minute {
		logHostOperation(logger, request, lane, "bounded-output")
		_ = encoder.Encode(auth.Response{Error: "bounded-output"})
		return
	}
	if runtime == nil || runtime.sampler == nil {
		logHostOperation(logger, request, lane, "system-backend-unavailable")
		_ = encoder.Encode(auth.Response{Error: "system-backend-unavailable"})
		return
	}
	_ = conn.SetDeadline(time.Time{})
	stream, unsubscribe := runtime.sampler.SubscribeEvery(interval)
	defer unsubscribe()
	expires := time.NewTimer(15 * time.Minute)
	defer expires.Stop()
	for {
		select {
		case sample, ok := <-stream:
			if !ok {
				logHostOperation(logger, request, lane, "")
				return
			}
			if err := encoder.Encode(auth.Response{MetricSample: &sample}); err != nil {
				logHostOperation(logger, request, lane, hostOperationErrorCode(err))
				return
			}
		case <-expires.C:
			logHostOperation(logger, request, lane, "timeout")
			return
		}
	}
}

func logHostOperation(logger *zap.Logger, request auth.Request, lane, errorCode string) {
	logger.Info("host operation completed",
		zap.String("operation", request.Operation),
		zap.String("module", hostOperationModule(request.Operation)),
		zap.String("scope", hostOperationScope(request)),
		zap.String("type", request.Operation),
		zap.String("lane", lane),
		zap.String("error_code", errorCode),
	)
}

func hostOperationRequestedLane(request auth.Request) string {
	switch {
	case request.AdminToken != "":
		return "root-sessiond"
	case request.Token != "":
		return "user-bridge"
	default:
		return "unresolved"
	}
}

func hostOperationErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if code := err.Error(); isStableHostError(code) {
		return code
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, platform.ErrInvalidServiceScope), errors.Is(err, platform.ErrInvalidServiceUnit), errors.Is(err, platform.ErrInvalidServiceAction):
		return "invalid-service-operation"
	case errors.Is(err, platform.ErrInvalidSignalOperation):
		return "invalid-signal-operation"
	case errors.Is(err, platform.ErrSignalUnauthorized):
		return "permission-denied"
	case errors.Is(err, platform.ErrProcessNotFound):
		return "process-not-found"
	case errors.Is(err, platform.ErrProcessReused):
		return "process-reused"
	case errors.Is(err, platform.ErrInvalidLoginHistoryQuery):
		return "invalid-login-history-query"
	case errors.Is(err, platform.ErrUpdateUnavailable):
		return "system-backend-unavailable"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "permission denied") || strings.Contains(message, "access denied") {
		return "permission-denied"
	}
	if strings.Contains(message, "limit") || strings.Contains(message, "bound") {
		return "bounded-output"
	}
	if strings.Contains(message, "conflict") || strings.Contains(message, "changed") {
		return "conflict"
	}
	return "system-backend-unavailable"
}

func isStableHostError(code string) bool {
	switch code {
	case "invalid-bridge-token", "invalid-admin-token", "user-manager-unavailable", "permission-denied", "system-backend-unavailable", "timeout", "bounded-output", "conflict", "invalid-service-operation", "invalid-signal-operation", "process-not-found", "process-reused", "invalid-login-history-query":
		return true
	default:
		return false
	}
}

func hostOperationModule(operation string) string {
	if index := strings.IndexByte(operation, '.'); index > 0 {
		return operation[:index]
	}
	return operation
}

func hostOperationScope(request auth.Request) string {
	if request.ServiceRead != nil && request.ServiceRead.Scope != "" {
		return request.ServiceRead.Scope
	}
	if request.ServiceAction != nil && request.ServiceAction.Scope != "" {
		return request.ServiceAction.Scope
	}
	if request.AdminToken != "" {
		return "system"
	}
	if request.Token != "" {
		return "user"
	}
	return "unresolved"
}
