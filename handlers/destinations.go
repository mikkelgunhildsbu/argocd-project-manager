package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"

	"github.com/example/argocd-destination-api/argocd"
	"github.com/example/argocd-destination-api/audit"
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
	Project     string `json:"project"`
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

// ProjectsResponse represents a list of projects
type ProjectsResponse struct {
	Projects []argocd.Project `json:"projects"`
}

// NewDestinationHandler creates a new destination handler
func NewDestinationHandler(client *argocd.Client, auditLogger *audit.Logger) *DestinationHandler {
	return &DestinationHandler{
		client:      client,
		auditLogger: auditLogger,
	}
}

// ListProjects handles GET /projects
func (h *DestinationHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.client.ListProjects(r.Context())
	if err != nil {
		log.Printf("Failed to list projects: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	if projects == nil {
		projects = []argocd.Project{}
	}

	writeJSON(w, http.StatusOK, ProjectsResponse{Projects: projects})
}

// ListDestinationsRequest represents a request to list destinations
type ListDestinationsRequest struct {
	Project string `json:"project"`
}

// ListDestinations handles POST /destinations/list
func (h *DestinationHandler) ListDestinations(w http.ResponseWriter, r *http.Request) {
	var req ListDestinationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if !h.validateProjectName(w, req.Project) {
		return
	}

	destinations, _, err := h.client.GetDestinations(r.Context(), req.Project)
	if err != nil {
		h.handleK8sError(w, err, req.Project)
		return
	}

	// Ensure we return an empty array, not null
	if destinations == nil {
		destinations = []argocd.Destination{}
	}

	writeJSON(w, http.StatusOK, DestinationsResponse{Destinations: destinations})
}

// AddDestination handles POST /destinations
func (h *DestinationHandler) AddDestination(w http.ResponseWriter, r *http.Request) {
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

	err := h.client.AddDestination(r.Context(), req.Project, dest)
	if err != nil {
		if errors.IsConflict(err) {
			writeJSONError(w, http.StatusConflict, "resource was modified, please retry")
			return
		}
		h.handleK8sError(w, err, req.Project)
		return
	}

	// Write audit log entry
	if err := h.auditLogger.Log(audit.Entry{
		Action:      "add",
		Project:     req.Project,
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
		req.Project, dest.Server, dest.Namespace, dest.Name, req.Description)

	writeJSON(w, http.StatusCreated, dest)
}

// RemoveDestination handles DELETE /destinations
func (h *DestinationHandler) RemoveDestination(w http.ResponseWriter, r *http.Request) {
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

	err := h.client.RemoveDestination(r.Context(), req.Project, dest)
	if err != nil {
		if errors.IsConflict(err) {
			writeJSONError(w, http.StatusConflict, "resource was modified, please retry")
			return
		}
		h.handleK8sError(w, err, req.Project)
		return
	}

	// Write audit log entry
	if err := h.auditLogger.Log(audit.Entry{
		Action:      "remove",
		Project:     req.Project,
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
		req.Project, dest.Server, dest.Namespace, dest.Name, req.Description)

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
	if !h.validateProjectName(w, req.Project) {
		return false
	}

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
