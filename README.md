# ArgoCD Destination API

A Go HTTP API service that manages destinations in ArgoCD AppProject resources. The service runs inside a Kubernetes cluster and uses a ServiceAccount to patch AppProject CRDs.

## Overview

This API provides a simple interface to add, remove, and list destinations on ArgoCD AppProjects without requiring direct access to kubectl or the ArgoCD UI. All changes require a description explaining why the change is being made, and every modification is logged to a persistent audit log.

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/destinations` | Add a destination to an AppProject |
| `DELETE` | `/destinations` | Remove a destination from an AppProject |
| `POST` | `/destinations/list` | List all destinations for an AppProject |
| `GET` | `/health` | Health check endpoint (no auth required) |

## Request/Response Format

### Add or Remove a Destination

**Request body:**
```json
{
  "project": "my-project",
  "server": "https://customer-cluster.example.com",
  "namespace": "production",
  "name": "customer-prod-cluster",
  "description": "Adding production cluster for customer onboarding (TICKET-123)"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `project` | Yes | The ArgoCD AppProject name |
| `server` | Yes | The Kubernetes API server URL (cannot be `*`) |
| `namespace` | Yes | The target namespace (cannot be `*`) |
| `name` | No | Optional friendly name for the destination |
| `description` | Yes | Explanation of why this change is being made (for audit purposes) |

### List Destinations

**Request body:**
```json
{
  "project": "my-project"
}
```

### List Destinations Response

```json
{
  "destinations": [
    {
      "server": "https://customer-cluster.example.com",
      "namespace": "production",
      "name": "customer-prod-cluster"
    }
  ]
}
```

### Error Response

```json
{
  "message": "description of what went wrong"
}
```

## Authentication

All endpoints except `/health` require an API key passed via the `X-API-Key` header:

```bash
curl -H "X-API-Key: your-secret-key" http://localhost:8080/projects/my-project/destinations
```

## Project Structure

```
.
├── main.go                 # Application entry point, HTTP server setup
├── go.mod                  # Go module definition
├── Dockerfile              # Multi-stage Docker build
├── .github/
│   └── workflows/
│       └── build-image.yaml # GitHub Actions CI/CD workflow
├── handlers/
│   └── destinations.go     # HTTP request handlers for all endpoints
├── argocd/
│   └── client.go           # Kubernetes client for AppProject CRDs
├── middleware/
│   └── auth.go             # API key authentication and request logging
├── audit/
│   └── logger.go           # Audit log writer (newline-delimited JSON)
└── deploy/
    ├── kustomization.yaml  # Kustomize configuration
    ├── serviceaccount.yaml # ServiceAccount for the API
    ├── role.yaml           # RBAC Role (get, list, patch appprojects)
    ├── rolebinding.yaml    # Binds ServiceAccount to Role
    ├── secret.yaml         # API key secret (change before deploying!)
    ├── pvc.yaml            # PersistentVolumeClaim for audit logs
    ├── deployment.yaml     # Deployment spec
    └── service.yaml        # ClusterIP Service
```

## Code Overview

### `main.go`

Entry point that:
- Reads configuration from environment variables
- Initializes the audit logger with a file path
- Creates the ArgoCD Kubernetes client using in-cluster credentials
- Sets up Chi router with middleware (request logging, recovery, auth)
- Starts the HTTP server

### `argocd/client.go`

Kubernetes client that:
- Uses the dynamic client to work with AppProject CRDs
- Fetches and patches `spec.destinations` on AppProjects
- Handles optimistic concurrency using `resourceVersion` (returns 409 Conflict on concurrent modifications)
- Implements idempotent operations (adding existing destination = no-op, removing non-existent = no-op)

### `handlers/destinations.go`

HTTP handlers that:
- Parse and validate incoming requests
- Enforce validation rules (no wildcards, valid project names)
- Require a description for all modifications
- Write to the audit log on successful changes
- Return appropriate HTTP status codes

### `middleware/auth.go`

Middleware that:
- Validates the `X-API-Key` header against the configured key
- Logs all requests with method, path, status code, and duration

### `audit/logger.go`

Audit logger that:
- Writes entries as newline-delimited JSON to a file
- Records timestamp, action, project, destination details, description, and request metadata
- Uses mutex for thread-safe writes

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `API_KEY` | (required) | API key for authenticating requests |
| `ARGOCD_NAMESPACE` | `argocd` | Namespace where AppProjects are located |
| `PORT` | `8080` | HTTP server port |
| `AUDIT_LOG_PATH` | `/var/log/audit/audit.log` | Path to the audit log file |

## Audit Log

All changes (add/remove) are logged to a persistent file in newline-delimited JSON format:

```json
{"timestamp":"2024-01-15T10:30:00Z","action":"add","project":"my-project","server":"https://cluster.example.com","namespace":"production","name":"prod-cluster","description":"Onboarding new customer (TICKET-123)","user_agent":"curl/7.88.1","remote_addr":"10.0.0.5:54321"}
{"timestamp":"2024-01-15T11:45:00Z","action":"remove","project":"my-project","server":"https://old-cluster.example.com","namespace":"staging","name":"","description":"Decommissioning old staging cluster","user_agent":"curl/7.88.1","remote_addr":"10.0.0.5:54322"}
```

The audit log is stored on a PersistentVolumeClaim to ensure logs survive pod restarts.

## CI/CD

The project includes a GitHub Actions workflow that automatically builds and pushes Docker images to GitHub Container Registry (ghcr.io).

### Workflow Triggers

| Event | Action |
|-------|--------|
| Push to `main` | Build and push image tagged with `main` and commit SHA |
| Push tag `v*` | Build and push image tagged with version (e.g., `v1.0.0`, `1.0`) |
| Pull request | Build only (no push), validates the build works |

### Image Tags

Images are automatically tagged:
- `main` - Latest from main branch
- `v1.2.3` - Semantic version from git tag
- `1.2` - Major.minor version
- `abc1234` - Git commit SHA

### Using the Image

After pushing to GitHub, images are available at:
```
ghcr.io/YOUR_USERNAME/argocd-project-manager:TAG
```

Update `deploy/deployment.yaml` or use kustomize to override:
```yaml
# In deploy/kustomization.yaml
images:
  - name: ghcr.io/OWNER/argocd-project-manager
    newName: ghcr.io/your-username/argocd-project-manager
    newTag: v1.0.0
```

### Repository Settings

The workflow uses `GITHUB_TOKEN` which is automatically provided. Ensure your repository has:
- **Settings > Actions > General > Workflow permissions**: "Read and write permissions"
- **Settings > Packages**: Package visibility set as needed (public/private)

## Deployment

### Prerequisites

- Kubernetes cluster with ArgoCD installed
- `kubectl` configured to access the cluster

### Deploy from GitHub Container Registry

1. **Update the image** in `deploy/kustomization.yaml`:
   ```yaml
   images:
     - name: ghcr.io/OWNER/argocd-project-manager
       newName: ghcr.io/your-username/argocd-project-manager
       newTag: v1.0.0
   ```

2. **Create the API key secret**:
   ```bash
   # Edit deploy/secret.yaml and replace CHANGE_ME_TO_A_SECURE_KEY
   # Or create the secret directly:
   kubectl create secret generic argocd-destination-api \
     --namespace argocd \
     --from-literal=api-key=$(openssl rand -base64 32)
   ```

3. **Deploy:**
   ```bash
   kubectl apply -k deploy/
   ```

### Build Locally (Alternative)

If you prefer to build locally instead of using GitHub Actions:

```bash
# Build the image
docker build -t ghcr.io/your-username/argocd-project-manager:latest .

# Push to GHCR (requires: echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin)
docker push ghcr.io/your-username/argocd-project-manager:latest
```

### Verify Deployment

```bash
# Check pod status
kubectl get pods -n argocd -l app=argocd-destination-api

# Check logs
kubectl logs -n argocd -l app=argocd-destination-api

# Test health endpoint (from within cluster)
kubectl run curl --rm -it --image=curlimages/curl -- \
  curl http://argocd-destination-api.argocd.svc/health
```

## Usage Examples

### List destinations
```bash
curl -X POST \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"project": "my-project"}' \
  http://argocd-destination-api.argocd-project-manager.svc/destinations/list
```

### Add a destination
```bash
curl -X POST \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "project": "my-project",
    "server": "https://customer-cluster.example.com",
    "namespace": "production",
    "name": "customer-prod",
    "description": "Adding production cluster for ACME Corp onboarding (TICKET-456)"
  }' \
  http://argocd-destination-api.argocd-project-manager.svc/destinations
```

### Remove a destination
```bash
curl -X DELETE \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "project": "my-project",
    "server": "https://customer-cluster.example.com",
    "namespace": "production",
    "name": "customer-prod",
    "description": "Removing cluster - customer offboarded (TICKET-789)"
  }' \
  http://argocd-destination-api.argocd-project-manager.svc/destinations
```

## HTTP Status Codes

| Code | Meaning |
|------|---------|
| `200` | Success (GET) |
| `201` | Created (POST - destination added) |
| `204` | No Content (DELETE - destination removed) |
| `400` | Bad Request (validation error, missing fields, wildcards) |
| `401` | Unauthorized (missing or invalid API key) |
| `403` | Forbidden (RBAC denies access to the project) |
| `404` | Not Found (AppProject doesn't exist) |
| `409` | Conflict (concurrent modification, retry the request) |
| `500` | Internal Server Error |

## Validation Rules

- **Project name**: Must contain only alphanumeric characters, dashes (`-`), and underscores (`_`)
- **Server**: Required, cannot be `*` (wildcard)
- **Namespace**: Required, cannot be `*` (wildcard)
- **Description**: Required for POST and DELETE operations

## Idempotency

The API is designed to be idempotent:
- **Adding** a destination that already exists returns `201 Created` without modifying the resource
- **Removing** a destination that doesn't exist returns `204 No Content` without error

## Concurrency Handling

The service uses Kubernetes optimistic concurrency control via `resourceVersion`. If two requests try to modify the same AppProject simultaneously, one will receive a `409 Conflict` response and should retry.

## Security Considerations

- The API runs as a non-root user (UID 1000)
- Read-only root filesystem (audit logs written to mounted PVC)
- All capabilities dropped
- Seccomp profile enabled
- API key should be rotated periodically
- Consider adding network policies to restrict access to the service

## Local Development

For local development outside a cluster, you'll need to modify `argocd/client.go` to use a kubeconfig file instead of in-cluster config:

```go
import "k8s.io/client-go/tools/clientcmd"

// Replace rest.InClusterConfig() with:
config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
```

Then run:
```bash
export API_KEY=dev-key
export ARGOCD_NAMESPACE=argocd
export AUDIT_LOG_PATH=./audit.log
go run .
```
