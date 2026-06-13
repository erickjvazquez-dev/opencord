package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeCreds(w, r)
	if !ok {
		return
	}
	if !usernameRe.MatchString(c.Username) || len(c.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "username must be 3-32 chars [a-zA-Z0-9_]; password must be >= 6 chars")
		return
	}
	u, err := s.Register(r.Context(), c.Username, c.Password)
	if errors.Is(err, ErrUserExists) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create account")
		return
	}
	s.respondWithToken(w, u)
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	c, ok := decodeCreds(w, r)
	if !ok {
		return
	}
	u, err := s.Login(r.Context(), c.Username, c.Password)
	if errors.Is(err, ErrInvalidLogin) {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	s.respondWithToken(w, u)
}

func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFrom(r.Context())
	writeJSON(w, http.StatusOK, u)
}

func (s *Service) respondWithToken(w http.ResponseWriter, u User) {
	token, err := s.Issue(u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not issue token")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{Token: token, User: u})
}

func decodeCreds(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var c credentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return credentials{}, false
	}
	return c, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
