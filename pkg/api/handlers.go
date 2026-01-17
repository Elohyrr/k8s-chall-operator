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
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
	"github.com/leo/chall-operator/pkg/builder"
)

// sanitizeName converts a string to be DNS-safe for Kubernetes resource names
// Example: "alice@ctf.local" -> "alice-at-ctf-local"
func sanitizeName(s string) string {
	result := strings.ReplaceAll(s, "@", "-at-")
	result = strings.ReplaceAll(result, ".", "-")
	result = strings.ToLower(result)
	// Truncate to 63 chars (K8s name limit)
	if len(result) > 63 {
		result = result[:63]
	}
	return result
}

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
// Supports both snake_case (our format) and camelCase (chall-manager format)
type CreateInstanceRequest struct {
	ChallengeID      string            `json:"challenge_id"`
	SourceID         string            `json:"source_id"`
	ChallengeIDCamel string            `json:"challengeId"`
	SourceIDCamel    string            `json:"sourceId"`
	Additional       map[string]string `json:"additional,omitempty"`
}

// GetChallengeID returns the challenge ID from either format
func (r *CreateInstanceRequest) GetChallengeID() string {
	if r.ChallengeID != "" {
		return r.ChallengeID
	}
	return r.ChallengeIDCamel
}

// GetSourceID returns the source ID from either format
func (r *CreateInstanceRequest) GetSourceID() string {
	if r.SourceID != "" {
		return r.SourceID
	}
	return r.SourceIDCamel
}

// InstanceResponse represents the response for instance operations
type InstanceResponse struct {
	ChallengeID    string   `json:"challenge_id"`
	SourceID       string   `json:"source_id"`
	ConnectionInfo string   `json:"connectionInfo"`
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

	// Get IDs from either format (snake_case or camelCase)
	challengeID := req.GetChallengeID()
	sourceID := req.GetSourceID()

	if challengeID == "" || sourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing required fields", "challenge_id/challengeId and source_id/sourceId are required")
		return
	}

	ctx := context.Background()

	// Generate instance name from challenge and source IDs (sanitized for K8s)
	// Prefix with "chal-" to ensure DNS-1035 compliance (must start with letter)
	sanitizedSourceID := sanitizeName(sourceID)
	instanceName := fmt.Sprintf("chal-%s-%s", challengeID, sanitizedSourceID)

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
		Name:      challengeID,
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
				"ctf.io/challenge": challengeID,
				"ctf.io/source":    sanitizedSourceID,
			},
		},
		Spec: ctfv1alpha1.ChallengeInstanceSpec{
			ChallengeID:   challengeID,
			SourceID:      sourceID,
			ChallengeName: challengeID, // Assume Challenge name = challengeID
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

	instanceName := fmt.Sprintf("chal-%s-%s", challengeID, sanitizeName(sourceID))

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

	instanceName := fmt.Sprintf("chal-%s-%s", challengeID, sanitizeName(sourceID))

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

	// Return success response for CTFd compatibility
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Instance deleted successfully",
	})
}

// ListInstances handles GET /api/v1/instance (query by source_id or sourceId)
func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	// Support both snake_case and camelCase query params
	sourceID := r.URL.Query().Get("source_id")
	if sourceID == "" {
		sourceID = r.URL.Query().Get("sourceId")
	}

	instanceList := &ctfv1alpha1.ChallengeInstanceList{}
	listOpts := []client.ListOption{
		client.InNamespace(h.namespace),
	}

	if sourceID != "" {
		listOpts = append(listOpts, client.MatchingLabels{
			"ctf.io/source": sanitizeName(sourceID),
		})
	}

	if err := h.client.List(context.Background(), instanceList, listOpts...); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to list instances", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Return instances in streaming format (one {"result": {...}} per line)
	// This matches the format expected by the CTFd plugin
	for _, instance := range instanceList.Items {
		response := h.buildInstanceResponse(&instance)
		result := map[string]interface{}{
			"result": response,
		}
		data, _ := json.Marshal(result)
		w.Write(data)
		w.Write([]byte("\n"))
	}
}
}

// ValidateFlagRequest represents the request body for flag validation
type ValidateFlagRequest struct {
	Flag string `json:"flag"`
}

// ValidateFlag handles POST /api/v1/instance/{challengeId}/{sourceId}/validate
// When the flag is correct, marks the instance for deletion by the janitor
func (h *Handler) ValidateFlag(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")
	sourceID := chi.URLParam(r, "sourceId")

	if challengeID == "" || sourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameters", "challengeId and sourceId are required")
		return
	}

	var req ValidateFlagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	if req.Flag == "" {
		h.writeError(w, http.StatusBadRequest, "Missing flag", "flag is required")
		return
	}

	instanceName := fmt.Sprintf("chal-%s-%s", challengeID, sanitizeName(sourceID))
	ctx := context.Background()

	instance := &ctfv1alpha1.ChallengeInstance{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      instanceName,
		Namespace: h.namespace,
	}, instance); err != nil {
		h.writeError(w, http.StatusNotFound, "Instance not found", err.Error())
		return
	}

	// Check if the flag is correct
	flagValid := false
	for _, correctFlag := range instance.Status.Flags {
		if req.Flag == correctFlag {
			flagValid = true
			break
		}
	}

	if !flagValid {
		h.writeError(w, http.StatusForbidden, "Invalid flag", "The submitted flag is incorrect")
		return
	}

	// Mark the instance for deletion by setting FlagValidated = true
	instance.Status.FlagValidated = true
	if err := h.client.Status().Update(ctx, instance); err != nil {
		log.Printf("Failed to mark instance %s as validated: %v", instanceName, err)
		h.writeError(w, http.StatusInternalServerError, "Failed to validate flag", err.Error())
		return
	}

	log.Printf("Flag validated for instance %s, marked for deletion", instanceName)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":   true,
		"message": "Flag correct! Instance will be cleaned up.",
	}); err != nil {
		log.Printf("handlers: encode responses: %v", err)
    	http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// RenewInstance handles POST /api/v1/instance/{challengeId}/{sourceId}/renew
// Extends the instance expiration time
func (h *Handler) RenewInstance(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")
	sourceID := chi.URLParam(r, "sourceId")

	if challengeID == "" || sourceID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameters", "challengeId and sourceId are required")
		return
	}

	instanceName := fmt.Sprintf("chal-%s-%s", challengeID, sanitizeName(sourceID))
	ctx := context.Background()

	instance := &ctfv1alpha1.ChallengeInstance{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      instanceName,
		Namespace: h.namespace,
	}, instance); err != nil {
		h.writeError(w, http.StatusNotFound, "Instance not found", err.Error())
		return
	}

	// Get timeout from challenge (default 600 seconds)
	timeout := int64(600)
	challenge := &ctfv1alpha1.Challenge{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.ChallengeName,
		Namespace: h.namespace,
	}, challenge); err == nil {
		if challenge.Spec.Timeout > 0 {
			timeout = challenge.Spec.Timeout
		}
	}

	// Extend expiration
	newUntil := metav1.NewTime(time.Now().Add(time.Duration(timeout) * time.Second))
	instance.Spec.Until = &newUntil

	if err := h.client.Update(ctx, instance); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to renew instance", err.Error())
		return
	}

	log.Printf("Instance %s renewed until %s", instanceName, newUntil.Format(time.RFC3339))
	h.writeInstanceResponse(w, instance)
}

// Health handles GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handlers: encode responses: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
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

	// Calculate connectionInfo if not already set by controller
	if resp.ConnectionInfo == "" {
		// Get Challenge to check for Ingress config
		challenge := &ctfv1alpha1.Challenge{}
		if err := h.client.Get(context.Background(), types.NamespacedName{
			Name:      instance.Spec.ChallengeID,
			Namespace: h.namespace,
		}, challenge); err == nil {
			// Generate hostname using builder
			hostname := builder.GetIngressHostname(instance, challenge)
			if hostname != "" {
				if challenge.Spec.Scenario.AttackBox != nil && challenge.Spec.Scenario.AttackBox.Enabled {
					resp.ConnectionInfo = fmt.Sprintf("Challenge: http://%s\nTerminal: http://%s/terminal", hostname, hostname)
				} else {
					resp.ConnectionInfo = fmt.Sprintf("http://%s", hostname)
				}
			}
		}
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

// FlexibleInt64 can unmarshal from both string and int
type FlexibleInt64 int64

func (f *FlexibleInt64) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as int first
	var i int64
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexibleInt64(i)
		return nil
	}
	// Try as string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	// Strip common duration suffixes (s, m, h, etc.)
	s = strings.TrimSuffix(s, "s")
	s = strings.TrimSuffix(s, "m")
	s = strings.TrimSuffix(s, "h")
	// Parse string to int
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = FlexibleInt64(i)
	return nil
}

// CreateChallengeRequest represents the request body for creating a challenge
// Supports both formats from CTFd plugin
type CreateChallengeRequest struct {
	ID       string        `json:"id"`
	Scenario string        `json:"scenario"` // Image reference (e.g. registry.local:5000/chal1:latest)
	Timeout  FlexibleInt64 `json:"timeout"`
	// Additional fields from CTFd
	DestroyOnFlag bool `json:"destroy_on_flag"`
	Shared        bool `json:"shared"`
}

// ChallengeResponse represents the response for challenge operations
type ChallengeResponse struct {
	ID       string `json:"id"`
	Scenario string `json:"scenario"`
	Timeout  int64  `json:"timeout"`
}

// CreateChallenge handles POST /api/v1/challenge
// In GitOps mode: just verifies the Challenge CRD exists (doesn't create it)
// The Challenge should be created manually via kubectl/ArgoCD
// Uses the "scenario" field as the Challenge ID (ignores CTFd auto-incremented ID)
func (h *Handler) CreateChallenge(w http.ResponseWriter, r *http.Request) {
	var req CreateChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	// Use scenario as the Challenge ID (GitOps: scenario = Challenge CRD name)
	challengeID := req.Scenario
	if challengeID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing required field", "scenario is required")
		return
	}

	ctx := context.Background()

	// GitOps mode: Challenge must already exist
	existingChallenge := &ctfv1alpha1.Challenge{}
	err := h.client.Get(ctx, types.NamespacedName{
		Name:      challengeID,
		Namespace: h.namespace,
	}, existingChallenge)

	if err != nil {
		// Challenge doesn't exist - in GitOps mode, this is an error
		log.Printf("Challenge %s not found (GitOps mode: create it manually with kubectl). CTFd ID: %s", challengeID, req.ID)
		h.writeError(w, http.StatusNotFound, "Challenge not found", fmt.Sprintf("Challenge %s must be created manually via kubectl/ArgoCD before creating it in CTFd", challengeID))
		return
	}

	// Challenge exists, return it
	log.Printf("Challenge %s found (GitOps mode). CTFd ID: %s", challengeID, req.ID)
	w.WriteHeader(http.StatusOK)
	h.writeChallengeResponse(w, existingChallenge)
}

// GetChallenge handles GET /api/v1/challenge/{challengeId}
func (h *Handler) GetChallenge(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")

	if challengeID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameter", "challengeId is required")
		return
	}

	challenge := &ctfv1alpha1.Challenge{}
	if err := h.client.Get(context.Background(), types.NamespacedName{
		Name:      challengeID,
		Namespace: h.namespace,
	}, challenge); err != nil {
		h.writeError(w, http.StatusNotFound, "Challenge not found", err.Error())
		return
	}

	h.writeChallengeResponse(w, challenge)
}

// UpdateChallenge handles PATCH /api/v1/challenge/{challengeId}
func (h *Handler) UpdateChallenge(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")

	if challengeID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameter", "challengeId is required")
		return
	}

	var req CreateChallengeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	ctx := context.Background()

	challenge := &ctfv1alpha1.Challenge{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      challengeID,
		Namespace: h.namespace,
	}, challenge); err != nil {
		h.writeError(w, http.StatusNotFound, "Challenge not found", err.Error())
		return
	}

	// Update fields if provided
	if req.Scenario != "" {
		challenge.Spec.Scenario.Image = req.Scenario
	}
	if req.Timeout > 0 {
		challenge.Spec.Timeout = int64(req.Timeout)
	}

	if err := h.client.Update(ctx, challenge); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to update challenge", err.Error())
		return
	}

	log.Printf("Updated challenge %s", challengeID)
	h.writeChallengeResponse(w, challenge)
}

// DeleteChallenge handles DELETE /api/v1/challenge/{challengeId}
func (h *Handler) DeleteChallenge(w http.ResponseWriter, r *http.Request) {
	challengeID := chi.URLParam(r, "challengeId")

	if challengeID == "" {
		h.writeError(w, http.StatusBadRequest, "Missing path parameter", "challengeId is required")
		return
	}

	ctx := context.Background()

	challenge := &ctfv1alpha1.Challenge{}
	if err := h.client.Get(ctx, types.NamespacedName{
		Name:      challengeID,
		Namespace: h.namespace,
	}, challenge); err != nil {
		h.writeError(w, http.StatusNotFound, "Challenge not found", err.Error())
		return
	}

	// Also delete all instances of this challenge
	instanceList := &ctfv1alpha1.ChallengeInstanceList{}
	if err := h.client.List(ctx, instanceList, client.InNamespace(h.namespace), client.MatchingLabels{
		"ctf.io/challenge": challengeID,
	}); err == nil {
		for _, instance := range instanceList.Items {
			if err := h.client.Delete(ctx, &instance); err != nil {
				log.Printf("Failed to delete instance %s: %v", instance.Name, err)
			}
		}
	}

	if err := h.client.Delete(ctx, challenge); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete challenge", err.Error())
		return
	}

	log.Printf("Deleted challenge %s and its instances", challengeID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// ListChallenges handles GET /api/v1/challenge
func (h *Handler) ListChallenges(w http.ResponseWriter, r *http.Request) {
	challengeList := &ctfv1alpha1.ChallengeList{}
	if err := h.client.List(context.Background(), challengeList, client.InNamespace(h.namespace)); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to list challenges", err.Error())
		return
	}

	// Stream response like chall-manager does
	w.Header().Set("Content-Type", "application/json")
	for _, challenge := range challengeList.Items {
		resp := map[string]interface{}{
			"result": ChallengeResponse{
				ID:       challenge.Spec.ID,
				Scenario: challenge.Spec.Scenario.Image,
				Timeout:  challenge.Spec.Timeout,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// writeChallengeResponse writes a challenge response
func (h *Handler) writeChallengeResponse(w http.ResponseWriter, challenge *ctfv1alpha1.Challenge) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChallengeResponse{
		ID:       challenge.Spec.ID,
		Scenario: challenge.Spec.Scenario.Image,
		Timeout:  challenge.Spec.Timeout,
	})
}
