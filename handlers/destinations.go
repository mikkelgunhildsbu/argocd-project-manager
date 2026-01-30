package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"

	"github.com/example/argocd-destination-api/argocd"
	"github.com/example/argocd-destination-api/audit"
	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/api/errors"
)

var projectNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// DestinationHandler handles destination-related HTTP requests
type DestinationHandler struct {
	client      *argocd.Client
	auditLogger *audit.Logger
}

// DestinationRequest represents a request to add or remove a destination
type DestinationRequest struct {
	Server      string `json:"server"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description"`
}

// ErrorResponse represents a JSON error response
type ErrorResponse struct {
	Message string `json:"message"`
}

// DestinationsResponse represents a list of destinations
type DestinationsResponse struct {
	Destinations []argocd.Destination `json:"destinations"`
}

// NewDestinationHandler creates a new destination handler
func NewDestinationHandler(client *argocd.Client, auditLogger *audit.Logger) *DestinationHandler {
	return &DestinationHandler{
		client:      client,
		auditLogger: auditLogger,
	}
}

// ListDestinations handles GET /projects/{project}/destinations
func (h *DestinationHandler) ListDestinations(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")

	if !h.validateProjectName(w, project) {
		return
	}

	destinations, _, err := h.client.GetDestinations(r.Context(), project)
	if err != nil {
		h.handleK8sError(w, err, project)
		return
	}

	// Ensure we return an empty array, not null
	if destinations == nil {
		destinations = []argocd.Destination{}
	}

	writeJSON(w, http.StatusOK, DestinationsResponse{Destinations: destinations})
}

// AddDestination handles POST /projects/{project}/destinations
func (h *DestinationHandler) AddDestination(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")

	if !h.validateProjectName(w, project) {
		return
	}

	var req DestinationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !h.validateDestinationRequest(w, req) {
		return
	}

	dest := argocd.Destination{
		Server:    req.Server,
		Namespace: req.Namespace,
		Name:      req.Name,
	}

	err := h.client.AddDestination(r.Context(), project, dest)
	if err != nil {
		if errors.IsConflict(err) {
			writeJSONError(w, http.StatusConflict, "resource was modified, please retry")
			return
		}
		h.handleK8sError(w, err, project)
		return
	}

	// Write audit log entry
	if err := h.auditLogger.Log(audit.Entry{
		Action:      "add",
		Project:     project,
		Server:      req.Server,
		Namespace:   req.Namespace,
		Name:        req.Name,
		Description: req.Description,
		UserAgent:   r.UserAgent(),
		RemoteAddr:  r.RemoteAddr,
	}); err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}

	log.Printf("Added destination to project %s: server=%s namespace=%s name=%s reason=%q",
		project, dest.Server, dest.Namespace, dest.Name, req.Description)

	writeJSON(w, http.StatusCreated, dest)
}

// RemoveDestination handles DELETE /projects/{project}/destinations
func (h *DestinationHandler) RemoveDestination(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")

	if !h.validateProjectName(w, project) {
		return
	}

	var req DestinationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !h.validateDestinationRequest(w, req) {
		return
	}

	dest := argocd.Destination{
		Server:    req.Server,
		Namespace: req.Namespace,
		Name:      req.Name,
	}

	err := h.client.RemoveDestination(r.Context(), project, dest)
	if err != nil {
		if errors.IsConflict(err) {
			writeJSONError(w, http.StatusConflict, "resource was modified, please retry")
			return
		}
		h.handleK8sError(w, err, project)
		return
	}

	// Write audit log entry
	if err := h.auditLogger.Log(audit.Entry{
		Action:      "remove",
		Project:     project,
		Server:      req.Server,
		Namespace:   req.Namespace,
		Name:        req.Name,
		Description: req.Description,
		UserAgent:   r.UserAgent(),
		RemoteAddr:  r.RemoteAddr,
	}); err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}

	log.Printf("Removed destination from project %s: server=%s namespace=%s name=%s reason=%q",
		project, dest.Server, dest.Namespace, dest.Name, req.Description)

	w.WriteHeader(http.StatusNoContent)
}

// validateProjectName validates the project name and writes an error if invalid
func (h *DestinationHandler) validateProjectName(w http.ResponseWriter, project string) bool {
	if project == "" {
		writeJSONError(w, http.StatusBadRequest, "project name is required")
		return false
	}

	if !projectNameRegex.MatchString(project) {
		writeJSONError(w, http.StatusBadRequest, "project name must contain only alphanumeric characters, dashes, and underscores")
		return false
	}

	return true
}

// validateDestinationRequest validates a destination request and writes an error if invalid
func (h *DestinationHandler) validateDestinationRequest(w http.ResponseWriter, req DestinationRequest) bool {
	if req.Server == "" {
		writeJSONError(w, http.StatusBadRequest, "server is required")
		return false
	}

	if req.Namespace == "" {
		writeJSONError(w, http.StatusBadRequest, "namespace is required")
		return false
	}

	if req.Server == "*" {
		writeJSONError(w, http.StatusBadRequest, "wildcard server (*) is not allowed")
		return false
	}

	if req.Namespace == "*" {
		writeJSONError(w, http.StatusBadRequest, "wildcard namespace (*) is not allowed")
		return false
	}

	if req.Description == "" {
		writeJSONError(w, http.StatusBadRequest, "description is required (explain why this change is being made)")
		return false
	}

	return true
}

// handleK8sError handles Kubernetes API errors and writes appropriate HTTP responses
func (h *DestinationHandler) handleK8sError(w http.ResponseWriter, err error, project string) {
	if errors.IsNotFound(err) {
		writeJSONError(w, http.StatusNotFound, "project not found: "+project)
		return
	}

	if errors.IsForbidden(err) {
		writeJSONError(w, http.StatusForbidden, "access denied to project: "+project)
		return
	}

	log.Printf("Kubernetes API error: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Message: message})
}
