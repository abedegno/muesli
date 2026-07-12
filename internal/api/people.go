package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// personResponse is the composed JSON shape returned by the people read
// endpoints: the person fields, plus the resolved company when available.
type personResponse struct {
	model.Person
	Company *model.Company `json:"company,omitempty"`
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	uid, _ := userIDFromContext(r.Context())
	people, err := s.deps.Store.ListPeople(r.Context(), uid)
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
