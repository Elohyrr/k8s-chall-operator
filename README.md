# Chall-Operator

**Kubernetes-native operator** for managing dynamic CTF challenge instances.

Replaces `chall-manager` with a cloud-native approach using CRDs and controllers.

---

## 🎯 Features

- ✅ **Flags uniques** par instance (template Go)
- ✅ **Lifecycle management** (création, expiration auto, suppression)
- ✅ **API compatible CTFd**
- ✅ **Auth Proxy sidecar** (vérifie l'identité utilisateur via OAuth2)
- ✅ **AttackBox** (terminal web ttyd pour chaque instance)
- ✅ **Ingress** avec OAuth2 annotations
- ✅ **NetworkPolicy** pour isolation des attackbox
- ✅ **Janitor** (cleanup auto à expiration ou après flag validé)
- ✅ **< 30s** pour créer une instance (vs 10+ min avec Pulumi)

---

## 🏗️ Architecture

```
CTFd → API Gateway (port 8080) → ChallengeInstance CRD → Controller → K8s Resources
                                        ↓
                                 Challenge CRD (template)
```

**Ressources créées par instance:**
- `Deployment` (challenge + auth-proxy sidecar)
- `Service` (ClusterIP/NodePort/LoadBalancer)
- `Deployment` AttackBox (si activé)
- `Service` AttackBox (si activé)
- `Ingress` (si `exposeType: Ingress`)
- `NetworkPolicy` (si activé)

---

## 📦 Installation

### Prérequis
- Kubernetes cluster (Kind, K3s, AWS EKS, etc.)
- `kubectl` configuré
- `kubebuilder` v3.12+ (pour dev)
- Go 1.21+ (pour build)

### Quick Deploy

```bash
# 1. Install CRDs
kubectl apply -f config/crd/

# 2. Create namespaces
kubectl create namespace ctf-operator-system
kubectl create namespace ctf-instances

# 3. Deploy operator
kubectl apply -f config/manager/

# 4. Deploy API gateway
kubectl apply -f config/gateway/

# 5. Verify
kubectl get pods -n ctf-operator-system
```

---

## 🚀 Quick Start

### 1. Créer un Challenge (template)

Le Challenge CRD définit **comment** déployer un challenge. Le `metadata.name` est l'ID utilisé par l'API.

#### Challenge Simple (NodePort)

```yaml
apiVersion: ctf.ctf.io/v1alpha1
kind: Challenge
metadata:
  name: simple-web        # ← C'est le challenge_id pour l'API
  namespace: ctf-instances
spec:
  id: "simple-web"
  scenario:
    image: nginx:alpine
    port: 80
    exposeType: NodePort  # NodePort, LoadBalancer, ou Ingress
    flagTemplate: 'FLAG{{"{"}}{{.ChallengeID}}_{{.RandomString}}{{"}"}}'
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
  timeout: 300  # secondes avant expiration
```

#### Challenge Complet (AuthProxy + AttackBox + Ingress + NetworkPolicy)

```yaml
apiVersion: ctf.ctf.io/v1alpha1
kind: Challenge
metadata:
  name: full-stack
  namespace: ctf-instances
spec:
  id: "full-stack"
  scenario:
    image: my-vuln-app:latest
    port: 8080
    exposeType: Ingress
    flagTemplate: 'CTF{{"{"}}{{.InstanceID}}_{{.SourceID}}{{"}"}}'
    resources:
      limits:
        cpu: 100m
        memory: 128Mi
    
    # Auth Proxy - vérifie X-Auth-Request-Email == user
    authProxy:
      enabled: true
      image: ctf-auth-proxy:simple
    
    # AttackBox - terminal web pour l'utilisateur
    attackBox:
      enabled: true
      image: attack-box:latest
      port: 7681
    
    # Ingress avec OAuth2
    ingress:
      enabled: true
      hostTemplate: "{{.InstanceName}}.{{.ChallengeID}}.ctf.local"
      ingressClassName: nginx
      annotations:
        nginx.ingress.kubernetes.io/auth-url: "http://oauth2-proxy.svc/oauth2/auth"
        nginx.ingress.kubernetes.io/auth-signin: "http://auth.ctf.local/oauth2/start"
    
    # NetworkPolicy pour isoler l'attackbox
    networkPolicy:
      enabled: true
      allowDNS: true
      allowInternet: true
  timeout: 600
```

```bash
kubectl apply -f challenge.yaml
```

### 2. Créer une Instance via API

```bash
# Port-forward API gateway
kubectl port-forward -n chall-operator-system svc/api-gateway 8080:8080

# Créer instance (challenge_id = metadata.name du Challenge)
curl -X POST http://localhost:8080/api/v1/instance \
  -H "Content-Type: application/json" \
  -d '{
    "challenge_id": "simple-web",
    "source_id": "user@ctf.local"
  }'
```

**Response:**
```json
{
  "challenge_id": "simple-web",
  "source_id": "user@ctf.local",
  "connection_info": "nc localhost 31155",
  "flags": ["FLAG{simple-web_a1b2c3d4e5f6}"],
  "since": "2026-01-17T22:00:00Z",
  "until": "2026-01-17T22:05:00Z"
}
```

### 3. Autres endpoints API

```bash
# Récupérer une instance
curl http://localhost:8080/api/v1/instance/simple-web/user@ctf.local

# Lister les instances d'un user
curl "http://localhost:8080/api/v1/instance?source_id=user@ctf.local"

# Valider un flag (déclenche cleanup auto)
curl -X POST http://localhost:8080/api/v1/instance/simple-web/user@ctf.local/validate \
  -H "Content-Type: application/json" \
  -d '{"flag": "FLAG{simple-web_a1b2c3d4e5f6}"}'

# Renouveler une instance (reset timeout)
curl -X POST http://localhost:8080/api/v1/instance/simple-web/user@ctf.local/renew

# Supprimer une instance
curl -X DELETE http://localhost:8080/api/v1/instance/simple-web/user@ctf.local
```

---

## 🔧 Development

### Bootstrap projet

```bash
cd /path/to/chall-operator

# Init with Kubebuilder
kubebuilder init --domain ctf.io --repo github.com/youruser/chall-operator

# Create CRDs
kubebuilder create api --group ctf --version v1alpha1 --kind Challenge --resource --controller=false
kubebuilder create api --group ctf --version v1alpha1 --kind ChallengeInstance --resource --controller

# Generate manifests
make manifests
make generate
```

### Build

```bash
# Build binaries
make build-operator
make build-gateway

# Build Docker images
docker build -t chall-operator:latest -f Dockerfile.operator .
docker build -t chall-operator-gateway:latest -f Dockerfile.gateway .

# Load to Kind (if using Kind)
kind load docker-image chall-operator:latest --name <cluster-name>
kind load docker-image chall-operator-gateway:latest --name <cluster-name>
```

### Test

```bash
# Unit tests
go test ./... -v

# Integration test
kubectl apply -f config/samples/

# Check logs
kubectl logs -n ctf-operator-system -l app=chall-operator -f
```

---

## 📖 API Reference

### POST /api/v1/instance

Crée une nouvelle instance de challenge.

**Request:**
```json
{
  "challenge_id": "1",
  "source_id": "user-123",
  "additional": {
    "team": "myteam"
  }
}
```

**Response:**
```json
{
  "challenge_id": "1",
  "source_id": "user-123",
  "connection_info": "nc ctf.dev.local 31337",
  "flags": ["FLAG{unique-flag}"],
  "since": "2025-01-17T12:00:00Z",
  "until": "2025-01-17T12:10:00Z"
}
```

### GET /api/v1/instance/{challengeId}/{sourceId}

Récupère les informations d'une instance.

### DELETE /api/v1/instance/{challengeId}/{sourceId}

Supprime une instance.

---

## 📁 Structure du Projet

```
chall-operator/
├── api/v1alpha1/              # CRD definitions
├── controllers/               # Reconcilers
├── cmd/
│   ├── operator/             # Operator entrypoint
│   └── api-gateway/          # API Gateway entrypoint
├── pkg/
│   ├── api/                  # HTTP handlers
│   ├── flaggen/              # Flag generation
│   └── builder/              # K8s resource builders
├── config/
│   ├── crd/                  # Generated CRDs
│   ├── manager/              # Operator deployment
│   └── gateway/              # Gateway deployment
└── README.md
```

---

## 🔐 RBAC

L'opérateur nécessite les permissions suivantes:

```yaml
- apiGroups: ["ctf.io"]
  resources: ["challenges", "challengeinstances"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

---

## ⚙️ Configuration

### Environment Variables (API Gateway)

- `PORT`: Port d'écoute (défaut: 8080)
- `KUBECONFIG`: Path to kubeconfig (pour dev local)
- `DEFAULT_NAMESPACE`: Namespace pour les instances (défaut: ctf-instances)

### Environment Variables (Operator)

- `METRICS_ADDR`: Metrics endpoint (défaut: :8081)
- `HEALTH_PROBE_ADDR`: Health probe endpoint (défaut: :8082)

---

## 🐛 Troubleshooting

### Operator crash loop

```bash
kubectl logs -n ctf-operator-system -l app=chall-operator
kubectl describe pod -n ctf-operator-system <pod-name>
```

**Causes communes:**
- CRDs pas installés → `kubectl apply -f config/crd/`
- RBAC insuffisant → Vérifier ServiceAccount/Role
- Image pas loaded dans Kind → `kind load docker-image`

### Instance reste en Pending

```bash
kubectl describe challengeinstance -n ctf-instances <name>
kubectl get events -n ctf-instances
```

**Causes communes:**
- Challenge CRD n'existe pas
- Image du challenge inaccessible
- Resources insuffisantes

### API Gateway inaccessible

```bash
kubectl get svc -n ctf-operator-system
kubectl logs -n ctf-operator-system -l app=api-gateway
```

**Causes communes:**
- Service pas créé
- Port-forward pas actif
- Firewall bloque le port

---

## 📝 Roadmap

### MVP ✅
- [x] CRDs Challenge + ChallengeInstance
- [x] Controller basique
- [x] Flag generation (Go templates)
- [x] API Gateway compatible CTFd
- [x] Deployment + Service creation

### Phase 2 ✅
- [x] Janitor controller (expiration auto)
- [x] Flag validation → cleanup auto
- [x] Renewal mechanism
- [x] Auth Proxy sidecar (port 8888)

### Phase 3 ✅
- [x] NetworkPolicy pour AttackBox
- [x] AttackBox (terminal web ttyd)
- [x] Multi-container (challenge + auth-proxy)
- [x] Ingress support avec OAuth2 annotations

### Phase 4 (TODO)
- [ ] Metrics Prometheus
- [ ] Webhooks validation
- [ ] E2E tests automatisés
- [ ] Production hardening
- [ ] Hot Proxy integration

---

## 🤝 Contributing

1. Fork le repo
2. Créer une branche (`git checkout -b feature/amazing`)
3. Commit (`git commit -am 'Add amazing feature'`)
4. Push (`git push origin feature/amazing`)
5. Open Pull Request

---

## 📄 License

MIT License - See LICENSE file

---

## 🔗 Links

- [Kubebuilder](https://book.kubebuilder.io/)
- [Controller Runtime](https://github.com/kubernetes-sigs/controller-runtime)
- [CTFd](https://github.com/CTFd/CTFd)
- [Original OPERATOR.md specs](./OPERATOR.md)

---

## 🎯 Goals vs chall-manager

| Feature | chall-manager | chall-operator |
|---------|---------------|----------------|
| Instance creation | ❌ 10+ min | ✅ < 30s |
| Cloud-native | ❌ Pulumi in container | ✅ CRDs natifs |
| GitOps | ❌ OCI scenarios | ✅ YAML declaratif |
| Network isolation | ⚠️ Manual | ✅ CNP auto (Phase 2) |
| Debugging | ❌ Opaque | ✅ kubectl describe |
| Image size | ❌ 500MB+ | ✅ <50MB |

---

**Status:** ✅ Full Stack Fonctionnel (MVP + Auth + AttackBox + Ingress + NetworkPolicy)  
**Tested:** 17 janvier 2026 sur Kind cluster  
**Maintainer:** @leo

---

## ⚠️ Important: exposeType et Ingress

| `exposeType` | Service Type | Ingress créé ? | Cas d'usage |
|--------------|--------------|----------------|-------------|
| `NodePort` | NodePort | ❌ Non | Dev local, accès direct via port |
| `LoadBalancer` | LoadBalancer | ❌ Non | Cloud avec LB externe |
| `Ingress` | ClusterIP | ✅ Oui | Production avec nginx-ingress |

**L'Ingress n'est créé que si `exposeType: Ingress`** dans le Challenge spec.
