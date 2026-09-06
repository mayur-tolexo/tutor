package httpapi

import (
	"net/http"

	"github.com/mayur-tolexo/tutor/internal/content"
)

// handleConfig tells the client which optional features are enabled.
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	clientID := ""
	if s.Google != nil {
		clientID = s.GoogleClientID
	}
	writeJSON(w, http.StatusOK, map[string]string{"google_client_id": clientID})
}

type trackJSON struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Units []unitJSON `json:"units"`
}

type unitJSON struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Exercises []exerciseSummJSON `json:"exercises"`
}

type exerciseSummJSON struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Difficulty int    `json:"difficulty"`
	MustPass   bool   `json:"must_pass"`
}

// handleTracks returns every track with its unit/exercise outline.
func (s *Server) handleTracks(w http.ResponseWriter, _ *http.Request) {
	out := make([]trackJSON, 0, len(s.Library.Tracks))
	for _, t := range s.Library.Tracks {
		tj := trackJSON{ID: t.ID, Title: t.Title, Units: []unitJSON{}}
		for _, u := range t.Units {
			must := map[string]bool{}
			for _, id := range u.MustPass {
				must[id] = true
			}
			uj := unitJSON{ID: u.ID, Title: u.Title, Exercises: []exerciseSummJSON{}}
			for _, id := range u.Exercises {
				ex := s.Library.Exercise(id)
				if ex == nil {
					continue
				}
				uj.Exercises = append(uj.Exercises, exerciseSummJSON{ID: ex.ID, Title: ex.Title, Difficulty: ex.Difficulty, MustPass: must[id]})
			}
			tj.Units = append(tj.Units, uj)
		}
		out = append(out, tj)
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"tracks": out})
}

type caseJSON struct {
	ID     string `json:"id"`
	Stdin  string `json:"stdin"`
	Stdout string `json:"stdout"`
}

type exerciseJSON struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	StatementMD  string     `json:"statement_md"`
	Starter      string     `json:"starter"`
	Concepts     []string   `json:"concepts"`
	Syllabus     string     `json:"syllabus"`
	Difficulty   int        `json:"difficulty"`
	VisibleCases []caseJSON `json:"visible_cases"`
}

// handleExercise returns one exercise without its solution, hints, or hidden
// cases.
func (s *Server) handleExercise(w http.ResponseWriter, r *http.Request) {
	ex := s.Library.Exercise(r.PathValue("id"))
	if ex == nil {
		s.writeError(w, errNotFound("exercise not found"))
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, toExerciseJSON(ex))
}

// toExerciseJSON projects the student-visible parts of an exercise.
func toExerciseJSON(ex *content.Exercise) exerciseJSON {
	concepts := ex.Concepts
	if concepts == nil {
		concepts = []string{}
	}
	out := exerciseJSON{
		ID: ex.ID, Title: ex.Title, StatementMD: ex.Statement, Starter: ex.Starter,
		Concepts: concepts, Syllabus: ex.Syllabus, Difficulty: ex.Difficulty, VisibleCases: []caseJSON{},
	}
	for _, c := range ex.VisibleCases() {
		out.VisibleCases = append(out.VisibleCases, caseJSON{ID: c.ID, Stdin: c.Stdin, Stdout: c.Stdout})
	}
	return out
}
