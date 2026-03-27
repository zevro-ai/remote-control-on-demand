package httpapi

import (
	"errors"
	"net/http"

	"github.com/zevro-ai/remote-control-on-demand/internal/httpauth"
)

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	status := s.auth.Status(r)
	resp := authStatusResponse{
		Mode:          status.Mode,
		TokenEnabled:  status.TokenEnabled,
		Authenticated: status.Authenticated,
		LoginURL:      status.LoginURL,
		LogoutURL:     status.LogoutURL,
	}
	if status.Provider != nil {
		resp.Provider = &authProviderResponse{
			ID:          status.Provider.ID,
			DisplayName: status.Provider.DisplayName,
		}
	}
	if status.User != nil {
		resp.User = &authUserResponse{
			Provider: status.User.Provider,
			Subject:  status.User.Subject,
			Login:    status.User.Login,
			Name:     status.User.Name,
			Email:    status.User.Email,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.HandleLogin(w, r); err != nil {
		writeAuthError(w, err)
	}
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.HandleCallback(w, r); err != nil {
		writeAuthError(w, err)
	}
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.HandleLogout(w, r)
}

func writeAuthError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, httpauth.ErrExternalAuthDisabled):
		status = http.StatusNotFound
	case errors.Is(err, httpauth.ErrInvalidAuthState), errors.Is(err, httpauth.ErrExpiredAuthState):
		status = http.StatusUnauthorized
	}
	writeJSON(w, status, errorResponse{Error: err.Error()})
}
