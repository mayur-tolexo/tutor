package httpapi

import (
	"net/http"
	"time"

	"github.com/mayur-tolexo/tutor/internal/store"
)

type meJSON struct {
	SignedIn    bool   `json:"signed_in"`
	DisplayName string `json:"display_name,omitempty"`
	LangPref    string `json:"lang_pref"`
	DeviceID    string `json:"device_id"`
}

// toMe renders the identity for the client.
func toMe(id identity) meJSON {
	m := meJSON{LangPref: id.Lang(), DeviceID: id.Device.ID}
	if id.Student != nil {
		m.SignedIn = true
		m.DisplayName = id.Student.DisplayName
	}
	return m
}

// handleMe returns who the request is from.
func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, id identity) error {
	writeJSON(w, http.StatusOK, toMe(id))
	return nil
}

// handleUpdateMe changes the tutor language for the student if signed in,
// otherwise for the device.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request, id identity) error {
	var body struct {
		LangPref string `json:"lang_pref"`
	}
	if err := decodeJSON(r, &body, 4<<10); err != nil {
		return err
	}
	if body.LangPref != "hinglish" && body.LangPref != "en" {
		return errInvalid("lang_pref must be hinglish or en")
	}
	if id.Student != nil {
		if err := s.Store.SetStudentLang(r.Context(), id.Student.ID, body.LangPref); err != nil {
			return err
		}
		id.Student.LangPref = body.LangPref
	} else {
		if err := s.Store.SetDeviceLang(r.Context(), id.Device.ID, body.LangPref); err != nil {
			return err
		}
		id.Device.LangPref = body.LangPref
	}
	writeJSON(w, http.StatusOK, toMe(id))
	return nil
}

// handleProgress lists the owner's standing per exercise.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request, id identity) error {
	prog, err := s.Store.ListProgress(r.Context(), id.Owner())
	if err != nil {
		return err
	}
	type progJSON struct {
		Status   string     `json:"status"`
		PassedAt *time.Time `json:"passed_at,omitempty"`
	}
	out := map[string]progJSON{}
	for k, p := range prog {
		out[k] = progJSON{Status: p.Status, PassedAt: p.PassedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"exercises": out})
	return nil
}

// handleGoogle verifies a Google ID token, upserts the student, links the
// device (merging anonymous progress), and issues the session cookie.
func (s *Server) handleGoogle(w http.ResponseWriter, r *http.Request, id identity) error {
	if s.Google == nil {
		return errNotFound("sign-in is not enabled")
	}
	var body struct {
		IDToken string `json:"id_token"`
	}
	if err := decodeJSON(r, &body, 16<<10); err != nil {
		return err
	}
	if body.IDToken == "" {
		return errInvalid("id_token is required")
	}
	g, err := s.Google.Verify(r.Context(), body.IDToken)
	if err != nil {
		return errUnauthorized("invalid Google token")
	}
	st, err := s.Store.UpsertStudent(r.Context(), store.Student{GoogleSub: g.Sub, Email: g.Email, DisplayName: g.Name})
	if err != nil {
		return err
	}
	if err := s.Store.LinkDevice(r.Context(), id.Device.ID, st.ID); err != nil {
		return err
	}
	s.Sessions.Issue(w, st.ID)
	id.Student = &st
	writeJSON(w, http.StatusOK, toMe(id))
	return nil
}

// handleLogout clears the session; the device keeps its own progress.
func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request, _ identity) error {
	s.Sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
