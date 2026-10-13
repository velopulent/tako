package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/velopulent/tako/internal/auth"
	"github.com/velopulent/tako/internal/session"
)

type problemSpec struct {
	status  int
	message string
}

// problems maps every stable error code to its HTTP status and operator-facing
// message. Handlers never echo internal error text.
var problems = map[string]problemSpec{
	"invalid-request":                {http.StatusBadRequest, "The request is invalid"},
	"timeout":                        {http.StatusGatewayTimeout, "The host did not respond in time"},
	"service-unavailable":            {http.StatusServiceUnavailable, "The privileged session service is unavailable"},
	"system-backend-unavailable":     {http.StatusServiceUnavailable, "The host service is unavailable"},
	"development-unsupported":        {http.StatusServiceUnavailable, "Not available in development mode; run the host-integrated stack"},
	"permission-denied":              {http.StatusForbidden, "UNIX authority does not permit this operation"},
	"user-bridge-unavailable":        {http.StatusForbidden, "Your authenticated user session is unavailable"},
	"user-manager-unavailable":       {http.StatusServiceUnavailable, "Your systemd user manager is unavailable"},
	"bounded-output":                 {http.StatusRequestEntityTooLarge, "The result exceeds the bounded output limit"},
	"session-expired":                {http.StatusUnauthorized, "Your session has expired"},
	"administrative-access-required": {http.StatusForbidden, "Gain administrative access first"},

	"invalid-file-operation": {http.StatusBadRequest, "File operation is invalid"},
	"file-not-found":         {http.StatusNotFound, "The file was not found"},
	"file-permission-denied": {http.StatusForbidden, "UNIX authority denied this file operation"},
	"file-conflict":          {http.StatusConflict, "The file changed; reload and try again"},
	"file-too-large":         {http.StatusRequestEntityTooLarge, "The file exceeds the bounded operation limit"},
	"unsafe-archive":         {http.StatusBadRequest, "The archive contains an unsafe path or link"},
	"archive-limit":          {http.StatusRequestEntityTooLarge, "The archive exceeds the bounded operation limit"},
	"file-operation-failed":  {http.StatusBadGateway, "The file operation failed"},
	"file-preview-expired":   {http.StatusForbidden, "The file preview expired; reopen the folder"},

	"invalid-network-operation":  {http.StatusBadRequest, "Network operation is invalid"},
	"network-conflict":           {http.StatusConflict, "Network state changed; refresh and try again"},
	"network-ownership-conflict": {http.StatusConflict, "Network ownership is conflicted; changes are disabled"},
	"network-checkpoint-invalid": {http.StatusConflict, "The network reconnect checkpoint is invalid or expired"},
	"network-unavailable":        {http.StatusServiceUnavailable, "The network adapter is unavailable"},
	"network-operation-failed":   {http.StatusBadGateway, "The network operation failed"},

	"invalid-storage-operation": {http.StatusBadRequest, "Storage operation is invalid"},
	"storage-conflict":          {http.StatusConflict, "Storage state changed; refresh and try again"},
	"storage-unsafe":            {http.StatusForbidden, "The storage target is protected or read-only"},
	"storage-busy":              {http.StatusConflict, "The storage target is busy"},
	"storage-unavailable":       {http.StatusServiceUnavailable, "The storage service is unavailable"},
	"storage-operation-failed":  {http.StatusBadGateway, "The storage operation failed"},

	"invalid-firewall-operation":  {http.StatusBadRequest, "Firewall operation is invalid"},
	"firewall-conflict":           {http.StatusConflict, "Firewall state changed; refresh and try again"},
	"firewall-ownership-conflict": {http.StatusConflict, "Conflicting firewall ownership detected"},
	"firewall-access-risk":        {http.StatusForbidden, "This change could lock out management access"},
	"firewall-checkpoint-invalid": {http.StatusConflict, "The firewall rollback checkpoint is invalid or expired"},
	"firewall-unavailable":        {http.StatusServiceUnavailable, "No supported active firewall is available"},
	"firewall-operation-failed":   {http.StatusBadGateway, "The firewall operation failed"},

	"invalid-security-operation": {http.StatusBadRequest, "Security operation is invalid"},
	"security-conflict":          {http.StatusConflict, "Security policy changed; refresh and try again"},
	"security-unsafe":            {http.StatusForbidden, "The remediation is outside the safe policy"},
	"security-unavailable":       {http.StatusServiceUnavailable, "The security framework is unavailable"},
	"security-operation-failed":  {http.StatusBadGateway, "The security operation failed"},

	"invalid-ssh-key-operation":   {http.StatusBadRequest, "The SSH key operation is invalid"},
	"ssh-key-conflict":            {http.StatusConflict, "Authorized keys changed; refresh before applying"},
	"ssh-key-not-found":           {http.StatusConflict, "The selected authorized key no longer exists"},
	"ssh-key-read-only":           {http.StatusForbidden, "Remote NSS identities are read-only"},
	"ssh-key-unauthorized":        {http.StatusForbidden, "You are not authorized to manage this user's SSH keys"},
	"ssh-key-protected":           {http.StatusForbidden, "The authorized_keys path is protected"},
	"ssh-key-verification-failed": {http.StatusBadGateway, "The authorized_keys change could not be verified"},
	"ssh-key-unavailable":         {http.StatusServiceUnavailable, "The SSH key service is unavailable"},

	"invalid-local-account-operation":   {http.StatusBadRequest, "The account operation is invalid"},
	"local-account-conflict":            {http.StatusConflict, "The account changed; refresh and try again"},
	"local-account-not-found":           {http.StatusConflict, "The account no longer exists"},
	"local-account-read-only":           {http.StatusForbidden, "Remote NSS identities are read-only"},
	"local-account-protected":           {http.StatusForbidden, "Your own account cannot be locked or deleted"},
	"local-account-verification-failed": {http.StatusBadGateway, "The account change could not be verified"},
	"local-account-unavailable":         {http.StatusServiceUnavailable, "Account management is unavailable"},

	"invalid-group-operation":         {http.StatusBadRequest, "The group operation is invalid"},
	"group-conflict":                  {http.StatusConflict, "Group membership changed; refresh and try again"},
	"group-not-found":                 {http.StatusConflict, "The group no longer exists"},
	"group-user-not-found":            {http.StatusConflict, "The user no longer exists"},
	"group-read-only":                 {http.StatusForbidden, "Remote identities and groups are read-only"},
	"group-verification-failed":       {http.StatusBadGateway, "The group change could not be verified"},
	"group-unavailable":               {http.StatusServiceUnavailable, "Group management is unavailable"},
	"administrative-role-unavailable": {http.StatusServiceUnavailable, "No supported sudo or wheel group is available"},
	"administrative-role-protected":   {http.StatusForbidden, "You cannot remove yourself or the last administrator"},

	"invalid-local-group-operation":   {http.StatusBadRequest, "The group operation is invalid"},
	"local-group-conflict":            {http.StatusConflict, "The group changed; refresh and try again"},
	"local-group-not-found":           {http.StatusConflict, "The group no longer exists"},
	"local-group-read-only":           {http.StatusForbidden, "Remote NSS groups are read-only"},
	"local-group-protected":           {http.StatusForbidden, "This system group cannot be deleted"},
	"local-group-verification-failed": {http.StatusBadGateway, "The group change could not be verified"},
	"local-group-unavailable":         {http.StatusServiceUnavailable, "Group management is unavailable"},

	"invalid-password-operation":     {http.StatusBadRequest, "The password operation is invalid"},
	"password-authentication-failed": {http.StatusForbidden, "The current password was not accepted"},
	"password-expired":               {http.StatusConflict, "The password has expired; change it through the host policy"},
	"password-policy-failed":         {http.StatusUnprocessableEntity, "The new password was rejected by host policy"},
	"password-unavailable":           {http.StatusServiceUnavailable, "The password service is unavailable"},

	"invalid-update-operation":   {http.StatusBadRequest, "Update operation is invalid"},
	"update-conflict":            {http.StatusConflict, "Available updates changed; refresh before applying"},
	"update-locked":              {http.StatusConflict, "Another package operation holds the package-manager lock"},
	"update-risk-not-accepted":   {http.StatusConflict, "Risky update changes require explicit confirmation"},
	"update-verification-failed": {http.StatusBadGateway, "The update could not be verified"},
	"update-apply-failed":        {http.StatusBadGateway, "The package manager reported a failure"},
	"updates-unavailable":        {http.StatusServiceUnavailable, "No supported update backend is available"},
	"invalid-kpatch-operation":   {http.StatusBadRequest, "Live patch operation is invalid"},
	"kpatch-apply-failed":        {http.StatusBadGateway, "Kernel live patching could not be configured"},

	"invalid-timer-operation": {http.StatusBadRequest, "Timer operation is invalid"},
	"timer-conflict":          {http.StatusConflict, "The timer changed; refresh before applying"},
	"timer-not-found":         {http.StatusConflict, "The timer files are not in a usable state"},
	"timer-pair-incomplete":   {http.StatusConflict, "The timer files are not in a usable state"},
	"timer-operation-failed":  {http.StatusBadGateway, "The timer operation failed; inspect system logs"},

	"invalid-service-override":   {http.StatusBadRequest, "Service override is invalid"},
	"service-override-conflict":  {http.StatusConflict, "The override changed; refresh and try again"},
	"service-override-not-found": {http.StatusConflict, "The managed Tako drop-in is unavailable"},
	"service-override-unmanaged": {http.StatusConflict, "The drop-in contains directives Tako does not manage"},
	"service-override-failed":    {http.StatusBadGateway, "The service override failed; inspect system logs"},

	"invalid-service-operation": {http.StatusBadRequest, "Unsupported service scope, unit, or action"},
	"service-action-failed":     {http.StatusBadGateway, "The service action failed; inspect system logs"},

	"invalid-signal-operation":    {http.StatusBadRequest, "Signal or process identity is invalid"},
	"process-not-found":           {http.StatusNotFound, "The process no longer exists"},
	"process-reused":              {http.StatusConflict, "The process ID now refers to a different process"},
	"process-signal-conflict":     {http.StatusConflict, "The process or tree changed; refresh and try again"},
	"process-signal-unauthorized": {http.StatusForbidden, "UNIX authority does not permit signaling this process"},
	"process-signal-failed":       {http.StatusBadGateway, "The process signal failed"},

	"invalid-journal-query":       {http.StatusBadRequest, "Unsupported journal filter, cursor, or time range"},
	"logs-busy":                   {http.StatusTooManyRequests, "Too many journal readers are active"},
	"logs-unavailable":            {http.StatusServiceUnavailable, "The journal could not be queried"},
	"invalid-login-history-query": {http.StatusBadRequest, "Unsupported identity, cursor, outcome, or time range"},
	"login-history-unavailable":   {http.StatusServiceUnavailable, "The login history could not be queried"},

	"invalid-host-configuration": {http.StatusBadRequest, "Hostname, timezone, and current fingerprint are required"},
	"host-config-conflict":       {http.StatusConflict, "Host configuration changed; refresh and try again"},
	"host-config-unavailable":    {http.StatusServiceUnavailable, "Hostname and time configuration is unavailable"},
	"host-config-failed":         {http.StatusBadGateway, "Host configuration could not be applied"},

	"invalid-power-action":       {http.StatusBadRequest, "Choose reboot or shutdown"},
	"invalid-power-confirmation": {http.StatusBadRequest, "Type the action name to confirm"},
	"invalid-power-fingerprint":  {http.StatusBadRequest, "The current power state fingerprint is required"},
	"power-conflict":             {http.StatusConflict, "Power state changed; refresh and try again"},
	"power-challenged":           {http.StatusForbidden, "The host requires an interactive authorization challenge"},
	"power-denied":               {http.StatusForbidden, "The host policy denies this power action"},
	"power-inhibited":            {http.StatusConflict, "An active inhibitor blocks this power action"},
	"power-unavailable":          {http.StatusServiceUnavailable, "Host power management is unavailable"},
	"power-request-failed":       {http.StatusBadGateway, "The power request failed"},
}

// fail writes the problem for err. Errors without a registered code use
// fallback; their text is logged by the caller's middleware, never returned.
func fail(writer http.ResponseWriter, err error, fallback string) {
	code := auth.ErrorCode(err, "")
	switch {
	case code == "invalid-admin-token":
		code = "administrative-access-required"
	case code == "invalid-bridge-token":
		code = "session-expired"
	case code == "" && errors.Is(err, auth.ErrServiceUnavailable):
		code = "service-unavailable"
	case code == "":
		code = fallback
	}
	spec, ok := problems[code]
	if !ok {
		spec = problemSpec{http.StatusBadGateway, "The host operation failed"}
	}
	problem(writer, spec.status, code, spec.message)
}

// reject writes a registered problem by code.
func reject(writer http.ResponseWriter, code string) {
	spec := problems[code]
	problem(writer, spec.status, code, spec.message)
}

// decodeBody strictly decodes a bounded JSON body. Unknown fields and trailing
// data are rejected with the given problem code.
func decodeBody[T any](writer http.ResponseWriter, request *http.Request, limit int64, code string) (T, bool) {
	var value T
	request.Body = http.MaxBytesReader(writer, request.Body, limit)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		spec, ok := problems[code]
		if !ok {
			spec = problems["invalid-request"]
		}
		problem(writer, http.StatusBadRequest, code, spec.message)
		var zero T
		return zero, false
	}
	return value, true
}

func currentSession(request *http.Request) session.Session {
	return request.Context().Value(sessionKey{}).(session.Session)
}

func hasAdministrativeAccess(current session.Session) bool {
	return current.Identity.AdminToken != "" && time.Now().Before(current.AdminUntil)
}

// requireAdmin writes 403 unless the session holds a live administrative grant.
func requireAdmin(writer http.ResponseWriter, current session.Session) bool {
	if !hasAdministrativeAccess(current) {
		reject(writer, "administrative-access-required")
		return false
	}
	return true
}

// sessionCredentials selects the strongest lane the session holds: the root
// lane while elevated, otherwise the user's own bridge.
func sessionCredentials(current session.Session) auth.Credentials {
	if hasAdministrativeAccess(current) {
		return auth.Credentials{AdminToken: current.Identity.AdminToken}
	}
	return auth.Credentials{Token: current.Identity.BridgeToken}
}

// scopeCredentials keeps user units in the user's bridge even while the
// session is elevated: elevation never changes the user identity.
func scopeCredentials(current session.Session, scope string) auth.Credentials {
	if scope == "user" {
		return auth.Credentials{Token: current.Identity.BridgeToken}
	}
	return sessionCredentials(current)
}

func adminCredentials(current session.Session) auth.Credentials {
	return auth.Credentials{AdminToken: current.Identity.AdminToken}
}

func userCredentials(current session.Session) auth.Credentials {
	return auth.Credentials{Token: current.Identity.BridgeToken}
}
