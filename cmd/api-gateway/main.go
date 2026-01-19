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

package main

import (
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctfv1alpha1 "github.com/leo/chall-operator/api/v1alpha1"
	_ "github.com/leo/chall-operator/docs" // Import generated docs
	"github.com/leo/chall-operator/pkg/api"
)

// @title CTF Challenge Operator API Gateway
// @version 1.0
// @description API Gateway for managing CTF challenges and instances via Kubernetes CRDs
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url https://github.com/Elohyrr/k8s-chall-operator
// @contact.email your-email@example.com

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1
// @schemes http https

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(ctfv1alpha1.AddToScheme(scheme))
}

func main() {
	// Setup K8s client
	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	// Create handler
	handler := api.NewHandler(k8sClient)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(corsMiddleware)

	// Health check (multiple routes for compatibility)
	r.Get("/health", handler.Health)
	r.Get("/healthz", handler.Health)
	r.Get("/healthcheck", handler.Health)

	// Swagger documentation
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// CTFd-compatible API endpoints
	r.Route("/api/v1", func(r chi.Router) {
		// Challenge management (CRD CRUD)
		r.Post("/challenge", handler.CreateChallenge)
		r.Get("/challenge", handler.ListChallenges)
		r.Get("/challenge/{challengeId}", handler.GetChallenge)
		r.Patch("/challenge/{challengeId}", handler.UpdateChallenge)
		r.Delete("/challenge/{challengeId}", handler.DeleteChallenge)

		// Instance management
		r.Post("/instance", handler.CreateInstance)
		r.Get("/instance", handler.ListInstances)
		r.Get("/instance/{challengeId}/{sourceId}", handler.GetInstance)
		r.Delete("/instance/{challengeId}/{sourceId}", handler.DeleteInstance)
		r.Patch("/instance/{challengeId}/{sourceId}", handler.RenewInstance) // CTFd plugin uses PATCH for renew
		r.Post("/instance/{challengeId}/{sourceId}/validate", handler.ValidateFlag)
		r.Post("/instance/{challengeId}/{sourceId}/renew", handler.RenewInstance)
	})

	// Get port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("API Gateway starting on :%s", port)
	log.Printf("Instance namespace: %s", os.Getenv("INSTANCE_NAMESPACE"))

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// corsMiddleware adds CORS headers for CTFd compatibility
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
