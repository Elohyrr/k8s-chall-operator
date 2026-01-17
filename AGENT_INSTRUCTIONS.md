# Instructions pour l'Agent de Code

## 🎯 Mission

Implémenter **chall-operator** - Un opérateur Kubernetes pour gérer des challenges CTF dynamiques.

**Deadline:** Lundi 20 janvier 2025 18h  
**Scope:** MVP fonctionnel (pas de pooling/janitor dans MVP)

---

## 📖 Documents de Référence

1. **`DEVELOPMENT_PLAN.md`** - Plan détaillé par phases (Ven → Lun)
2. **`README.md`** - Documentation du projet
3. **`../local-kube-stack/OPERATOR.md`** - Specs complètes de l'architecture

**IMPORTANT:** Suis le plan dans `DEVELOPMENT_PLAN.md` **phase par phase**. Ne saute pas d'étapes.

---

## 🚦 Workflow de Travail

### Règles Générales

1. ✅ **Une phase à la fois** - Valide chaque phase avant de passer à la suivante
2. ✅ **Commit après chaque phase** - Git commit avec message descriptif
3. ✅ **Build & test** - Compile et teste après chaque changement majeur
4. ✅ **Documente** - Ajoute des commentaires dans le code
5. ❌ **Pas de raccourcis** - Implémente tout proprement même si c'est MVP

### Checklist par Phase

#### Phase 1: Bootstrap & CRDs (Vendredi Soir - 4h)

**Actions:**
- [ ] `kubebuilder init --domain ctf.io --repo github.com/leo/chall-operator`
- [ ] `kubebuilder create api --group ctf --version v1alpha1 --kind Challenge --resource --controller=false`
- [ ] `kubebuilder create api --group ctf --version v1alpha1 --kind ChallengeInstance --resource --controller`
- [ ] Éditer `api/v1alpha1/challenge_types.go` avec la spec du plan
- [ ] Éditer `api/v1alpha1/challengeinstance_types.go` avec la spec du plan
- [ ] `make manifests`
- [ ] `make generate`
- [ ] Vérifier que `config/crd/` contient les CRDs générés
- [ ] `git add -A && git commit -m "feat: bootstrap project with CRDs"`

**Validation:**
```bash
# Doit afficher les CRDs
ls config/crd/bases/

# Doit compiler sans erreur
go build ./...
```

**Problèmes potentiels:**
- Kubebuilder pas installé → Installer avec `go install sigs.k8s.io/kubebuilder/v3/cmd@latest`
- Erreurs de génération → Vérifier syntaxe des types Go

---

#### Phase 2: Controller Core (Samedi Matin - 4h)

**Actions:**
- [ ] Créer `pkg/builder/deployment.go` avec fonction `BuildDeployment()`
- [ ] Créer `pkg/builder/service.go` avec fonction `BuildService()`
- [ ] Éditer `controllers/challengeinstance_controller.go` avec logique Reconcile
- [ ] Implémenter owner references (SetControllerReference)
- [ ] Ajouter watch sur Deployments et Services
- [ ] `go mod tidy`
- [ ] Compiler: `go build -o bin/operator cmd/manager/main.go`
- [ ] `git commit -m "feat: implement instance controller with builders"`

**Validation:**
```bash
# Compilation sans erreur
go build ./...

# Vérifier imports
go mod tidy

# Tests unitaires (si tu as le temps)
go test ./pkg/builder/... -v
```

**Code critique:**
- ✅ Owner references pour cascade delete
- ✅ Error handling dans Reconcile
- ✅ Requeue logic (10 secondes)

---

#### Phase 3: Flag Generation (Samedi Après-midi - 4h)

**Actions:**
- [ ] Créer `pkg/flaggen/generator.go`
- [ ] Implémenter fonction `Generate(template, instanceID, sourceID, challengeID)`
- [ ] Utiliser `crypto/rand` pour random string
- [ ] Parser template avec `text/template`
- [ ] Intégrer dans controller (générer flag dans Reconcile)
- [ ] Stocker flag dans `instance.Status.Flags`
- [ ] Tester avec différents templates
- [ ] `git commit -m "feat: add flag generation with templates"`

**Validation:**
```bash
# Test flag generation
go test ./pkg/flaggen/... -v

# Vérifier que flag change à chaque run
```

**Templates à tester:**
- `FLAG{{{.ChallengeID}}_{{.SourceID}}_{{.RandomString}}}`
- `FLAG{{{.RandomString}}}`
- `CTF{test_{{.SourceID}}}`

---

#### Phase 4: API Gateway (Samedi Soir - 4h)

**Actions:**
- [ ] Créer `cmd/api-gateway/main.go`
- [ ] Créer `pkg/api/handlers.go` avec Handler struct
- [ ] Implémenter `CreateInstance()` handler
- [ ] Implémenter `GetInstance()` handler
- [ ] Implémenter `DeleteInstance()` handler
- [ ] Ajouter endpoint `/health`
- [ ] Setup Chi router
- [ ] Initialiser K8s client avec controller-runtime
- [ ] Compiler: `go build -o bin/api-gateway cmd/api-gateway/main.go`
- [ ] `git commit -m "feat: add API gateway with CTFd-compatible endpoints"`

**Validation:**
```bash
# Compile
go build -o bin/api-gateway cmd/api-gateway/main.go

# Test local (sans K8s d'abord)
./bin/api-gateway
curl http://localhost:8080/health
# Devrait retourner "OK"
```

**Points d'attention:**
- ✅ JSON encoding/decoding correct
- ✅ Error handling (404, 500)
- ✅ Attente que instance soit Ready (polling 30s max)

---

#### Phase 5: Build & Deploy (Dimanche Matin - 4h)

**Actions:**
- [ ] Créer `Makefile` avec targets build/deploy
- [ ] Créer `Dockerfile.operator`
- [ ] Créer `Dockerfile.gateway`
- [ ] Créer `config/namespace.yaml` (ctf-operator-system, ctf-instances)
- [ ] Créer `config/manager/deployment.yaml` pour operator
- [ ] Créer `config/gateway/deployment.yaml` pour gateway
- [ ] Créer `config/rbac/` avec ServiceAccount, Role, RoleBinding
- [ ] Build images: `make docker-build-operator docker-build-gateway`
- [ ] Load to Kind: `kind load docker-image chall-operator:latest --name devleo`
- [ ] Deploy: `make deploy`
- [ ] `git commit -m "build: add Dockerfiles and K8s manifests"`

**Validation:**
```bash
# Build Docker images
docker build -t chall-operator:latest -f Dockerfile.operator .
docker build -t chall-operator-gateway:latest -f Dockerfile.gateway .

# Load to Kind
kind load docker-image chall-operator:latest --name devleo
kind load docker-image chall-operator-gateway:latest --name devleo

# Deploy
kubectl apply -f config/namespace.yaml
kubectl apply -f config/crd/
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
kubectl apply -f config/gateway/

# Check
kubectl get pods -n ctf-operator-system
# Les deux pods doivent être Running
```

**RBAC minimal requis:**
```yaml
- apiGroups: ["ctf.io"]
  resources: ["challenges", "challengeinstances", "challengeinstances/status"]
  verbs: ["*"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["*"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["*"]
```

---

#### Phase 6: Tests & Debug (Dimanche Après-midi - 4h)

**Actions:**
- [ ] Créer `config/samples/challenge-test.yaml`
- [ ] Apply challenge: `kubectl apply -f config/samples/challenge-test.yaml`
- [ ] Port-forward gateway: `kubectl port-forward -n ctf-operator-system svc/api-gateway 8080:8080`
- [ ] Test CREATE: `curl -X POST http://localhost:8080/api/v1/instance -d '{"challenge_id":"1","source_id":"user-123"}'`
- [ ] Vérifier ChallengeInstance créé: `kubectl get challengeinstance -n ctf-instances`
- [ ] Vérifier Deployment créé: `kubectl get deployment -n ctf-instances`
- [ ] Vérifier Service créé: `kubectl get svc -n ctf-instances`
- [ ] Vérifier flag dans status: `kubectl get challengeinstance -n ctf-instances -o yaml`
- [ ] Test GET: `curl http://localhost:8080/api/v1/instance/1/user-123`
- [ ] Test DELETE: `curl -X DELETE http://localhost:8080/api/v1/instance/1/user-123`
- [ ] Vérifier cascade delete (Deployment + Service supprimés aussi)
- [ ] Check logs pour erreurs: `kubectl logs -n ctf-operator-system -l app=chall-operator`
- [ ] Documenter bugs trouvés et fixes
- [ ] `git commit -m "test: add sample challenge and validate MVP"`

**Validation complète:**
```bash
# 1. Create instance
RESPONSE=$(curl -X POST http://localhost:8080/api/v1/instance \
  -H "Content-Type: application/json" \
  -d '{"challenge_id":"1","source_id":"user-123"}')

echo $RESPONSE | jq .
# Doit contenir: challenge_id, source_id, connection_info, flags

# 2. Vérifier ressources K8s
kubectl get challengeinstance,deployment,svc -n ctf-instances
# Doit montrer 1 instance, 1 deployment, 1 service

# 3. Get instance
curl http://localhost:8080/api/v1/instance/1/user-123 | jq .

# 4. Delete instance
curl -X DELETE http://localhost:8080/api/v1/instance/1/user-123

# 5. Vérifier cleanup
kubectl get challengeinstance,deployment,svc -n ctf-instances
# Doit être vide (cascade delete)
```

**Debug commun:**
- Operator crash → `kubectl logs -n ctf-operator-system -l app=chall-operator`
- Instance Pending → `kubectl describe challengeinstance -n ctf-instances <name>`
- Service pas de NodePort → Vérifier `exposeType` dans Challenge
- Flag vide → Vérifier `flagTemplate` dans Challenge
- Connection info vide → Vérifier que Service a un NodePort assigné

---

#### Phase 7: Documentation (Dimanche Soir - 2h)

**Actions:**
- [ ] Mettre à jour `README.md` avec résultats des tests
- [ ] Ajouter section Troubleshooting avec bugs trouvés
- [ ] Créer `docs/quickstart.md` avec demo complète
- [ ] Créer `docs/api.md` avec exemples curl
- [ ] Ajouter badges dans README (build status si possible)
- [ ] Screenshots ou asciicast de la demo
- [ ] `git commit -m "docs: complete documentation with examples"`
- [ ] Tag release: `git tag v0.1.0-mvp`

---

## 🎯 Critères de Succès MVP

### Must Have (Bloquants pour validation)
- [x] CRDs installés sans erreur
- [x] Operator Running (pas de crash loop)
- [x] API Gateway accessible
- [x] POST /api/v1/instance crée ChallengeInstance + Deployment + Service
- [x] Flag généré unique par instance
- [x] Connection info retourné (format `nc <ip> <port>`)
- [x] GET /api/v1/instance retourne status
- [x] DELETE /api/v1/instance supprime tout (cascade)

### Nice to Have (Bonus)
- [ ] Tests unitaires (au moins flaggen)
- [ ] Metrics endpoint dans operator
- [ ] Validation webhook pour CRDs
- [ ] Multiple flags support
- [ ] Custom connection info template

---

## 🔧 Outils Requis

```bash
# Installer Kubebuilder
go install sigs.k8s.io/kubebuilder/v3/cmd@latest

# Vérifier version Go
go version  # >= 1.21

# Vérifier kubectl
kubectl version --client

# Vérifier Kind cluster
kubectl get nodes
```

---

## 📦 Structure Cible Finale

```
chall-operator/
├── api/
│   └── v1alpha1/
│       ├── challenge_types.go
│       ├── challengeinstance_types.go
│       ├── groupversion_info.go
│       └── zz_generated.deepcopy.go
├── controllers/
│   ├── challengeinstance_controller.go
│   └── suite_test.go
├── cmd/
│   ├── manager/
│   │   └── main.go                    # Kubebuilder default
│   └── api-gateway/
│       └── main.go
├── pkg/
│   ├── api/
│   │   └── handlers.go
│   ├── flaggen/
│   │   ├── generator.go
│   │   └── generator_test.go
│   └── builder/
│       ├── deployment.go
│       └── service.go
├── config/
│   ├── crd/
│   │   └── bases/
│   ├── manager/
│   │   └── deployment.yaml
│   ├── gateway/
│   │   ├── deployment.yaml
│   │   └── service.yaml
│   ├── rbac/
│   │   ├── serviceaccount.yaml
│   │   ├── role.yaml
│   │   └── rolebinding.yaml
│   ├── namespace.yaml
│   └── samples/
│       └── challenge-test.yaml
├── docs/
│   ├── quickstart.md
│   └── api.md
├── hack/
│   └── boilerplate.go.txt
├── Dockerfile.operator
├── Dockerfile.gateway
├── Makefile
├── go.mod
├── go.sum
├── PROJECT
├── README.md
├── DEVELOPMENT_PLAN.md
└── AGENT_INSTRUCTIONS.md (ce fichier)
```

---

## 🚨 Points d'Attention Critiques

### 1. Owner References
**CRUCIAL pour cascade delete!**

```go
import "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

// Dans Reconcile, lors de création Deployment/Service:
controllerutil.SetControllerReference(instance, deployment, r.Scheme)
```

### 2. Status vs Spec
- **Spec** = desired state (user input)
- **Status** = observed state (controller output)

**Toujours update status séparément:**
```go
// Update status
instance.Status.Phase = "Running"
r.Status().Update(ctx, instance)
```

### 3. Requeue Logic
```go
// Requeue after 10s pour check readiness
return ctrl.Result{RequeueAfter: 10 * time.Second}, nil

// Pas de requeue (done)
return ctrl.Result{}, nil

// Requeue immédiatement (error)
return ctrl.Result{}, err
```

### 4. Connection Info Format
Pour NodePort:
```go
connInfo := fmt.Sprintf("nc %s %d", nodeIP, nodePort)
```

Pour LoadBalancer:
```go
connInfo := fmt.Sprintf("nc %s %d", loadBalancerIP, port)
```

### 5. Flag Generation - Sécurité
```go
// BON: crypto/rand
randomBytes := make([]byte, 16)
rand.Read(randomBytes)

// MAUVAIS: math/rand (pas sécurisé)
```

### 6. API Gateway - Polling
```go
// Attendre que instance soit Ready avant de return
for i := 0; i < 30; i++ {
    time.Sleep(1 * time.Second)
    r.client.Get(ctx, key, instance)
    if instance.Status.Ready {
        break
    }
}
```

---

## 🐛 Troubleshooting Guide

### Operator ne démarre pas
```bash
# Check logs
kubectl logs -n ctf-operator-system deployment/chall-operator

# Common issues:
# - CRDs pas installés → kubectl apply -f config/crd/
# - RBAC manquant → kubectl apply -f config/rbac/
# - Image pas dans Kind → kind load docker-image
```

### ChallengeInstance reste Pending
```bash
kubectl describe challengeinstance -n ctf-instances <name>

# Check events
kubectl get events -n ctf-instances --sort-by='.lastTimestamp'

# Common issues:
# - Challenge CRD pas trouvé
# - Image pull error
# - Resources insuffisantes
```

### API retourne 500
```bash
kubectl logs -n ctf-operator-system deployment/api-gateway

# Common issues:
# - K8s client pas initialisé
# - Namespace "ctf-instances" n'existe pas
# - JSON encoding error
```

### Cascade delete ne marche pas
```bash
# Vérifier owner reference
kubectl get deployment -n ctf-instances <name> -o yaml | grep ownerReferences

# Doit montrer:
# ownerReferences:
# - apiVersion: ctf.io/v1alpha1
#   kind: ChallengeInstance
#   controller: true
```

---

## ✅ Checklist Validation Finale (Lundi 18h)

**Avant de déclarer MVP terminé, vérifier:**

### Build & Deploy
- [ ] `go build ./...` → pas d'erreur
- [ ] `docker build -f Dockerfile.operator .` → succès
- [ ] `docker build -f Dockerfile.gateway .` → succès
- [ ] `kubectl get crds | grep ctf.io` → 2 CRDs
- [ ] `kubectl get pods -n ctf-operator-system` → 2 pods Running

### Functional Tests
- [ ] Create instance via API → 201 OK
- [ ] Instance status = Running dans 30s
- [ ] Flag présent dans response
- [ ] Connection info valide
- [ ] `kubectl get deployment -n ctf-instances` → 1 deployment
- [ ] `kubectl get svc -n ctf-instances` → 1 service NodePort
- [ ] Get instance via API → 200 OK
- [ ] Delete instance via API → 204 No Content
- [ ] Resources K8s supprimés (cascade)

### Code Quality
- [ ] Pas de `TODO` ou `FIXME` dans code critique
- [ ] Error handling partout
- [ ] Logs informatifs
- [ ] Commentaires sur code complexe
- [ ] go.mod à jour (`go mod tidy`)

### Documentation
- [ ] README.md complet avec quickstart
- [ ] Exemples de Challenge CRD
- [ ] API endpoints documentés
- [ ] Troubleshooting section

---

## 🎬 Commencer Maintenant

```bash
cd /home/leo/Documents/3-TROISIEME/PROJECTSEM1/chall-operator

# Phase 1: Bootstrap
kubebuilder init --domain ctf.io --repo github.com/leo/chall-operator

# Puis suivre DEVELOPMENT_PLAN.md phase par phase
```

**Bon courage! 🚀**

**En cas de blocage:**
1. Check logs operator/gateway
2. Consulter DEVELOPMENT_PLAN.md
3. Référence: https://book.kubebuilder.io/
4. Demander à Leo si vraiment bloqué

**Objectif: MVP fonctionnel lundi 18h. Go!**
