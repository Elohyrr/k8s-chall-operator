# Chall-Operator

**Kubernetes-native operator** for managing dynamic CTF challenge instances.

Replaces `chall-manager` with a cloud-native approach using CRDs and controllers.

---

## 🎯 Objectif

Déployer des challenges CTF dynamiques (1 instance par user/team) avec:
- ✅ Flags uniques par instance
- ✅ Lifecycle management (création, suppression)
- ✅ API compatible CTFd
- ✅ Architecture cloud-native (CRDs)
- ❌ Plus de Pulumi timeout!

---

## 🏗️ Architecture

```
CTFd → API Gateway (port 8080) → CRDs → Operator → Deployments + Services
```

**Composants:**
1. **CRDs**: `Challenge`, `ChallengeInstance`
2. **Operator**: Reconcile loop qui crée Deployment + Service
3. **API Gateway**: API REST compatible CTFd

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

### 1. Créer un Challenge

```yaml
# example-challenge.yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: web-exploit
  namespace: ctf-instances
spec:
  id: "1"
  scenario:
    image: nginx:alpine
    port: 80
    exposeType: NodePort
    flagTemplate: "FLAG{web_{{.SourceID}}_{{.RandomString}}}"
    env:
    - name: CUSTOM_VAR
      value: "test"
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
      requests:
        cpu: 100m
        memory: 128Mi
  timeout: 600
```

```bash
kubectl apply -f example-challenge.yaml
```

### 2. Créer une Instance via API

```bash
# Port-forward API gateway
kubectl port-forward -n ctf-operator-system svc/api-gateway 8080:8080

# Créer instance
curl -X POST http://localhost:8080/api/v1/instance \
  -H "Content-Type: application/json" \
  -d '{
    "challenge_id": "1",
    "source_id": "user-123"
  }'

# Response
{
  "challenge_id": "1",
  "source_id": "user-123",
  "connection_info": "nc <node-ip> <node-port>",
  "flags": ["FLAG{web_user-123_a1b2c3d4e5f6...}"],
  "since": "2025-01-17T12:00:00Z",
  "until": "2025-01-17T12:10:00Z"
}
```

### 3. Récupérer une Instance

```bash
curl http://localhost:8080/api/v1/instance/1/user-123
```

### 4. Supprimer une Instance

```bash
curl -X DELETE http://localhost:8080/api/v1/instance/1/user-123
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

### MVP (Semaine 1) ✅
- [x] CRDs Challenge + ChallengeInstance
- [x] Controller basique
- [x] Flag generation
- [x] API Gateway
- [x] Deployment + Service creation

### Phase 2 (Semaine 2)
- [ ] Instance pooling
- [ ] Janitor controller (cleanup auto)
- [ ] Renewal mechanism
- [ ] Metrics Prometheus

### Phase 3 (Semaine 3)
- [ ] Cilium Network Policies
- [ ] AttackBox CRD
- [ ] Multi-container challenges
- [ ] Ingress support

### Phase 4 (Semaine 4)
- [ ] HA operator
- [ ] Webhooks validation
- [ ] E2E tests
- [ ] Production hardening

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

**Status:** MVP en développement  
**Deadline:** Lundi 20 janvier 2025  
**Maintainer:** @leo
