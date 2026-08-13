package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/velopulent/tako/internal/platform"
	"github.com/velopulent/tako/internal/session"
)

func (server *Server) loginHistory(writer http.ResponseWriter, request *http.Request) {
	current := request.Context().Value(sessionKey{}).(session.Session)
	username := chi.URLParam(request, "username")
	if username != current.Identity.Username && !hasAdministrativeAccess(current) {
		problem(writer, http.StatusForbidden, "administrative-access-required", "Administrative access is required to inspect another identity's login history")
		return
	}
	query, err := parseLoginHistoryQuery(request, username)
	if err != nil {
		problem(writer, http.StatusBadRequest, "invalid-login-history-query", "Unsupported identity, cursor, outcome, or time range")
		return
	}
	if server.queryLoginHistory == nil {
		problem(writer, http.StatusServiceUnavailable, "login-history-unavailable", "The journal login history service is unavailable")
		return
	}
	page, err := server.queryLoginHistory(request.Context(), query)
	if err != nil {
		if request.Context().Err() != nil {
			return
		}
		if errors.Is(err, platform.ErrInvalidLoginHistoryQuery) {
			problem(writer, http.StatusBadRequest, "invalid-login-history-query", "Unsupported identity, cursor, outcome, or time range")
			return
		}
		problem(writer, http.StatusServiceUnavailable, "login-history-unavailable", "The journal login history could not be queried")
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseLoginHistoryQuery(request *http.Request, username string) (platform.LoginHistoryQuery, error) {
	values := request.URL.Query()
	query := platform.LoginHistoryQuery{Username: username, Limit: 50, Outcome: values.Get("outcome")}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return platform.LoginHistoryQuery{}, err
		}
		query.Limit = limit
	}
	if raw := values.Get("cursor"); raw != "" {
		cursor, err := platform.DecodeJournalCursor(raw)
		if err != nil {
			return platform.LoginHistoryQuery{}, err
		}
		query.Cursor = cursor
	}
	for key, destination := range map[string]*time.Time{"since": &query.Since, "until": &query.Until} {
		if raw := values.Get(key); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return platform.LoginHistoryQuery{}, err
			}
			*destination = parsed
		}
	}
	if err := query.Validate(); err != nil {
		return platform.LoginHistoryQuery{}, err
	}
	return query, nil
}
