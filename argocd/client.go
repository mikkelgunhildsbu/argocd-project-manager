package argocd

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// Destination represents an ArgoCD AppProject destination
type Destination struct {
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
	Name      string `json:"name,omitempty"`
}

// Client provides methods to interact with ArgoCD AppProjects
type Client struct {
	dynamicClient dynamic.Interface
	namespace     string
	gvr           schema.GroupVersionResource
}

// NewClient creates a new ArgoCD client using in-cluster configuration
func NewClient(namespace string) (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Client{
		dynamicClient: dynamicClient,
		namespace:     namespace,
		gvr: schema.GroupVersionResource{
			Group:    "argoproj.io",
			Version:  "v1alpha1",
			Resource: "appprojects",
		},
	}, nil
}

// Project represents an ArgoCD AppProject summary
type Project struct {
	Name             string        `json:"name"`
	DestinationCount int           `json:"destinationCount"`
	Destinations     []Destination `json:"destinations"`
}

// ListProjects retrieves all AppProjects
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	list, err := c.dynamicClient.Resource(c.gvr).Namespace(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var projects []Project
	for _, item := range list.Items {
		name := item.GetName()
		destinations, _ := c.extractDestinations(&item)
		if destinations == nil {
			destinations = []Destination{}
		}
		projects = append(projects, Project{
			Name:             name,
			DestinationCount: len(destinations),
			Destinations:     destinations,
		})
	}

	return projects, nil
}

// GetDestinations retrieves all destinations for an AppProject
func (c *Client) GetDestinations(ctx context.Context, projectName string) ([]Destination, string, error) {
	project, err := c.dynamicClient.Resource(c.gvr).Namespace(c.namespace).Get(ctx, projectName, metav1.GetOptions{})
	if err != nil {
		return nil, "", err
	}

	resourceVersion := project.GetResourceVersion()
	destinations, err := c.extractDestinations(project)
	if err != nil {
		return nil, "", err
	}

	return destinations, resourceVersion, nil
}

// AddDestination adds a destination to an AppProject (idempotent)
func (c *Client) AddDestination(ctx context.Context, projectName string, dest Destination) error {
	// Get current state
	destinations, resourceVersion, err := c.GetDestinations(ctx, projectName)
	if err != nil {
		return err
	}

	// Check if destination already exists (idempotent)
	for _, existing := range destinations {
		if c.destinationsEqual(existing, dest) {
			return nil // Already exists, nothing to do
		}
	}

	// Add the new destination
	destinations = append(destinations, dest)

	// Patch the AppProject
	return c.patchDestinations(ctx, projectName, destinations, resourceVersion)
}

// RemoveDestination removes a destination from an AppProject (idempotent)
func (c *Client) RemoveDestination(ctx context.Context, projectName string, dest Destination) error {
	// Get current state
	destinations, resourceVersion, err := c.GetDestinations(ctx, projectName)
	if err != nil {
		return err
	}

	// Find and remove the destination
	var newDestinations []Destination
	found := false
	for _, existing := range destinations {
		if c.destinationsEqual(existing, dest) {
			found = true
			continue // Skip this one (remove it)
		}
		newDestinations = append(newDestinations, existing)
	}

	// If not found, nothing to do (idempotent)
	if !found {
		return nil
	}

	// Patch the AppProject
	return c.patchDestinations(ctx, projectName, newDestinations, resourceVersion)
}

// patchDestinations patches the destinations array on an AppProject
func (c *Client) patchDestinations(ctx context.Context, projectName string, destinations []Destination, resourceVersion string) error {
	// Build the patch
	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"resourceVersion": resourceVersion,
		},
		"spec": map[string]interface{}{
			"destinations": destinations,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = c.dynamicClient.Resource(c.gvr).Namespace(c.namespace).Patch(
		ctx,
		projectName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)

	return err
}

// extractDestinations extracts destinations from an unstructured AppProject
func (c *Client) extractDestinations(project *unstructured.Unstructured) ([]Destination, error) {
	spec, found, err := unstructured.NestedMap(project.Object, "spec")
	if err != nil {
		return nil, fmt.Errorf("failed to get spec: %w", err)
	}
	if !found {
		return []Destination{}, nil
	}

	destinationsRaw, found, err := unstructured.NestedSlice(spec, "destinations")
	if err != nil {
		return nil, fmt.Errorf("failed to get destinations: %w", err)
	}
	if !found {
		return []Destination{}, nil
	}

	var destinations []Destination
	for _, d := range destinationsRaw {
		destMap, ok := d.(map[string]interface{})
		if !ok {
			continue
		}

		dest := Destination{}
		if server, ok := destMap["server"].(string); ok {
			dest.Server = server
		}
		if namespace, ok := destMap["namespace"].(string); ok {
			dest.Namespace = namespace
		}
		if name, ok := destMap["name"].(string); ok {
			dest.Name = name
		}
		destinations = append(destinations, dest)
	}

	return destinations, nil
}

// destinationsEqual checks if two destinations are equal
func (c *Client) destinationsEqual(a, b Destination) bool {
	return a.Server == b.Server && a.Namespace == b.Namespace && a.Name == b.Name
}
