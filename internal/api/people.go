package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/people"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// peopleRefreshKickTimeout bounds the background people derivation kicked
// off by POST /api/people/refresh so a hung upstream fetch cannot leak a
// goroutine forever.
const peopleRefreshKickTimeout = 2 * time.Minute

// personResponse is the composed JSON shape returned by the people read
// endpoints: the person fields, plus the resolved company when available.
type personResponse struct {
	model.Person
	Company *model.Company `json:"company,omitempty"`
}

type companyResponse struct {
	model.Company
	PeopleCount int64 `json:"people_count"`
}

type companyWithPeopleResponse struct {
	model.Company
	People []model.Person `json:"people"`
}

type mergeCompaniesRequest struct {
	Into string `json:"into"`
}

type mergePeopleRequest struct {
	Into string `json:"into"`
}

// kickPeopleRefresh runs people.DerivePeople in the background, logging and
// swallowing its error: the HTTP response has already gone out by the time
// this runs, so there is no one left to report the error to.
func kickPeopleRefresh(st *store.Store, uid string) {
	go func(uid string) {
		ctx, cancel := context.WithTimeout(context.Background(), peopleRefreshKickTimeout)
		defer cancel()
		if err := people.DerivePeople(ctx, st, uid); err != nil {
			log.Printf("people refresh %s: %v", uid, err)
		}
	}(uid)
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	q := r.URL.Query().Get("q")
	people, err := s.deps.Store.ListPeople(r.Context(), uid, q)
	if err != nil {
		log.Printf("handleListPeople: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	companyIDs := make(map[string]struct{})
	for _, person := range people {
		if person.CompanyID != nil {
			companyIDs[*person.CompanyID] = struct{}{}
		}
	}

	companies := make(map[string]model.Company)
	if len(companyIDs) > 0 {
		listed, err := s.deps.Store.ListCompanies(r.Context(), uid)
		if err != nil {
			log.Printf("handleListPeople: list companies: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, company := range listed {
			if _, ok := companyIDs[company.ID]; ok {
				companies[company.ID] = company
			}
		}
	}

	out := make([]personResponse, 0, len(people))
	for _, person := range people {
		resp := personResponse{Person: person}
		if person.CompanyID != nil {
			if company, ok := companies[*person.CompanyID]; ok {
				company := company
				resp.Company = &company
			}
		}
		out = append(out, resp)
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListCompanies(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	q := r.URL.Query().Get("q")
	companies, err := s.deps.Store.ListCompaniesWithPeopleCount(r.Context(), uid, q)
	if err != nil {
		log.Printf("handleListCompanies: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]companyResponse, 0, len(companies))
	for _, company := range companies {
		out = append(out, companyResponse{
			Company:     company.Company,
			PeopleCount: company.PeopleCount,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetPerson(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validPersonID(w, id) {
		return
	}

	person, err := s.deps.Store.GetPerson(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleGetPerson: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := personResponse{Person: person}
	if person.CompanyID != nil {
		companies, err := s.deps.Store.ListCompanies(r.Context(), uid)
		if err != nil {
			log.Printf("handleGetPerson: list companies: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, company := range companies {
			if company.ID == *person.CompanyID {
				company := company
				resp.Company = &company
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdatePerson(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validPersonID(w, id) {
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var displayName *string
	var companyID *string
	clearCompany := false

	if v, ok := raw["display_name"]; ok {
		if string(v) == "null" {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		displayName = &s
	}

	if v, ok := raw["company_id"]; ok {
		if string(v) == "null" {
			clearCompany = true
		} else {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			s = strings.TrimSpace(s)
			if s == "" {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			if _, err := uuid.Parse(s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			companyID = &s
		}
	}

	person, err := s.deps.Store.UpdatePerson(r.Context(), uid, id, displayName, companyID, clearCompany)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleUpdatePerson: update person: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := personResponse{Person: person}
	if person.CompanyID != nil {
		companies, err := s.deps.Store.ListCompanies(r.Context(), uid)
		if err != nil {
			log.Printf("handleUpdatePerson: list companies: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, company := range companies {
			if company.ID == *person.CompanyID {
				company := company
				resp.Company = &company
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMergePeople(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	fromID := chi.URLParam(r, "id")
	if !validPersonID(w, fromID) {
		return
	}

	var req mergePeopleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Into) == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if _, err := uuid.Parse(req.Into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Into == fromID {
		writeError(w, http.StatusBadRequest, "invalid merge")
		return
	}

	person, err := s.deps.Store.MergePeople(r.Context(), uid, fromID, req.Into)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleMergePeople: merge people: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	resp := personResponse{Person: person}
	if person.CompanyID != nil {
		companies, err := s.deps.Store.ListCompanies(r.Context(), uid)
		if err != nil {
			log.Printf("handleMergePeople: list companies: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, company := range companies {
			if company.ID == *person.CompanyID {
				company := company
				resp.Company = &company
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validPersonID(w, id) {
		return
	}

	if err := s.deps.Store.DeletePerson(r.Context(), uid, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleDeletePerson: delete person: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListPersonNotes(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validPersonID(w, id) {
		return
	}

	person, err := s.deps.Store.GetPerson(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleListPersonNotes: get person: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	notes, err := s.deps.Store.NotesForPerson(r.Context(), uid, person.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleListPersonNotes: notes for person: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, notes)
}

func (s *Server) handleGetCompany(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if !validCompanyID(w, id) {
		return
	}

	company, err := s.deps.Store.GetCompany(r.Context(), uid, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleGetCompany: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	people, err := s.deps.Store.ListPeopleByCompany(r.Context(), uid, id)
	if err != nil {
		log.Printf("handleGetCompany: list people by company: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, companyWithPeopleResponse{
		Company: company,
		People:  people,
	})
}

func (s *Server) handleMergeCompanies(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	fromID := chi.URLParam(r, "id")
	if !validCompanyID(w, fromID) {
		return
	}

	var req mergeCompaniesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Into) == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if _, err := uuid.Parse(req.Into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	if err := s.deps.Store.MergeCompanies(r.Context(), uid, fromID, req.Into); errors.Is(err, store.ErrInvalidMerge) {
		writeError(w, http.StatusBadRequest, "invalid merge")
		return
	} else if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleMergeCompanies: merge companies: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	company, err := s.deps.Store.GetCompany(r.Context(), uid, req.Into)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		log.Printf("handleMergeCompanies: get surviving company: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	people, err := s.deps.Store.ListPeopleByCompany(r.Context(), uid, req.Into)
	if err != nil {
		log.Printf("handleMergeCompanies: list people by company: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, companyWithPeopleResponse{
		Company: company,
		People:  people,
	})
}

// handleRefreshPeople kicks an async re-derivation of the calling user's own
// people records from calendar events. It responds immediately; the actual
// derivation continues in the background.
func (s *Server) handleRefreshPeople(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	kickPeopleRefresh(s.deps.Store, uid)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// validPersonID reports whether id is a syntactically valid UUID. People
// handlers return 400 for malformed ids so callers can distinguish bad input
// from a missing record.
func validPersonID(w http.ResponseWriter, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return false
	}
	return true
}

func validCompanyID(w http.ResponseWriter, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return false
	}
	return true
}
