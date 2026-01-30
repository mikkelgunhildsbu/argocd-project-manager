package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/argocd-destination-api/argocd"
	"github.com/example/argocd-destination-api/audit"
	"github.com/example/argocd-destination-api/handlers"
	"github.com/example/argocd-destination-api/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// Get configuration from environment
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("API_KEY environment variable is required")
	}

	namespace := os.Getenv("ARGOCD_NAMESPACE")
	if namespace == "" {
		namespace = "argocd"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	auditLogPath := os.Getenv("AUDIT_LOG_PATH")
	if auditLogPath == "" {
		auditLogPath = "/var/log/audit/audit.log"
	}

	// Initialize audit logger
	auditLogger, err := audit.NewLogger(auditLogPath)
	if err != nil {
		log.Fatalf("Failed to create audit logger: %v", err)
	}
	defer auditLogger.Close()

	// Initialize ArgoCD client
	client, err := argocd.NewClient(namespace)
	if err != nil {
		log.Fatalf("Failed to create ArgoCD client: %v", err)
	}

	// Initialize handlers
	destHandler := handlers.NewDestinationHandler(client, auditLogger)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.RequestLogger)
	r.Use(chimiddleware.Recoverer)

	// Health check endpoint (no auth required)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKeyAuth(apiKey))

		r.Route("/projects/{project}/destinations", func(r chi.Router) {
			r.Get("/", destHandler.ListDestinations)
			r.Post("/", destHandler.AddDestination)
			r.Delete("/", destHandler.RemoveDestination)
		})
	})

	log.Printf("Starting server on :%s", port)
	log.Printf("ArgoCD namespace: %s", namespace)
	log.Printf("Audit log path: %s", auditLogPath)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
