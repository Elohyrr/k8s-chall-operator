# Chall-Operator Development Plan - MVP Lundi

## 🎯 Objectif

**Deadline:** Lundi soir  
**Scope:** MVP fonctionnel pour remplacer chall-manager  
**Contrainte:** Stabilité immédiate, pas de pooling/janitor dans MVP

---

## 📋 MVP - Ce qui DOIT marcher lundi

### Core Features (Non-négociables)
1. ✅ CRD `Challenge` - Définition d'un challenge
2. ✅ CRD `ChallengeInstance` - Instance par user/team
3. ✅ Controller qui crée Deployment + Service
4. ✅ Flag generation unique par instance
5. ✅ API Gateway compatible CTFd (POST/GET/DELETE instance)
6. ✅ Connection info retourné à CTFd

### Out of Scope (Phase 2 après lundi)
- ❌ Instance pooling
- ❌ Janitor (cleanup automatique)
- ❌ AttackBox CRD
- ❌ Cilium Network Policies (CNP)
- ❌ Renewal mechanism
- ❌ Metrics/Observability avancée

---

## 🏗️ Architecture Simplifiée MVP

```
CTFd 
  ↓ HTTP POST /api/v1/instance
API Gateway (port 8080)
  ↓ Creates ChallengeInstance CRD
Instance Controller (watches CRDs)
  ↓ Reconcile
Deployment + Service (NodePort ou LoadBalancer)
  ↓ Returns connection_info
CTFd (displays to user)
```

---

## 📁 Structure du Projet

```
chall-operator/
├── api/
│   └── v1alpha1/
│       ├── challenge_types.go          # Challenge CRD definition
│       ├── challengeinstance_types.go  # ChallengeInstance CRD definition
│       └── groupversion_info.go
├── controllers/
│   └── challengeinstance_controller.go # Main controller logic
├── cmd/
│   ├── operator/
│   │   └── main.go                     # Operator entrypoint
│   └── api-gateway/
│       └── main.go                     # API Gateway entrypoint
├── pkg/
│   ├── api/
│   │   └── handlers.go                 # HTTP handlers for CTFd API
│   ├── flaggen/
│   │   └── generator.go                # Flag generation logic
│   └── builder/
│       ├── deployment.go               # Build Deployment from ChallengeInstance
│       └── service.go                  # Build Service from ChallengeInstance
├── config/
│   ├── crd/                            # Generated CRD YAML
│   ├── rbac/                           # RBAC manifests
│   └── manager/                        # Operator deployment
├── hack/
│   └── tools.go                        # Build tools
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

---

## 🔧 Stack Technique

- **Language:** Go 1.21+
- **Framework:** Kubebuilder v3.12+
- **API:** Chi router (léger et rapide)
- **K8s Client:** controller-runtime + client-go
- **Flag Gen:** crypto/rand + templates

---

## 📝 Phase de Développement (3 jours)

### Vendredi Soir (4h)
**Phase 1: Bootstrap & CRDs**

**Tâches:**
1. ✅ Init projet avec Kubebuilder
```bash
cd /path/to/chall-operator
kubebuilder init --domain ctf.io --repo github.com/youruser/chall-operator
kubebuilder create api --group ctf --version v1alpha1 --kind Challenge
kubebuilder create api --group ctf --version v1alpha1 --kind ChallengeInstance
```

2. ✅ Définir CRD `Challenge` (simplifié)
```go
type ChallengeSpec struct {
    ID       string                 `json:"id"`
    Scenario ChallengeScenarioSpec  `json:"scenario"`
    Timeout  int64                  `json:"timeout,omitempty"`  // seconds
}

type ChallengeScenarioSpec struct {
    Image            string                       `json:"image"`
    Port             int32                        `json:"port"`
    ExposeType       string                       `json:"exposeType"`  // NodePort, LoadBalancer
    Env              []corev1.EnvVar              `json:"env,omitempty"`
    FlagTemplate     string                       `json:"flagTemplate,omitempty"`
    Resources        corev1.ResourceRequirements  `json:"resources,omitempty"`
}

type ChallengeStatus struct {
    ActiveInstances int32 `json:"activeInstances"`
}
```

3. ✅ Définir CRD `ChallengeInstance` (simplifié)
```go
type ChallengeInstanceSpec struct {
    ChallengeID  string            `json:"challengeId"`
    SourceID     string            `json:"sourceId"`
    ChallengeName string           `json:"challengeName"`  // Reference to Challenge
    Additional   map[string]string `json:"additional,omitempty"`
    Since        metav1.Time       `json:"since"`
    Until        *metav1.Time      `json:"until,omitempty"`
}

type ChallengeInstanceStatus struct {
    Phase          string   `json:"phase"`  // Pending, Running, Failed
    ConnectionInfo string   `json:"connectionInfo,omitempty"`
    Flags          []string `json:"flags,omitempty"`
    DeploymentName string   `json:"deploymentName,omitempty"`
    ServiceName    string   `json:"serviceName,omitempty"`
    Ready          bool     `json:"ready"`
}
```

4. ✅ Générer manifests
```bash
make manifests
make generate
```

**Livrable:** CRDs définies, manifests générés

---

### Samedi Matin (4h)
**Phase 2: Controller Core**

**Tâches:**
1. ✅ Implémenter `ChallengeInstanceController.Reconcile()`

**Logique:**
```go
func (r *ChallengeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // 1. Fetch ChallengeInstance
    instance := &ctfv1alpha1.ChallengeInstance{}
    if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Fetch Challenge template
    challenge := &ctfv1alpha1.Challenge{}
    challengeKey := types.NamespacedName{
        Name:      instance.Spec.ChallengeName,
        Namespace: instance.Namespace,
    }
    if err := r.Get(ctx, challengeKey, challenge); err != nil {
        return ctrl.Result{}, err
    }
    
    // 3. Generate flag if not exists
    if len(instance.Status.Flags) == 0 {
        flag := generateFlag(challenge.Spec.Scenario.FlagTemplate, instance)
        instance.Status.Flags = []string{flag}
        r.Status().Update(ctx, instance)
    }
    
    // 4. Create or Update Deployment
    deployment := buildDeployment(instance, challenge)
    if err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
        // Set owner reference
        controllerutil.SetControllerReference(instance, deployment, r.Scheme)
        return nil
    }); err != nil {
        return ctrl.Result{}, err
    }
    
    // 5. Create or Update Service
    service := buildService(instance, challenge)
    if err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
        controllerutil.SetControllerReference(instance, service, r.Scheme)
        return nil
    }); err != nil {
        return ctrl.Result{}, err
    }
    
    // 6. Check if Deployment is ready
    if deployment.Status.ReadyReplicas > 0 {
        instance.Status.Phase = "Running"
        instance.Status.Ready = true
        
        // 7. Get connection info from Service
        connInfo := getConnectionInfo(service, challenge)
        instance.Status.ConnectionInfo = connInfo
        
        r.Status().Update(ctx, instance)
    }
    
    // 8. Check expiry (simple)
    if instance.Spec.Until != nil && time.Now().After(instance.Spec.Until.Time) {
        log.Info("Instance expired, deleting")
        return ctrl.Result{}, r.Delete(ctx, instance)
    }
    
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

2. ✅ Créer builders (`pkg/builder/`)

**deployment.go:**
```go
func BuildDeployment(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *appsv1.Deployment {
    labels := map[string]string{
        "app":         "challenge",
        "challenge":   instance.Spec.ChallengeID,
        "instance":    instance.Name,
    }
    
    // Inject flag into env
    env := challenge.Spec.Scenario.Env
    if len(instance.Status.Flags) > 0 {
        env = append(env, corev1.EnvVar{
            Name:  "FLAG",
            Value: instance.Status.Flags[0],
        })
    }
    
    return &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      instance.Name + "-deployment",
            Namespace: instance.Namespace,
            Labels:    labels,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: ptr.To(int32(1)),
            Selector: &metav1.LabelSelector{
                MatchLabels: labels,
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: labels,
                },
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{
                        {
                            Name:      "challenge",
                            Image:     challenge.Spec.Scenario.Image,
                            Ports: []corev1.ContainerPort{
                                {
                                    ContainerPort: challenge.Spec.Scenario.Port,
                                },
                            },
                            Env:       env,
                            Resources: challenge.Spec.Scenario.Resources,
                        },
                    },
                },
            },
        },
    }
}
```

**service.go:**
```go
func BuildService(instance *ctfv1alpha1.ChallengeInstance, challenge *ctfv1alpha1.Challenge) *corev1.Service {
    labels := map[string]string{
        "app":       "challenge",
        "challenge": instance.Spec.ChallengeID,
        "instance":  instance.Name,
    }
    
    serviceType := corev1.ServiceTypeNodePort
    if challenge.Spec.Scenario.ExposeType == "LoadBalancer" {
        serviceType = corev1.ServiceTypeLoadBalancer
    }
    
    return &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      instance.Name + "-svc",
            Namespace: instance.Namespace,
            Labels:    labels,
        },
        Spec: corev1.ServiceSpec{
            Type:     serviceType,
            Selector: labels,
            Ports: []corev1.ServicePort{
                {
                    Port:       challenge.Spec.Scenario.Port,
                    TargetPort: intstr.FromInt(int(challenge.Spec.Scenario.Port)),
                },
            },
        },
    }
}
```

**Livrable:** Controller qui crée Deployment + Service

---

### Samedi Après-midi (4h)
**Phase 3: Flag Generation**

**Tâches:**
1. ✅ Implémenter `pkg/flaggen/generator.go`

```go
package flaggen

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "strings"
    "text/template"
    "bytes"
)

type FlagContext struct {
    InstanceID   string
    SourceID     string
    ChallengeID  string
    RandomString string
}

func Generate(tmpl string, instanceID, sourceID, challengeID string) (string, error) {
    if tmpl == "" {
        // Default template
        tmpl = "FLAG{{{.ChallengeID}}_{{.SourceID}}_{{.RandomString}}}"
    }
    
    // Generate random string
    randomBytes := make([]byte, 16)
    if _, err := rand.Read(randomBytes); err != nil {
        return "", err
    }
    randomStr := hex.EncodeToString(randomBytes)
    
    // Parse template
    t, err := template.New("flag").Parse(tmpl)
    if err != nil {
        return "", err
    }
    
    // Execute template
    ctx := FlagContext{
        InstanceID:   instanceID,
        SourceID:     sourceID,
        ChallengeID:  challengeID,
        RandomString: randomStr,
    }
    
    var buf bytes.Buffer
    if err := t.Execute(&buf, ctx); err != nil {
        return "", err
    }
    
    flag := buf.String()
    
    // Replace template syntax
    flag = strings.ReplaceAll(flag, "{{{", "{{")
    flag = strings.ReplaceAll(flag, "}}}", "}}")
    
    return flag, nil
}
```

2. ✅ Intégrer dans controller

**Livrable:** Flag generation fonctionnelle

---

### Samedi Soir (4h)
**Phase 4: API Gateway**

**Tâches:**
1. ✅ Créer `cmd/api-gateway/main.go`

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    
    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5"
    "k8s.io/client-go/kubernetes/scheme"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    
    ctfv1alpha1 "github.com/youruser/chall-operator/api/v1alpha1"
    "github.com/youruser/chall-operator/pkg/api"
)

func main() {
    // Setup K8s client
    cfg := ctrl.GetConfigOrDie()
    k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
    if err != nil {
        log.Fatal(err)
    }
    
    // Register CRDs
    ctfv1alpha1.AddToScheme(scheme.Scheme)
    
    // Setup router
    r := chi.NewRouter()
    
    handler := api.NewHandler(k8sClient)
    
    // CTFd-compatible endpoints
    r.Post("/api/v1/instance", handler.CreateInstance)
    r.Get("/api/v1/instance/{challengeId}/{sourceId}", handler.GetInstance)
    r.Delete("/api/v1/instance/{challengeId}/{sourceId}", handler.DeleteInstance)
    
    // Health check
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("OK"))
    })
    
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    
    log.Printf("API Gateway listening on :%s", port)
    http.ListenAndServe(":"+port, r)
}
```

2. ✅ Créer `pkg/api/handlers.go`

```go
package api

import (
    "context"
    "encoding/json"
    "net/http"
    "time"
    
    "github.com/go-chi/chi/v5"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/types"
    "sigs.k8s.io/controller-runtime/pkg/client"
    
    ctfv1alpha1 "github.com/youruser/chall-operator/api/v1alpha1"
)

type Handler struct {
    client client.Client
}

func NewHandler(c client.Client) *Handler {
    return &Handler{client: c}
}

type CreateInstanceRequest struct {
    ChallengeID string            `json:"challenge_id"`
    SourceID    string            `json:"source_id"`
    Additional  map[string]string `json:"additional,omitempty"`
}

type InstanceResponse struct {
    ChallengeID    string   `json:"challenge_id"`
    SourceID       string   `json:"source_id"`
    ConnectionInfo string   `json:"connection_info"`
    Flags          []string `json:"flags"`
    Since          string   `json:"since"`
    Until          string   `json:"until,omitempty"`
}

func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
    var req CreateInstanceRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    ctx := context.Background()
    
    // Create ChallengeInstance CRD
    instanceName := req.ChallengeID + "-" + req.SourceID
    timeout := 600 // 10 minutes default
    
    instance := &ctfv1alpha1.ChallengeInstance{
        ObjectMeta: metav1.ObjectMeta{
            Name:      instanceName,
            Namespace: "ctf-instances",  // TODO: configurable
        },
        Spec: ctfv1alpha1.ChallengeInstanceSpec{
            ChallengeID:   req.ChallengeID,
            SourceID:      req.SourceID,
            ChallengeName: req.ChallengeID,  // Assume Challenge name = challengeID
            Additional:    req.Additional,
            Since:         metav1.Now(),
            Until:         &metav1.Time{Time: time.Now().Add(time.Duration(timeout) * time.Second)},
        },
    }
    
    if err := h.client.Create(ctx, instance); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Wait for instance to be ready (poll status)
    for i := 0; i < 30; i++ {  // 30 seconds timeout
        time.Sleep(1 * time.Second)
        
        if err := h.client.Get(ctx, types.NamespacedName{
            Name:      instanceName,
            Namespace: "ctf-instances",
        }, instance); err != nil {
            continue
        }
        
        if instance.Status.Ready {
            break
        }
    }
    
    // Return response
    resp := InstanceResponse{
        ChallengeID:    instance.Spec.ChallengeID,
        SourceID:       instance.Spec.SourceID,
        ConnectionInfo: instance.Status.ConnectionInfo,
        Flags:          instance.Status.Flags,
        Since:          instance.Spec.Since.Format(time.RFC3339),
    }
    
    if instance.Spec.Until != nil {
        resp.Until = instance.Spec.Until.Format(time.RFC3339)
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
    challengeID := chi.URLParam(r, "challengeId")
    sourceID := chi.URLParam(r, "sourceId")
    
    instanceName := challengeID + "-" + sourceID
    
    instance := &ctfv1alpha1.ChallengeInstance{}
    if err := h.client.Get(context.Background(), types.NamespacedName{
        Name:      instanceName,
        Namespace: "ctf-instances",
    }, instance); err != nil {
        http.Error(w, "Instance not found", http.StatusNotFound)
        return
    }
    
    resp := InstanceResponse{
        ChallengeID:    instance.Spec.ChallengeID,
        SourceID:       instance.Spec.SourceID,
        ConnectionInfo: instance.Status.ConnectionInfo,
        Flags:          instance.Status.Flags,
        Since:          instance.Spec.Since.Format(time.RFC3339),
    }
    
    if instance.Spec.Until != nil {
        resp.Until = instance.Spec.Until.Format(time.RFC3339)
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (h *Handler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
    challengeID := chi.URLParam(r, "challengeId")
    sourceID := chi.URLParam(r, "sourceId")
    
    instanceName := challengeID + "-" + sourceID
    
    instance := &ctfv1alpha1.ChallengeInstance{}
    if err := h.client.Get(context.Background(), types.NamespacedName{
        Name:      instanceName,
        Namespace: "ctf-instances",
    }, instance); err != nil {
        http.Error(w, "Instance not found", http.StatusNotFound)
        return
    }
    
    if err := h.client.Delete(context.Background(), instance); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.WriteHeader(http.StatusNoContent)
}
```

**Livrable:** API Gateway compatible CTFd

---

### Dimanche Matin (4h)
**Phase 5: Build & Deploy**

**Tâches:**
1. ✅ Créer Makefile

```makefile
.PHONY: build deploy install-crds test

# Build operator
build-operator:
	go build -o bin/operator cmd/operator/main.go

# Build API gateway
build-gateway:
	go build -o bin/api-gateway cmd/api-gateway/main.go

# Build Docker images
docker-build-operator:
	docker build -t chall-operator:latest -f Dockerfile.operator .

docker-build-gateway:
	docker build -t chall-operator-gateway:latest -f Dockerfile.gateway .

# Install CRDs
install-crds:
	kubectl apply -f config/crd/

# Deploy operator
deploy-operator:
	kubectl apply -f config/manager/

# Deploy API gateway
deploy-gateway:
	kubectl apply -f config/gateway/

# Full deploy
deploy: install-crds deploy-operator deploy-gateway

# Test
test:
	go test ./... -v
```

2. ✅ Créer Dockerfiles

**Dockerfile.operator:**
```dockerfile
FROM golang:1.21 as builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o operator cmd/operator/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/operator .
USER 65532:65532
ENTRYPOINT ["/operator"]
```

**Dockerfile.gateway:**
```dockerfile
FROM golang:1.21 as builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o api-gateway cmd/api-gateway/main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/api-gateway .
USER 65532:65532
ENTRYPOINT ["/api-gateway"]
```

3. ✅ Générer manifests K8s

```bash
make manifests
```

4. ✅ Créer namespace
```yaml
# config/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ctf-operator-system
---
apiVersion: v1
kind: Namespace
metadata:
  name: ctf-instances
```

**Livrable:** Builds + Dockerfiles

---

### Dimanche Après-midi (4h)
**Phase 6: Tests & Debug**

**Tâches:**
1. ✅ Déployer sur Kind cluster

```bash
# Load images to Kind
kind load docker-image chall-operator:latest --name devleo
kind load docker-image chall-operator-gateway:latest --name devleo

# Deploy
make deploy
```

2. ✅ Créer un Challenge de test

```yaml
# test-challenge.yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: test-pwn
  namespace: ctf-instances
spec:
  id: "1"
  scenario:
    image: nginx:alpine
    port: 80
    exposeType: NodePort
    flagTemplate: "FLAG{test_{{.SourceID}}_{{.RandomString}}}"
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
      requests:
        cpu: 100m
        memory: 128Mi
  timeout: 600
```

3. ✅ Tester via API Gateway

```bash
# Create instance
curl -X POST http://localhost:8080/api/v1/instance \
  -H "Content-Type: application/json" \
  -d '{
    "challenge_id": "1",
    "source_id": "user-123"
  }'

# Get instance
curl http://localhost:8080/api/v1/instance/1/user-123

# Delete instance
curl -X DELETE http://localhost:8080/api/v1/instance/1/user-123
```

4. ✅ Debug issues

**Common issues:**
- RBAC permissions → Check ServiceAccount/Role/RoleBinding
- CRD not installed → `kubectl get crds`
- Controller crash → `kubectl logs -n ctf-operator-system`
- Service not ready → `kubectl get svc -n ctf-instances`

**Livrable:** MVP fonctionnel testé

---

### Dimanche Soir (2h)
**Phase 7: Documentation**

**Tâches:**
1. ✅ README.md avec quickstart
2. ✅ Exemples de Challenge CRDs
3. ✅ Instructions de déploiement

**Livrable:** Documentation

---

## 🚀 Commandes Essentielles

### Bootstrap (Vendredi)
```bash
cd /home/leo/Documents/3-TROISIEME/PROJECTSEM1/chall-operator

# Init projet
kubebuilder init --domain ctf.io --repo github.com/leo/chall-operator

# Create APIs
kubebuilder create api --group ctf --version v1alpha1 --kind Challenge --resource --controller=false
kubebuilder create api --group ctf --version v1alpha1 --kind ChallengeInstance --resource --controller

# Generate
make manifests
make generate
```

### Build (Dimanche)
```bash
# Build binaries
make build-operator
make build-gateway

# Build Docker images
make docker-build-operator
make docker-build-gateway

# Load to Kind
kind load docker-image chall-operator:latest --name devleo
kind load docker-image chall-operator-gateway:latest --name devleo
```

### Deploy (Dimanche)
```bash
# Install CRDs
kubectl apply -f config/crd/

# Deploy operator
kubectl apply -f config/manager/

# Deploy API gateway
kubectl apply -f config/gateway/

# Check status
kubectl get pods -n ctf-operator-system
kubectl logs -n ctf-operator-system -l app=chall-operator
```

### Test (Dimanche)
```bash
# Create test challenge
kubectl apply -f test-challenge.yaml

# Port-forward API gateway
kubectl port-forward -n ctf-operator-system svc/api-gateway 8080:8080

# Test API
curl -X POST http://localhost:8080/api/v1/instance \
  -d '{"challenge_id":"1","source_id":"user-123"}'
```

---

## 🎯 Success Criteria (Lundi)

### Critères de validation MVP:
1. ✅ CRDs installés et validés (`kubectl get crds`)
2. ✅ Operator tourne sans crash (`kubectl get pods -n ctf-operator-system`)
3. ✅ API Gateway accessible (`curl http://localhost:8080/health`)
4. ✅ Création d'instance fonctionnelle
5. ✅ Deployment + Service créés automatiquement
6. ✅ Flag généré unique par instance
7. ✅ Connection info retourné à l'API
8. ✅ Suppression d'instance clean (owner reference)

---

## ⚠️ Points d'Attention

### Performance
- Instance creation: viser < 30 secondes
- Controller reconciliation: 10 secondes max

### Sécurité MVP
- Pas de network policies (Phase 2)
- Isolation par namespace
- RBAC minimal mais fonctionnel

### Limitations MVP
- Pas de pooling → Création à la demande
- Pas de janitor → Expiry check dans controller
- Pas de renewal → Timeout fixe
- 1 container par challenge (pas multi-container)
- NodePort ou LoadBalancer uniquement (pas Ingress)

---

## 📦 Dependencies

```go
// go.mod minimal
module github.com/leo/chall-operator

go 1.21

require (
    github.com/go-chi/chi/v5 v5.0.10
    k8s.io/api v0.28.3
    k8s.io/apimachinery v0.28.3
    k8s.io/client-go v0.28.3
    sigs.k8s.io/controller-runtime v0.16.3
)
```

---

## 🔄 Phases Post-MVP (Après Lundi)

### Phase 2 (Semaine 2): Stabilisation
- Instance pooling
- Janitor controller
- Renewal mechanism
- Metrics basiques

### Phase 3 (Semaine 3): Features Avancées
- Cilium Network Policies
- AttackBox CRD
- Multi-container challenges
- Ingress support

### Phase 4 (Semaine 4): Production
- HA operator (multiple replicas)
- Webhooks de validation
- E2E tests
- Documentation complète

---

## 🐛 Troubleshooting

### Operator ne démarre pas
```bash
kubectl describe pod -n ctf-operator-system
kubectl logs -n ctf-operator-system -l app=chall-operator
```

### CRD non reconnu
```bash
kubectl get crds | grep ctf.io
kubectl apply -f config/crd/
```

### Instance reste Pending
```bash
kubectl describe challengeinstance -n ctf-instances <name>
kubectl get events -n ctf-instances
```

### Service pas de NodePort
```bash
kubectl get svc -n ctf-instances
kubectl describe svc -n ctf-instances <name>
```

---

## ✅ Checklist Finale (Lundi Soir)

- [ ] Operator build sans erreur
- [ ] API Gateway build sans erreur
- [ ] CRDs installés
- [ ] Operator deployed et Running
- [ ] API Gateway deployed et accessible
- [ ] Test: Créer instance via API
- [ ] Test: Flag généré et retourné
- [ ] Test: Connection info valide
- [ ] Test: Supprimer instance
- [ ] README.md avec quickstart
- [ ] Demo fonctionnelle

**Objectif:** Tout coché lundi 18h max.

---

## 🎬 Next Steps Immédiat

**MAINTENANT (pour l'agent de code):**

1. `cd /home/leo/Documents/3-TROISIEME/PROJECTSEM1/chall-operator`
2. `kubebuilder init --domain ctf.io --repo github.com/leo/chall-operator`
3. Suivre Phase 1 du plan
4. Commit after each phase
5. Update README.md avec progrès

**Go go go! 🚀**
