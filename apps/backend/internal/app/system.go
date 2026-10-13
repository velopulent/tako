package app

import (
	"net/http"

	"github.com/velopulent/tako/internal/platform"
)

func (server *Server) storage(writer http.ResponseWriter, request *http.Request) {
	var snapshot platform.StorageSnapshot
	if err := server.broker.Call(request.Context(), "storage.read", sessionCredentials(currentSession(request)), nil, &snapshot); err != nil {
		fail(writer, err, "storage-unavailable")
		return
	}
	if snapshot.Filesystems == nil {
		snapshot.Filesystems = []platform.Filesystem{}
	}
	if snapshot.Devices == nil {
		snapshot.Devices = []platform.StorageDevice{}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"items": snapshot.Filesystems, "devices": snapshot.Devices,
		"fingerprint": snapshot.Fingerprint, "readOnly": snapshot.ReadOnly, "reason": snapshot.Reason,
	})
}

func (server *Server) network(writer http.ResponseWriter, request *http.Request) {
	var snapshot platform.NetworkSnapshot
	if err := server.broker.Call(request.Context(), "network.read", sessionCredentials(currentSession(request)), nil, &snapshot); err != nil {
		fail(writer, err, "network-unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"items": snapshot.Interfaces, "addresses": snapshot.Addresses, "routes": snapshot.Routes,
		"dns": snapshot.DNS, "ownership": snapshot.Ownership, "fingerprint": snapshot.Fingerprint,
	})
}

// adminOperation runs an administrative preview or apply whose payload is
// passed to sessiond unchanged. force sets the action for preview endpoints.
func adminOperation[T, R any](server *Server, writer http.ResponseWriter, request *http.Request, operation, invalidCode, fallback string, setAction func(*T) string, apply bool) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	payload, ok := decodeBody[T](writer, request, 16<<10, invalidCode)
	if !ok {
		return
	}
	action := setAction(&payload)
	var result R
	err := server.broker.Call(request.Context(), operation, adminCredentials(current), payload, &result)
	if apply {
		server.audit(request, operation+"/"+action, err, true)
	}
	if err != nil {
		fail(writer, err, fallback)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (server *Server) storagePreview(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.StorageOperation, platform.StorageState](server, writer, request, "storage.apply", "invalid-storage-operation", "storage-operation-failed",
		func(operation *platform.StorageOperation) string {
			operation.Action = "preview"
			return operation.Action
		}, false)
}

func (server *Server) storageApply(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.StorageOperation, platform.StorageState](server, writer, request, "storage.apply", "invalid-storage-operation", "storage-operation-failed",
		func(operation *platform.StorageOperation) string { return operation.Action }, true)
}

func (server *Server) networkPreview(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.NetworkOperation, platform.NetworkState](server, writer, request, "network.apply", "invalid-network-operation", "network-operation-failed",
		func(operation *platform.NetworkOperation) string {
			operation.Action = "preview"
			return operation.Action
		}, false)
}

func (server *Server) networkApply(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.NetworkOperation, platform.NetworkState](server, writer, request, "network.apply", "invalid-network-operation", "network-operation-failed",
		func(operation *platform.NetworkOperation) string { return operation.Action }, true)
}

func (server *Server) firewallStatus(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	var state platform.FirewallState
	if err := server.broker.Call(request.Context(), "firewall.apply", adminCredentials(current), platform.FirewallOperation{Backend: "auto", Action: "preview"}, &state); err != nil {
		fail(writer, err, "firewall-operation-failed")
		return
	}
	writeJSON(writer, http.StatusOK, state.Snapshot)
}

func (server *Server) firewallPreview(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.FirewallOperation, platform.FirewallState](server, writer, request, "firewall.apply", "invalid-firewall-operation", "firewall-operation-failed",
		func(operation *platform.FirewallOperation) string {
			operation.Action = "preview"
			return operation.Action
		}, false)
}

func (server *Server) firewallApply(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.FirewallOperation, platform.FirewallState](server, writer, request, "firewall.apply", "invalid-firewall-operation", "firewall-operation-failed",
		func(operation *platform.FirewallOperation) string { return operation.Action }, true)
}

func (server *Server) securityStatus(writer http.ResponseWriter, request *http.Request) {
	current := currentSession(request)
	if !requireAdmin(writer, current) {
		return
	}
	var status platform.SecurityStatus
	if err := server.broker.Call(request.Context(), "security.preview", adminCredentials(current), platform.SecurityOperation{Action: "inspect", Framework: "SELinux"}, &status); err != nil {
		fail(writer, err, "security-operation-failed")
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (server *Server) securityPreview(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.SecurityOperation, platform.SecurityStatus](server, writer, request, "security.preview", "invalid-security-operation", "security-operation-failed",
		func(operation *platform.SecurityOperation) string { return operation.Action }, false)
}

func (server *Server) securityApply(writer http.ResponseWriter, request *http.Request) {
	adminOperation[platform.SecurityOperation, platform.SecurityStatus](server, writer, request, "security.apply", "invalid-security-operation", "security-operation-failed",
		func(operation *platform.SecurityOperation) string { return operation.Action }, true)
}
