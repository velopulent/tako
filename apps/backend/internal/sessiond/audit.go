package sessiond

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/velopulent/tako/internal/audit"
	"github.com/velopulent/tako/internal/auth"
	"go.uber.org/zap"
)

// auditedOperations change the host or a sign-in. Reads are never audited.
var auditedOperations = map[string]bool{
	"session.conversation": true, "session.close": true, "admin.authorize": true, "terminal.open": true,
	"services.action": true, "timers.apply": true, "overrides.apply": true,
	"host.configure": true, "power.request": true, "processes.signal": true,
	"accounts.local": true, "groups.membership": true, "groups.admin-role": true, "groups.local": true,
	"passwords.change": true, "ssh-keys.apply": true,
	"storage.apply": true, "network.apply": true, "firewall.apply": true, "security.apply": true,
	"updates.refresh": true, "updates.apply": true, "kpatch.apply": true, "files.apply": true,
}

// auditTargetFields are the payload fields a record may name, in order. This
// is an allowlist: passwords, keys, file content, environment variables,
// mount options, and commands can never reach the journal.
var auditTargetFields = []string{
	"scope", "unit", "name", "username", "group", "role", "signal", "tree",
	"device", "target", "filesystem", "backend", "interface", "connection",
	"address", "gateway", "zone", "defaultZone", "service", "port", "source",
	"framework", "boolean", "value", "profile", "path", "destination",
	"hostname", "timezone", "ntpEnabled", "fingerprint", "force", "apply",
}

// readOnlyFileActions are files.apply actions that change nothing.
var readOnlyFileActions = map[string]bool{"list": true, "stat": true, "read": true, "search": true, "upload-status": true}

type auditPlan struct {
	operation string
	action    string
	target    string
	username  string
}

// planAudit decides from the request payload whether the call is audited and
// what it names. It runs before the operation clears its payload.
func planAudit(operation string, payload json.RawMessage) (auditPlan, bool) {
	if !auditedOperations[operation] {
		return auditPlan{}, false
	}
	var fields map[string]json.RawMessage
	if len(payload) > 0 && json.Unmarshal(payload, &fields) != nil {
		fields = nil
	}
	plan := auditPlan{operation: operation, action: stringField(fields, "action")}
	var preview bool
	_ = json.Unmarshal(fields["preview"], &preview)
	if preview || plan.action == "preview" || plan.action == "inspect" {
		return auditPlan{}, false
	}
	if operation == "files.apply" {
		if readOnlyFileActions[plan.action] {
			return auditPlan{}, false
		}
		// Uploads arrive in chunks; record the upload once, when it starts.
		var offset int64
		_ = json.Unmarshal(fields["offset"], &offset)
		if plan.action == "write-chunk" && offset != 0 {
			return auditPlan{}, false
		}
	}
	if operation == "session.conversation" {
		plan.username = stringField(fields, "username")
	}
	var target []string
	for _, key := range auditTargetFields {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		if key == "target" && operation == "processes.signal" {
			var process struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(raw, &process) == nil && process.PID > 0 {
				target = append(target, fmt.Sprintf("pid=%d", process.PID))
			}
			continue
		}
		if value := scalarField(raw); value != "" {
			target = append(target, key+"="+value)
		}
	}
	plan.target = strings.Join(target, " ")
	return plan, true
}

func stringField(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}

// scalarField renders a string, number, or boolean; anything else is skipped.
func scalarField(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		if len(typed) > 256 {
			typed = typed[:256] + "…"
		}
		return strings.Map(func(character rune) rune {
			if character < 0x20 || character == 0x7f {
				return ' '
			}
			return character
		}, typed)
	case float64, bool:
		return fmt.Sprint(typed)
	default:
		return ""
	}
}

// recordAudit writes the outcome of an audited call. Login rounds that only
// return the next prompt are not outcomes and are skipped.
func (c *call) recordAudit(plan auditPlan, result any, err error, code string) {
	record := audit.Record{Actor: c.identity.Username, UID: c.identity.UID, Administrative: c.administrative, Operation: plan.operation, Action: plan.action, Target: plan.target, Result: "succeeded"}
	if plan.operation == "session.conversation" {
		response, _ := result.(auth.ConversationResponse)
		if err == nil && response.Identity == nil {
			return
		}
		record.Actor, record.UID = plan.username, -1
		if response.Identity != nil {
			record.Actor, record.UID = response.Identity.Username, response.Identity.UID
		}
	}
	if record.Actor == "" {
		record.Actor = "unknown"
	}
	if err != nil {
		record.Result, record.ErrorCode = "failed", code
	}
	c.emitAudit(record)
}

func (c *call) emitAudit(record audit.Record) {
	if err := audit.Emit(record); err != nil {
		// Without journald the record still lands in the service log.
		c.handler.logger.Info(record.Message(), zap.Bool("audit", true), zap.NamedError("journal_error", err))
	}
}
