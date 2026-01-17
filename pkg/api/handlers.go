/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
)

// Handler handles HTTP requests for the CTFd-compatible API
type Handler struct {
	client    client.Client
	namespace string
}

// NewHandler creates a new API handler
func NewHandler(c client.Client) *Handler {
	namespace := os.Getenv("INSTANCE_NAMESPACE")
	if namespace == "" {
		namespace = "ctf-instances"
	}
	return &Handler{
		client:    c,
		namespace: namespace,
	}
}

// CreateInstanceRequest represents the request body for creating an instance
type CreateInstanceRequest struct {
	ChallengeID string            `json:"challenge_id"`
	SourceID    string            `json:"source_id"`
	Additional  map[string]string `json:"additional,omitempty"`
}

// InstanceResponse represents the response for instance operations
type InstanceResponse struct {
	ChallengeID    string   `json:"challenge_id"`
	SourceID       string   `json:"source_id"`
	ConnectionInfo string   `json:"connection_info"`
	Flags          []string `json:"flags,omitempty"`
	Flag           string   `json:"flag,omitempty"` // Deprecated but kept for compatibility
	Since          string   `json:"since"`
	Until          string   `json:"until,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// CreateInstance handles POST /api/v1/instance
func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	if req.ChallengeID == "" || req.SourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing required fields", "challenge_id and source_id are required")
		return
	}

	ctx := context.Background()

	// Generate instance name from challenge and source IDs
	instanceName := fmt.Sprintf("%s-%s", req.ChallengeID, req.SourceID)

	// Check if instance already exists
	existingInstance := &ctfv1alpha1.ChallengeInstance{}
	err := h.client.Get(ctx, types.NamespacedName{
		Name:      instanceName,
		Namespace: h.namespace,
	}, existingInstance)

	if err == nil {
		// Instance already exists, return it
		log.Printf("Instance %s already exists, returning existing", instanceName)
		h.writeInstanceResponse(w, existingInstance)
		return
	}

	// Get timeout from challenge (default 600 seconds)
	timeout := int64(600)
	challenge := &ctfv1alpha1.Challenge{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      req.ChallengeID,
		Namespace: h.namespace,
	}, challenge); err == nil {
		if challenge.Spec.Timeout > 0 {
			timeout = challenge.Spec.Timeout
		}
	}

	// Create ChallengeInstance CRD
	now := metav1.Now()
	until := metav1.NewTime(time.Now().Add(time.Duration(timeout) * time.Second))

	instance := &ctfv1alpha1.ChallengeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: h.namespace,
			Labels: map[string]string{
				"ctf.io/challenge": req.ChallengeID,
				"ctf.io/source":    req.SourceID,
			},
		},
		Spec: ctfv1alpha1.ChallengeInstanceSpec{
			ChallengeID:   req.ChallengeID,
			SourceID:      req.SourceID,
			ChallengeName: req.ChallengeID, // Assume Challenge name = challengeID
			Additional:    req.Additional,
			Since:         now,
			Until:         &until,
		},
	}

	if err := h.client.Create(ctx, instance); err != nil {
		log.Printf("Failed to create instance %s: %v", instanceName, err)
		h.writeError(w, http.StatusInternalServerError, "Failed to create instance", err.Error())
		return
	}

	log.Printf("Created instance %s, waiting for ready state", instanceName)

	// Wait for instance to be ready (poll status)
	var readyInstance *ctfv1alpha1.ChallengeInstance
	for i := 0; i < 60; i++ { // 60 seconds timeout
		time.Sleep(1 * time.Second)

		instance := &ctfv1alpha1.ChallengeInstance{}
		if err := h.client.Get(ctx, types.NamespacedName{
			Name:      instanceName,
			Namespace: h.namespace,
		}, instance); err != nil {
			continue
		}

		if instance.Status.Ready {
			readyInstance = instance
			log.Printf("Instance %s is ready", instanceName)
			break
		}

		// Check for failure
		if instance.Status.Phase == "Failed" {
			h.writeError(w, http.StatusInternalServerError, "Instance failed to start", "Challenge deployment failed")
			return
		}
	}

	if readyInstance == nil {
		// Timeout waiting for ready, but return what we have
		instance := &ctfv1alpha1.ChallengeInstance{}
		if err := h.client.Get(ctx, types.NamespacedName{
			Name:      instanceName,
			Namespace: h.namespace,
		}, instance); err != nil {
			h.writeError(w, http.StatusInternalServerError, "Failed to get instance status", err.Error())
			return
		}
		readyInstance = instance
		log.Printf("Instance %s not ready after timeout, returning current state", instanceName)
	}

	w.WriteHeader(http.StatusCreated)
	h.writeInstanceResponse(w, readyInstance)
}

// GetInstance handles GET /api/v1/instance/{challengeId}/{sourceId}
func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")
	sourceID := chi.URLParam(r, "sourceId")

	if challengeID == "" || sourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameters", "challengeId and sourceId are required")
		return
	}

	instanceName := fmt.Sprintf("%s-%s", challengeID, sourceID)

	instance := &ctfv1alpha1.ChallengeInstance{}
	if err := h.client.Get(context.Background(), types.NamespacedName{
		Name:      instanceName,
		Namespace: h.namespace,
	}, instance); err != nil {
		h.writeError(w, http.StatusNotFound, "Instance not found", err.Error())
		return
	}

	h.writeInstanceResponse(w, instance)
}

// DeleteInstance handles DELETE /api/v1/instance/{challengeId}/{sourceId}
func (h *Handler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")
	sourceID := chi.URLParam(r, "sourceId")

	if challengeID == "" || sourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameters", "challengeId and sourceId are required")
		return
	}

	instanceName := fmt.Sprintf("%s-%s", challengeID, sourceID)

	instance := &ctfv1alpha1.ChallengeInstance{}
	ctx := context.Background()

	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      instanceName,
		Namespace: h.namespace,
	}, instance); err != nil {
		h.writeError(w, http.StatusNotFound, "Instance not found", err.Error())
		return
	}

	if err := h.client.Delete(ctx, instance); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete instance", err.Error())
		return
	}

	log.Printf("Deleted instance %s", instanceName)
	w.WriteHeader(http.StatusNoContent)
}

// ListInstances handles GET /api/v1/instance (query by source_id)
func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	sourceID := r.URL.Query().Get("source_id")

	instanceList := &ctfv1alpha1.ChallengeInstanceList{}
	listOpts := []client.ListOption{
		client.InNamespace(h.namespace),
	}

	if sourceID != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			"ctf.io/source": sourceID,
		})
	}

	if err := h.client.List(context.Background(), instanceList, listOpts...); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to list instances", err.Error())
		return
	}

	responses := make([]InstanceResponse, 0, len(instanceList.Items))
	for _, instance := range instanceList.Items {
		responses = append(responses, h.buildInstanceResponse(&instance))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

// Health handles GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// writeError writes an error response
func (h *Handler) writeError(w http.ResponseWriter, status int, error, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   error,
		Message: message,
	})
}

// writeInstanceResponse writes an instance response
func (h *Handler) writeInstanceResponse(w http.ResponseWriter, instance *ctfv1alpha1.ChallengeInstance) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.buildInstanceResponse(instance))
}

// buildInstanceResponse creates an InstanceResponse from a ChallengeInstance
func (h *Handler) buildInstanceResponse(instance *ctfv1alpha1.ChallengeInstance) InstanceResponse {
	resp := InstanceResponse{
		ChallengeID:    instance.Spec.ChallengeID,
		SourceID:       instance.Spec.SourceID,
		ConnectionInfo: instance.Status.ConnectionInfo,
		Flags:          instance.Status.Flags,
		Since:          instance.Spec.Since.Format(time.RFC3339),
	}

	// Set deprecated Flag field for backwards compatibility
	if len(instance.Status.Flags) > 0 {
		resp.Flag = instance.Status.Flags[0]
	}

	if instance.Spec.Until != nil {
		resp.Until = instance.Spec.Until.Format(time.RFC3339)
	}

	return resp
}
