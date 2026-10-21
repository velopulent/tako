package sessiond

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/host"
	"github.com/velopulent/tako/internal/metrics"
	"github.com/velopulent/tako/internal/platform"
	"go.uber.org/zap"
)

// operations is the complete sessiond surface. Every entry names the
// credential lane it accepts; anything not listed is rejected.
var operations map[string]operation

func init() {
	operations = map[string]operation{
		"session.conversation": {lane: laneNone, timeout: time.Minute, fallback: "authentication-failed", run: runConversation},
		"session.cancel":       {lane: laneNone, run: runCancelConversation},
		"session.confirm":      {lane: laneSession, fallback: "invalid-bridge-token", run: runConfirmSession},
		"session.close":        {lane: laneSession, fallback: "invalid-bridge-token", run: runCloseSession},
		"admin.authorize":      {lane: laneSession, timeout: 15 * time.Second, fallback: "administrative-access-denied", run: runAuthorizeAdmin},
		"admin.revoke":         {lane: laneAdminToken, fallback: "invalid-admin-token", run: runRevokeAdmin},
		"terminal.open":        {lane: laneSession, takeover: openTerminal},
		"terminal.resize":      {lane: laneSession, fallback: "terminal-resize-failed", run: runResizeTerminal},
		"tls.certificate":      {lane: laneNone, fallback: "certificate-unavailable", run: runCertificate},

		"services.list":          {lane: laneEither, fallback: "system-backend-unavailable", run: runServicesList},
		"services.detail":        {lane: laneEither, fallback: "system-backend-unavailable", run: runServiceDetail},
		"services.configuration": {lane: laneEither, fallback: "system-backend-unavailable", run: runServiceConfiguration},
		"services.action":        {lane: laneEither, timeout: 20 * time.Second, fallback: "service-action-failed", run: runServiceAction},
		"timers.apply":           {lane: laneEither, timeout: 20 * time.Second, fallback: "timer-operation-failed", run: runTimer},
		"overrides.apply":        {lane: laneEither, timeout: 20 * time.Second, fallback: "service-override-failed", run: runOverride},

		"host.info":          {lane: laneEither, fallback: "system-backend-unavailable", run: runHostInfo},
		"host.configuration": {lane: laneEither, fallback: "host-config-unavailable", run: runHostConfiguration},
		"host.configure":     {lane: laneAdmin, timeout: 20 * time.Second, fallback: "host-config-failed", run: runHostConfigure},
		"power.status":       {lane: laneEither, fallback: "power-unavailable", run: runPowerStatus},
		"power.request":      {lane: laneAdmin, timeout: 15 * time.Second, fallback: "power-request-failed", run: runPowerRequest},
		"capabilities.read":  {lane: laneEither, fallback: "system-backend-unavailable", run: runCapabilities},

		"processes.list":           {lane: laneEither, fallback: "system-backend-unavailable", run: runProcessList},
		"processes.detail":         {lane: laneEither, fallback: "system-backend-unavailable", run: runProcessDetail},
		"processes.signal-preview": {lane: laneEither, fallback: "process-signal-failed", run: runSignalPreview},
		"processes.signal":         {lane: laneEither, timeout: 10 * time.Second, fallback: "process-signal-failed", run: runSignal},

		"identities.list":    {lane: laneEither, fallback: "system-backend-unavailable", run: runIdentities},
		"accounts.local":     {lane: laneAdmin, timeout: 20 * time.Second, fallback: "local-account-unavailable", run: runLocalAccount},
		"groups.membership":  {lane: laneAdmin, timeout: 20 * time.Second, fallback: "group-unavailable", run: runGroupMembership},
		"groups.admin-role":  {lane: laneAdmin, timeout: 20 * time.Second, fallback: "group-unavailable", run: runAdministrativeRole},
		"groups.local":       {lane: laneAdmin, timeout: 30 * time.Second, fallback: "local-group-unavailable", run: runLocalGroup},
		"passwords.change":   {lane: laneEither, timeout: 20 * time.Second, fallback: "password-unavailable", run: runPassword},
		"ssh-keys.apply":     {lane: laneEither, timeout: 20 * time.Second, fallback: "ssh-key-unavailable", run: runSSHKeys},
		"login-history.read": {lane: laneEither, fallback: "login-history-unavailable", run: runLoginHistory},

		"storage.read":     {lane: laneEither, fallback: "storage-unavailable", run: runStorageRead},
		"storage.apply":    {lane: laneAdmin, timeout: 2 * time.Minute, fallback: "storage-operation-failed", run: runStorageApply},
		"network.read":     {lane: laneEither, fallback: "network-unavailable", run: runNetworkRead},
		"network.apply":    {lane: laneAdmin, timeout: 2 * time.Minute, fallback: "network-operation-failed", run: runNetworkApply},
		"firewall.apply":   {lane: laneAdmin, timeout: 2 * time.Minute, fallback: "firewall-operation-failed", run: runFirewallApply},
		"security.preview": {lane: laneAdmin, fallback: "security-operation-failed", run: runSecurityPreview},
		"security.apply":   {lane: laneAdmin, timeout: 2 * time.Minute, fallback: "security-operation-failed", run: runSecurityApply},

		"updates.status":  {lane: laneEither, fallback: "updates-unavailable", run: runUpdateStatus},
		"updates.preview": {lane: laneEither, timeout: 2 * time.Minute, fallback: "updates-unavailable", run: runUpdatePreview},
		"updates.refresh": {lane: laneAdmin, timeout: 5 * time.Minute, fallback: "updates-unavailable", run: runUpdateRefresh},
		"updates.history": {lane: laneEither, fallback: "updates-unavailable", run: runUpdateHistory},
		"updates.live":    {lane: laneEither, fallback: "updates-unavailable", run: runUpdateLive},
		"updates.apply":   {lane: laneAdmin, timeout: 30 * time.Minute, fallback: "updates-unavailable", run: runUpdateApply},
		"kpatch.read":     {lane: laneEither, fallback: "updates-unavailable", run: runKpatchRead},
		"kpatch.apply":    {lane: laneAdmin, timeout: 5 * time.Minute, fallback: "kpatch-apply-failed", run: runKpatchApply},

		"files.apply":     {lane: laneEither, timeout: 2 * time.Minute, fallback: "file-operation-failed", run: runFiles},
		"journal.query":   {lane: laneEither, fallback: "logs-unavailable", run: runJournalQuery},
		"journal.follow":  {lane: laneEither, takeover: followJournal},
		"metrics.history": {lane: laneEither, fallback: "system-backend-unavailable", run: runMetricsHistory},
		"metrics.follow":  {lane: laneEither, takeover: followMetrics},
	}
}

var errPermissionDenied = auth.Code("permission-denied")

// Login and administrative grants.

func runConversation(ctx context.Context, c *call) (any, error) {
	var request auth.ConversationRequest
	err := c.decode(&request)
	defer clearConversation(&request)
	if err != nil || request.Cancel || !request.Valid() {
		return nil, auth.Code("invalid-conversation")
	}
	username := request.Username
	response, err := c.handler.conversations.advance(ctx, request)
	if err != nil {
		code := "authentication-failed"
		switch {
		case errors.Is(err, errConversationBusy):
			code = "authentication-busy"
		case errors.Is(err, errConversationInvalid), errors.Is(err, errConversationBounds):
			code = "invalid-conversation"
		case errors.Is(err, errSessionFailed):
			code = "session-failed"
		}
		// Keep the client response generic; the underlying PAM or bridge
		// failure stays in the privileged journal. Secrets are already cleared.
		c.handler.logger.Warn("authentication conversation failed", zap.String("username", username), zap.String("reason", code), zap.Error(err))
		return nil, auth.Code(code)
	}
	if response.Identity != nil {
		c.handler.logger.Info("user session opened", zap.String("username", response.Identity.Username), zap.Int("uid", response.Identity.UID))
	}
	return response, nil
}

func clearConversation(request *auth.ConversationRequest) {
	request.Password = ""
	for index := range request.Responses {
		request.Responses[index].Value = ""
	}
}

func runCancelConversation(_ context.Context, c *call) (any, error) {
	var request auth.ConversationCancel
	if err := c.decode(&request); err != nil || request.ConversationID == "" || len(request.ConversationID) > 64 {
		return nil, auth.Code("invalid-conversation")
	}
	c.handler.conversations.cancel(request.ConversationID)
	return nil, nil
}

func runConfirmSession(_ context.Context, c *call) (any, error) {
	if !c.handler.grants.confirm(c.request.Token) {
		return nil, auth.Code("invalid-bridge-token")
	}
	return nil, nil
}

func runCloseSession(_ context.Context, c *call) (any, error) {
	if !c.handler.grants.close(c.request.Token) {
		return nil, auth.Code("invalid-bridge-token")
	}
	return nil, nil
}

func runAuthorizeAdmin(ctx context.Context, c *call) (any, error) {
	var request auth.AdminAuthorization
	err := c.decode(&request)
	defer func() { request.Password = "" }()
	if err != nil || request.Password == "" || len(request.Password) > 16<<10 || request.TTLSeconds > 3600 {
		return nil, auth.Code("invalid-request")
	}
	if c.handler.policy == nil {
		return nil, auth.Code("administrative-policy-unavailable")
	}
	token, until, err := c.handler.grants.authorize(ctx, c.request.Token, request.Password, request.TTLSeconds, c.handler.policy)
	if err != nil {
		if errors.Is(err, errAdministrativeUnavailable) {
			return nil, auth.Code("administrative-policy-unavailable")
		}
		return nil, auth.Code("administrative-access-denied")
	}
	c.handler.logger.Info("administrative access granted", zap.String("username", c.identity.Username), zap.Time("until", until))
	return auth.AdminGrant{Token: token, Until: until}, nil
}

func runRevokeAdmin(_ context.Context, c *call) (any, error) {
	if !c.handler.grants.revokeAdmin(c.request.AdminToken) {
		return nil, auth.Code("invalid-admin-token")
	}
	return nil, nil
}

const maxCertificateBytes = 1 << 20

// runCertificate returns the configured TLS pair. Only the gateway can reach
// the session socket; reading the files here keeps private keys in root-only
// locations instead of granting the sandboxed gateway file access.
func runCertificate(_ context.Context, c *call) (any, error) {
	certificatePath, keyPath := c.handler.certificate, c.handler.certificateKey
	if certificatePath == "" || keyPath == "" {
		return nil, auth.Code("certificate-unavailable")
	}
	certificate, err := readBoundedFile(certificatePath, maxCertificateBytes)
	if err != nil {
		return nil, err
	}
	key, err := readBoundedFile(keyPath, maxCertificateBytes)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	if _, err := tls.X509KeyPair(certificate, key); err != nil {
		return nil, auth.Code("certificate-invalid")
	}
	return auth.CertificateMaterial{Certificate: string(certificate), Key: string(key)}, nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, auth.Code("certificate-invalid")
	}
	return content, nil
}

func runResizeTerminal(_ context.Context, c *call) (any, error) {
	var size auth.TerminalSize
	if err := c.decode(&size); err != nil {
		return nil, err
	}
	if code := c.handler.grants.resize(c.request.Token, size.Columns, size.Rows); code != "" {
		return nil, auth.Code(code)
	}
	return nil, nil
}

func openTerminal(c *call) {
	var size auth.TerminalSize
	if err := c.decode(&size); err != nil {
		c.fail("invalid-request")
		return
	}
	_ = c.conn.SetDeadline(time.Time{})
	identity, environment, ok := c.handler.grants.claim(c.request.Token)
	if !ok {
		c.fail("terminal-busy")
		return
	}
	c.handler.logger.Info("terminal starting", zap.String("username", identity.Username), zap.Int("uid", identity.UID))
	runTerminal(c.conn, c.reader, identity, environment, size, c.handler.grants, c.request.Token, c.handler.logger)
}

// Services and units.

func validateServiceRead(operation auth.ServiceReadOperation) error {
	if operation.Scope != "system" && operation.Scope != "user" {
		return platform.ErrInvalidServiceScope
	}
	if operation.Type != "" && !platform.ValidUnitType(operation.Type) {
		return platform.ErrInvalidServiceUnit
	}
	return nil
}

// userScopeLane rejects user units in the root lane: administrative elevation
// never changes the user identity, so user units always run in the bridge.
func (c *call) userScopeLane(scope string) error {
	if scope == "user" && c.administrative {
		return errPermissionDenied
	}
	return nil
}

func runServicesList(ctx context.Context, c *call) (any, error) {
	var operation auth.ServiceReadOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := validateServiceRead(operation); err != nil || operation.Unit != "" {
		return nil, platform.ErrInvalidServiceUnit
	}
	if err := c.userScopeLane(operation.Scope); err != nil {
		return nil, err
	}
	if c.administrative {
		return platform.Units(ctx, operation.Scope, operation.Type)
	}
	return c.viaBridge(ctx, "services.read", operation)
}

func runServiceDetail(ctx context.Context, c *call) (any, error) {
	var operation auth.ServiceReadOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateServiceTarget(operation.Scope, operation.Unit); err != nil {
		return nil, err
	}
	if err := c.userScopeLane(operation.Scope); err != nil {
		return nil, err
	}
	if c.administrative {
		return platform.UnitDetails(ctx, operation.Scope, operation.Unit)
	}
	return c.viaBridge(ctx, "services.detail", operation)
}

func runServiceConfiguration(ctx context.Context, c *call) (any, error) {
	var operation auth.ServiceReadOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateServiceTarget(operation.Scope, operation.Unit); err != nil {
		return nil, err
	}
	if err := c.userScopeLane(operation.Scope); err != nil {
		return nil, err
	}
	if c.administrative {
		return platform.ReadUnitConfiguration(ctx, operation.Scope, operation.Unit)
	}
	return c.viaBridge(ctx, "services.configuration", operation)
}

func runServiceAction(ctx context.Context, c *call) (any, error) {
	var operation platform.ServiceOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if _, err := platform.ParseServiceOperation(operation.Scope, operation.Unit, operation.Action); err != nil {
		return nil, err
	}
	if err := c.userScopeLane(operation.Scope); err != nil {
		return nil, err
	}
	if operation.Scope == "user" {
		if _, err := c.viaBridge(ctx, "services.action", operation); err != nil {
			return nil, err
		}
	} else {
		if !c.administrative {
			return nil, errPermissionDenied
		}
		if err := c.handler.services.Run(ctx, operation); err != nil {
			return nil, err
		}
	}
	c.handler.logger.Info("service action", zap.String("username", c.identity.Username), zap.String("scope", operation.Scope), zap.String("unit", operation.Unit), zap.String("action", operation.Action))
	return nil, nil
}

// scopeLane enforces the rule shared by timers and overrides: system units
// need administrative access and user units need the user's own session.
func (c *call) scopeLane(scope string) (bridgeCaller, error) {
	if scope == "system" {
		if !c.administrative {
			return nil, errPermissionDenied
		}
		return nil, nil
	}
	if c.administrative {
		return nil, errPermissionDenied
	}
	return c.bridge()
}

func runTimer(ctx context.Context, c *call) (any, error) {
	var operation platform.TimerOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateTimerOperation(operation); err != nil {
		return nil, err
	}
	bridge, err := c.scopeLane(operation.Scope)
	if err != nil {
		return nil, err
	}
	state, err := c.handler.timers.Apply(ctx, operation, bridge)
	if err == nil && operation.Action != "preview" {
		c.handler.logger.Info("timer changed", zap.String("username", c.identity.Username), zap.String("scope", operation.Scope), zap.String("name", operation.Name), zap.String("action", operation.Action))
	}
	return state, err
}

func runOverride(ctx context.Context, c *call) (any, error) {
	var operation platform.OverrideOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateOverrideOperation(operation); err != nil {
		return nil, err
	}
	bridge, err := c.scopeLane(operation.Scope)
	if err != nil {
		return nil, err
	}
	state, err := c.handler.overrides.Apply(ctx, operation, bridge)
	if err == nil && operation.Action != "preview" {
		c.handler.logger.Info("service override changed", zap.String("username", c.identity.Username), zap.String("scope", operation.Scope), zap.String("unit", operation.Unit), zap.String("action", operation.Action))
	}
	return state, err
}

// Host identity, configuration, and power.

func runHostInfo(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return host.ReadContext(ctx), nil
	}
	return c.viaBridge(ctx, "host.info", nil)
}

func runHostConfiguration(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return c.handler.hostConfig.Read(ctx)
	}
	return c.viaBridge(ctx, "host.configuration.read", nil)
}

func runHostConfigure(ctx context.Context, c *call) (any, error) {
	var operation auth.HostConfigurationOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := validateHostConfiguration(operation); err != nil {
		return nil, auth.Code("invalid-host-configuration")
	}
	current, err := c.handler.hostConfig.Read(ctx)
	if err != nil {
		return nil, auth.Code("host-config-unavailable")
	}
	if current.Fingerprint != operation.ExpectedFingerprint {
		return nil, auth.Code("host-config-conflict")
	}
	desired := platform.NewHostConfiguration(operation.Hostname, operation.Timezone, operation.NTPEnabled)
	if err := c.handler.hostConfig.Apply(ctx, current, desired); err != nil {
		return nil, err
	}
	c.handler.logger.Info("host configuration changed", zap.String("username", c.identity.Username))
	return nil, nil
}

func validateHostConfiguration(operation auth.HostConfigurationOperation) error {
	timezone := operation.Timezone
	if !validHostname(operation.Hostname) || timezone == "" || len(timezone) > 128 || strings.ContainsAny(timezone, "\x00\r\n") || strings.Contains(timezone, "..") {
		return errors.New("invalid host configuration")
	}
	if !validFingerprint(operation.ExpectedFingerprint) {
		return errors.New("invalid host configuration fingerprint")
	}
	return nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validHostname(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func runPowerStatus(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return c.handler.power.Read(ctx)
	}
	return c.viaBridge(ctx, "host.power.read", nil)
}

func runPowerRequest(ctx context.Context, c *call) (any, error) {
	var operation auth.PowerOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if code := validatePowerOperation(operation); code != "" {
		return nil, auth.Code(code)
	}
	status, err := c.handler.power.Read(ctx)
	if err != nil {
		return nil, auth.Code("power-unavailable")
	}
	if status.Fingerprint != operation.ExpectedFingerprint {
		return nil, auth.Code("power-conflict")
	}
	selected := status.Reboot
	if operation.Action == "shutdown" {
		selected = status.Shutdown
	}
	if !selected.Available {
		return nil, auth.Code("power-" + selected.State)
	}
	if err := c.handler.power.Request(ctx, operation.Action); err != nil {
		return nil, err
	}
	c.handler.logger.Info("host power request", zap.String("username", c.identity.Username), zap.String("action", operation.Action))
	return nil, nil
}

func validatePowerOperation(operation auth.PowerOperation) string {
	if operation.Action != "reboot" && operation.Action != "shutdown" {
		return "invalid-power-action"
	}
	if operation.Confirmation != strings.ToUpper(operation.Action) {
		return "invalid-power-confirmation"
	}
	if !validFingerprint(operation.ExpectedFingerprint) {
		return "invalid-power-fingerprint"
	}
	return ""
}

func runCapabilities(ctx context.Context, c *call) (any, error) {
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	return platform.Detect(ctx, runtime.updates), nil
}

// Processes. Inventory and inspection come from the shared root collector.
// Without administrative access the result is cut down to what the caller
// could read in /proc themselves. Signals run in the caller's lane.

func runProcessList(_ context.Context, c *call) (any, error) {
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	items, err := runtime.processTracker.Snapshot()
	if err != nil || c.administrative {
		return items, err
	}
	return c.processView().Processes(items), nil
}

func runProcessDetail(ctx context.Context, c *call) (any, error) {
	var operation auth.ProcessReadOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	details, err := runtime.processTracker.Inspect(ctx, operation.PID, operation.Started)
	if err != nil || c.administrative {
		return details, err
	}
	return c.processView().Details(details)
}

// processView describes the caller's /proc visibility. Group lookup failures
// fall back to the primary group, which only ever narrows the view.
func (c *call) processView() platform.ProcessView {
	groups := []int{c.identity.GID}
	if account, err := user.LookupId(strconv.Itoa(c.identity.UID)); err == nil {
		if credential, err := userCredential(account, c.identity); err == nil {
			for _, group := range credential.Groups {
				groups = append(groups, int(group))
			}
		}
	}
	return platform.NewProcessView(c.identity.UID, groups)
}

func runSignalPreview(ctx context.Context, c *call) (any, error) {
	var operation platform.SignalOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if c.administrative {
		return platform.PreviewSignal(ctx, operation)
	}
	return c.viaBridge(ctx, "processes.signal-preview", operation)
}

func runSignal(ctx context.Context, c *call) (any, error) {
	var operation platform.SignalOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if operation.Action != "apply" {
		return nil, platform.ErrInvalidSignalOperation
	}
	if err := platform.ValidateSignalOperation(operation); err != nil {
		return nil, err
	}
	var result any
	var err error
	if c.administrative {
		result, err = platform.ApplySignal(ctx, operation, nil)
	} else {
		result, err = c.viaBridge(ctx, "processes.signal", operation)
	}
	if err == nil {
		c.handler.logger.Info("process signal", zap.String("username", c.identity.Username), zap.String("signal", string(operation.Signal)), zap.Int("pid", operation.Target.PID), zap.Bool("tree", operation.Tree), zap.Bool("administrative", c.administrative))
	}
	return result, err
}

// Accounts and groups.

func runIdentities(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return platform.ListIdentityInventory(ctx)
	}
	return c.viaBridge(ctx, "identities.read", nil)
}

func runLocalAccount(ctx context.Context, c *call) (any, error) {
	var operation platform.LocalAccountOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateLocalAccountOperation(operation); err != nil {
		return nil, err
	}
	if operation.Preview {
		return c.handler.accounts.Preview(ctx, operation, c.identity)
	}
	return c.handler.accounts.Apply(ctx, operation, c.identity)
}

func runGroupMembership(ctx context.Context, c *call) (any, error) {
	var operation platform.GroupMembershipOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateGroupMembershipOperation(operation); err != nil {
		return nil, err
	}
	if operation.Preview {
		return c.handler.groups.Preview(ctx, operation, c.identity)
	}
	return c.handler.groups.Apply(ctx, operation, c.identity)
}

func runAdministrativeRole(ctx context.Context, c *call) (any, error) {
	var operation platform.AdministrativeRoleOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateAdministrativeRoleOperation(operation); err != nil {
		return nil, err
	}
	if operation.Preview {
		return c.handler.roles.Preview(ctx, operation, c.identity)
	}
	return c.handler.roles.Apply(ctx, operation, c.identity)
}

func runLocalGroup(ctx context.Context, c *call) (any, error) {
	var operation platform.LocalGroupOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateLocalGroupOperation(operation); err != nil {
		return nil, err
	}
	if operation.Preview {
		return platform.PreviewLocalGroup(ctx, operation)
	}
	return platform.ApplyLocalGroup(ctx, operation)
}

func runPassword(ctx context.Context, c *call) (any, error) {
	var operation auth.PasswordChangeOperation
	err := c.decode(&operation)
	defer operation.Clear()
	if err != nil || auth.ValidatePasswordChangeOperation(operation) != nil {
		return nil, auth.ErrPasswordInvalid
	}
	// Changing your own password needs the user session and the current
	// password; resetting another account needs administrative access.
	if (operation.Action == "change") == c.administrative {
		return nil, auth.ErrPasswordInvalid
	}
	if operation.Action == "change" {
		err = c.handler.passwords.Change(ctx, c.identity.Username, operation.CurrentPassword, operation.NewPassword)
	} else {
		err = c.handler.passwords.Reset(ctx, operation.Username, operation.NewPassword)
	}
	if err != nil {
		return nil, err
	}
	c.handler.logger.Info("password changed", zap.String("username", c.identity.Username), zap.String("action", operation.Action))
	return nil, nil
}

func runSSHKeys(ctx context.Context, c *call) (any, error) {
	var operation platform.SSHKeyOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if !c.administrative && operation.Username != c.identity.Username {
		return nil, platform.ErrSSHKeyUnauthorized
	}
	if err := platform.ValidateSSHKeyOperation(operation); err != nil {
		return nil, err
	}
	if operation.Preview {
		return c.handler.sshKeys.Preview(ctx, operation, c.identity, c.administrative)
	}
	return c.handler.sshKeys.Apply(ctx, operation, c.identity, c.administrative)
}

func runLoginHistory(ctx context.Context, c *call) (any, error) {
	var query platform.LoginHistoryQuery
	if err := c.decode(&query); err != nil {
		return nil, err
	}
	if !c.administrative && query.Username != c.identity.Username {
		return nil, errPermissionDenied
	}
	if c.administrative {
		return platform.QueryLoginHistory(ctx, query)
	}
	return c.viaBridge(ctx, "login-history.read", query)
}

// Storage, network, firewall, security.

func runStorageRead(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return platform.ReadStorageSnapshot(ctx)
	}
	filesystems, err := bridgeInto[[]platform.Filesystem](ctx, c, "storage.read", nil)
	if err != nil {
		return nil, err
	}
	return platform.StorageSnapshotFromFilesystems(filesystems, "Hardware inventory requires administrative access."), nil
}

func runStorageApply(ctx context.Context, c *call) (any, error) {
	var operation platform.StorageOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateStorageOperation(operation); err != nil {
		return nil, err
	}
	if operation.Action == "preview" {
		return platform.PreviewStorageOperation(ctx, operation)
	}
	return platform.ApplyStorageOperation(ctx, operation)
}

func runNetworkRead(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return platform.NetworkSnapshotRead(ctx)
	}
	return c.viaBridge(ctx, "network.read", nil)
}

func runNetworkApply(ctx context.Context, c *call) (any, error) {
	var operation platform.NetworkOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateNetworkOperation(operation); err != nil {
		return nil, err
	}
	if operation.Action == "preview" {
		return platform.PreviewNetworkOperation(ctx, operation)
	}
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.network.Apply(ctx, operation)
}

func runFirewallApply(ctx context.Context, c *call) (any, error) {
	var operation platform.FirewallOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateFirewallOperation(operation); err != nil {
		return nil, err
	}
	if operation.Action == "preview" {
		return platform.PreviewFirewallOperation(ctx, operation)
	}
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.firewall.Apply(ctx, operation)
}

// runSecurityPreview inspects policy state and, for a remediation, reports
// what it would change. It never modifies the host.
func runSecurityPreview(ctx context.Context, c *call) (any, error) {
	var operation platform.SecurityOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	return platform.PreviewSecurityOperation(ctx, operation)
}

func runSecurityApply(ctx context.Context, c *call) (any, error) {
	var operation platform.SecurityOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if operation.Action == "inspect" || operation.Action == "" {
		return nil, platform.ErrInvalidSecurityOperation
	}
	if err := platform.ValidateSecurityOperation(operation); err != nil {
		return nil, err
	}
	return platform.ApplySecurityOperation(ctx, operation)
}

// Software updates and kernel live patching.

func (c *call) updateService() (*platform.UpdateService, error) {
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	if runtime.updates == nil {
		return nil, platform.ErrUpdateUnavailable
	}
	return runtime.updates, nil
}

func runUpdateStatus(ctx context.Context, c *call) (any, error) {
	service, err := c.updateService()
	if err != nil {
		return nil, err
	}
	return service.Status(ctx), nil
}

func runUpdatePreview(ctx context.Context, c *call) (any, error) {
	var operation platform.UpdateOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	service, err := c.updateService()
	if err != nil {
		return nil, err
	}
	return service.Preview(ctx, operation)
}

func runUpdateRefresh(ctx context.Context, c *call) (any, error) {
	var operation auth.UpdateRefreshOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	service, err := c.updateService()
	if err != nil {
		return nil, err
	}
	if err := service.Refresh(ctx, operation.Force); err != nil {
		return nil, err
	}
	return service.Status(ctx), nil
}

func runUpdateHistory(ctx context.Context, c *call) (any, error) {
	service, err := c.updateService()
	if err != nil {
		return nil, err
	}
	return service.History(ctx)
}

func runUpdateLive(_ context.Context, c *call) (any, error) {
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.updateObservation(), nil
}

func runUpdateApply(ctx context.Context, c *call) (any, error) {
	var operation platform.UpdateOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateUpdateOperation(operation, true); err != nil {
		return nil, err
	}
	result, err := c.handler.updates.Apply(ctx, operation, c.identity)
	if err == nil {
		c.handler.logger.Info("software updates applied", zap.String("username", c.identity.Username), zap.Int("changes", len(result.Changes)))
	}
	return result, err
}

func runKpatchRead(ctx context.Context, c *call) (any, error) {
	if c.administrative {
		return auth.KpatchReadResult{Status: platform.InspectKpatchStatus(ctx), Settings: platform.InspectKpatchSettings(ctx)}, nil
	}
	return c.viaBridge(ctx, "kpatch.read", nil)
}

func runKpatchApply(ctx context.Context, c *call) (any, error) {
	var operation platform.KpatchOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if err := platform.ValidateKpatchOperation(operation); err != nil {
		return nil, err
	}
	settings, err := c.handler.kpatch.Apply(ctx, operation, c.identity)
	if err == nil {
		c.handler.logger.Info("kpatch settings changed", zap.String("username", c.identity.Username), zap.Bool("apply", operation.Apply != nil && *operation.Apply))
	}
	return settings, err
}

// Files, journal, metrics.

func runFiles(ctx context.Context, c *call) (any, error) {
	var operation platform.FileOperation
	if err := c.decode(&operation); err != nil {
		return nil, platform.ErrInvalidFileOperation
	}
	if err := platform.ValidateFileOperation(operation); err != nil {
		return nil, err
	}
	if c.administrative {
		return platform.ApplyPrivilegedFileOperation(ctx, operation)
	}
	return c.viaBridge(ctx, "file.apply", operation)
}

// journalCredential runs user-lane journal reads with the caller's UNIX
// identity; the administrative lane reads the whole journal as root.
func (c *call) journalCredential() *syscall.Credential {
	if c.administrative {
		return nil
	}
	credential := &syscall.Credential{Uid: uint32(c.identity.UID), Gid: uint32(c.identity.GID)}
	if account, err := user.LookupId(strconv.Itoa(c.identity.UID)); err == nil {
		if full, err := userCredential(account, c.identity); err == nil {
			credential = full
		}
	}
	return credential
}

func (c *call) journalQuery() (platform.JournalQuery, error) {
	var query platform.JournalQuery
	if err := c.decode(&query); err != nil {
		return query, platform.ErrInvalidJournalQuery
	}
	if err := query.Validate(); err != nil {
		return query, platform.ErrInvalidJournalQuery
	}
	return query, nil
}

func runJournalQuery(ctx context.Context, c *call) (any, error) {
	query, err := c.journalQuery()
	if err != nil {
		return nil, err
	}
	release, allowed := journalSlots.acquire(c.identity.UID, false)
	if !allowed {
		return nil, auth.Code("logs-busy")
	}
	defer release()
	return platform.QueryLogsAs(ctx, query, c.journalCredential())
}

func followJournal(c *call) {
	query, err := c.journalQuery()
	if err != nil {
		c.fail("invalid-journal-query")
		return
	}
	release, allowed := journalSlots.acquire(c.identity.UID, true)
	if !allowed {
		c.fail("logs-busy")
		return
	}
	defer release()
	ctx, cancel := c.streamContext()
	defer cancel()
	err = platform.FollowJournalAs(ctx, query, c.journalCredential(), func(entry platform.LogEntry) error {
		entry.Cursor = ""
		return c.emit(entry)
	})
	if err != nil && ctx.Err() == nil {
		c.fail("logs-unavailable")
	}
}

func runMetricsHistory(_ context.Context, c *call) (any, error) {
	var operation auth.MetricsHistoryOperation
	if err := c.decode(&operation); err != nil {
		return nil, err
	}
	if operation.Maximum < 0 || operation.Maximum > 10000 {
		return nil, auth.Code("bounded-output")
	}
	runtime, err := c.requireRuntime()
	if err != nil {
		return nil, err
	}
	if operation.Since.IsZero() {
		operation.Since = time.Now().Add(-24 * time.Hour)
	}
	samples := runtime.sampler.HistorySince(operation.Since, operation.Maximum)
	if samples == nil {
		samples = []metrics.Sample{}
	}
	return samples, nil
}

func followMetrics(c *call) {
	var operation auth.MetricsFollowOperation
	if err := c.decode(&operation); err != nil {
		c.fail("invalid-request")
		return
	}
	interval := operation.Interval
	if interval == 0 {
		interval = hostMetricInterval
	}
	if interval < time.Second || interval > 5*time.Minute {
		c.fail("bounded-output")
		return
	}
	runtime, err := c.requireRuntime()
	if err != nil {
		c.fail("system-backend-unavailable")
		return
	}
	ctx, cancel := c.streamContext()
	defer cancel()
	stream, unsubscribe := runtime.sampler.SubscribeEvery(interval)
	defer unsubscribe()
	for {
		select {
		case sample, ok := <-stream:
			if !ok || c.emit(sample) != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

const (
	maxStreamLifetime   = 15 * time.Minute
	streamAuthorization = 5 * time.Second
)

// streamContext bounds a stream, ends it when the client hangs up, and
// re-checks the caller's grant so a closed session or dropped administrative
// access stops the stream promptly.
func (c *call) streamContext() (context.Context, context.CancelFunc) {
	_ = c.conn.SetDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), maxStreamLifetime)
	go func() {
		var buffer [1]byte
		_, _ = c.conn.Read(buffer[:])
		cancel()
	}()
	go func() {
		ticker := time.NewTicker(streamAuthorization)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lane := laneSession
				if c.administrative {
					lane = laneAdmin
				}
				if (&call{handler: c.handler, request: c.request}).authorize(lane) != "" {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}
