# CTF Challenge Operator - Specification

## Executive Summary

This document specifies a **Kubernetes-native operator** to replace `chall-manager`, providing dynamic CTF challenge infrastructure with better cloud-native patterns, without the Pulumi timeout issues.

### Current Problem
`chall-manager` uses Pulumi embedded in a Docker image to deploy challenge instances. This causes:
- **10+ minute timeouts** waiting for Kubernetes Service endpoints
- Complex workarounds needed
- Not truly cloud-native (IaC engine in a container)
- Heavy Docker images with Pulumi runtime

### Proposed Solution
A **Kubernetes Operator** with Custom Resource Definitions (CRDs) that:
- Manages challenge lifecycle natively in Kubernetes
- Uses CRDs instead of Pulumi for infrastructure definition
- Automatically creates Network Policies (Cilium CNP)
- Provides the **same API as chall-manager** for CTFd compatibility
- Enables GitOps workflows (challenges as code)

---

## Current chall-manager Architecture

### What chall-manager Does

**Core Features:**
1. **Dynamic Challenge Deployment**: Creates isolated instances per user/team
2. **Flag Generation**: Generates unique flags per instance to prevent flag sharing
3. **Connection Info**: Exposes connection details (IP, port, credentials, etc.)
4. **Lifecycle Management**: Timeout-based cleanup (janitor), renewal mechanisms
5. **Instance Pooling**: Pre-provisions instances for fast claiming
6. **Environment Injection**: Injects dynamic variables (flags, user IDs) into containers
7. **OCI Scenarios**: Loads challenge definitions from OCI registries
8. **CTFd Integration**: REST/gRPC API compatible with CTFd plugin

### Current Workflow

```
CTFd (user requests challenge)
    ↓ HTTP POST /api/v1/instance
chall-manager API
    ↓ Loads scenario from OCI registry
Pulumi stack.Up()
    ↓ Creates Deployment, Service, Ingress, etc.
Kubernetes Resources
    ↓ Returns connection_info + flag
CTFd (shows info to user)
```

### API Contract (Must Preserve for CTFd)

**Challenge Management:**
- `POST /api/v1/challenge` - Register a challenge
- `GET /api/v1/challenge/{id}` - Retrieve challenge info
- `PATCH /api/v1/challenge/{id}` - Update challenge
- `DELETE /api/v1/challenge/{id}` - Delete challenge

**Instance Management:**
- `POST /api/v1/instance` - Create instance for user
- `GET /api/v1/instance/{challenge_id}/{source_id}` - Get instance info
- `PATCH /api/v1/instance/{challenge_id}/{source_id}` - Renew instance
- `DELETE /api/v1/instance/{challenge_id}/{source_id}` - Delete instance

**Request/Response Format:**
```json
{
  "challenge_id": "operation-blackout",
  "source_id": "user-123",
  "additional": {
    "team": "myteam"
  }
}
```

**Response:**
```json
{
  "challenge_id": "operation-blackout",
  "source_id": "user-123",
  "connection_info": "nc ctf.dev.local 31337",
  "flags": ["FLAG{unique-per-instance}"],
  "since": "2025-01-17T12:00:00Z",
  "until": "2025-01-17T12:10:00Z"
}
```

### Challenge Definition (from CTFd)

Example challenge configuration sent from CTFd:
```json
{
  "name": "operation blackout",
  "category": "pwn",
  "type": "dynamic_iac",
  "value": 1000,
  "state": "visible",
  "initial": 1,
  "decay": 1,
  "minimum": 2,
  "function": "linear",
  "mana_cost": 2,
  "timeout": 600,
  "shared": false,
  "destroy_on_flag": true,
  "scenario": "registry.ctfd.svc.cluster.local:5000/ctf-operation-blackout:latest",
  "logic": "any",
  "max_attempts": 0
}
```

### Current Scenario Structure (Pulumi)

Scenarios are OCI images containing:
- `main.go` - Pulumi program (IaC)
- `go.mod`, `go.sum` - Dependencies
- Metadata defining outputs (`connection_info`, `flags`)

**Problems:**
1. Requires Go + Pulumi runtime in container
2. Pulumi waits for Service endpoints (10+ min timeout)
3. Heavy images (~500MB+)
4. Complex to debug
5. Not declarative (imperative Go code)

---

## Proposed Operator Architecture

### High-Level Design

```
CTFd
    ↓ Same API endpoint
Operator API Gateway (maintains chall-manager API compatibility)
    ↓ Creates CRDs
Kubernetes Operator (watches CRDs)
    ↓ Reconciles
Challenge Instances (Deployments, Services, Ingress, CNP)
```

### Custom Resource Definitions (CRDs)

#### 1. Challenge CRD

Defines a challenge template that can be instantiated multiple times.

```yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: operation-blackout
  namespace: ctf-challenges
spec:
  # Challenge metadata
  id: "1"
  category: "pwn"
  difficulty: "hard"
  
  # Scenario definition (replaces Pulumi)
  scenario:
    # Container template
    containers:
    - name: vulnerable-app
      image: registry.ctfd.svc.cluster.local:5000/ctf-operation-blackout:latest
      ports:
      - name: http
        containerPort: 8080
        expose:
          type: NodePort  # or LoadBalancer, or Ingress
      env:
      - name: FLAG
        valueFrom:
          flagRef:
            template: "FLAG{{{.InstanceID}}_{{.UserID}}_{{randomString 16}}}"
      - name: USER_ID
        valueFrom:
          instanceField: "spec.sourceId"
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          cpu: "500m"
          memory: "512Mi"
    
    # Optional: Additional services (database, etc.)
    additionalServices:
    - name: postgres
      image: postgres:15
      env:
      - name: POSTGRES_PASSWORD
        valueFrom:
          secretKeyRef:
            name: db-password
            key: password
  
  # Network policies
  network:
    isolation: strict  # or relaxed, none
    allowedEgress:
    - to:
      - podSelector:
          matchLabels:
            app: postgres
    allowedIngress:
    - from:
      - namespaceSelector:
          matchLabels:
            type: attackbox
    
  # Lifecycle settings
  lifecycle:
    timeout: 600  # seconds
    renewalEnabled: true
    destroyOnFlag: true
    janitor:
      enabled: true
      checkInterval: 60
  
  # Instance pooling
  pool:
    min: 1  # Pre-provision 1 instance
    max: 5  # Max 5 instances
    strategy: "hot"  # hot (always ready) or cold (create on demand)
  
  # Connection info template
  connectionInfo:
    template: "nc {{.NodeIP}} {{.NodePort}}"
    # or for HTTP: "http://{{.IngressHost}}/{{.InstanceID}}"

status:
  activeInstances: 3
  pooledInstances: 1
  conditions:
  - type: Ready
    status: "True"
```

#### 2. ChallengeInstance CRD

Represents a running instance for a specific user/team.

```yaml
apiVersion: ctf.io/v1alpha1
kind: ChallengeInstance
metadata:
  name: operation-blackout-user-123
  namespace: ctf-instances
  labels:
    challenge: operation-blackout
    sourceId: user-123
spec:
  challengeRef:
    name: operation-blackout
    namespace: ctf-challenges
  
  sourceId: "user-123"  # User or team ID
  challengeId: "1"
  
  # Additional configuration from CTFd
  additional:
    team: "myteam"
    ctfd_user_id: "42"
  
  # Lifecycle
  since: "2025-01-17T12:00:00Z"
  until: "2025-01-17T12:10:00Z"
  renewals: 2

status:
  phase: Running  # Pending, Running, Succeeded, Failed
  connectionInfo: "nc ctf.dev.local 31337"
  flags:
  - "FLAG{abc123_user123_randomhash}"
  
  # Resource references
  deployment: operation-blackout-user-123
  service: operation-blackout-user-123-svc
  ingress: operation-blackout-user-123-ing
  networkPolicy: operation-blackout-user-123-cnp
  
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2025-01-17T12:00:30Z"
  - type: FlagCaptured
    status: "False"
```

#### 3. AttackBox CRD (Optional)

Provides isolation and attack environments for users.

```yaml
apiVersion: ctf.io/v1alpha1
kind: AttackBox
metadata:
  name: kali-user-123
  namespace: ctf-attackboxes
spec:
  sourceId: "user-123"
  
  # AttackBox configuration
  image: kalilinux/kali-rolling:latest
  resources:
    cpu: "2"
    memory: "4Gi"
  
  # VNC/SSH access
  access:
    vnc:
      enabled: true
      password: "generated"
    ssh:
      enabled: true
      authorizedKeys:
      - "ssh-rsa AAAAB3..."
  
  # Network connectivity to challenges
  allowedChallenges:
  - operation-blackout
  - web-exploit
  
  persistence:
    enabled: true
    size: "10Gi"

status:
  phase: Running
  connectionInfo:
    vnc: "vnc://ctf.dev.local:5900"
    ssh: "ssh user@ctf.dev.local -p 2222"
```

---

## Operator Components

### 1. API Gateway Service

**Purpose:** Maintains compatibility with chall-manager API for CTFd.

**Responsibilities:**
- Expose REST/gRPC endpoints identical to chall-manager
- Translate API calls to CRD operations
- Query CRD status and return responses
- Handle authentication/authorization

**Implementation:**
- Go service with Gin/Chi or grpc-gateway
- Watches CRDs to populate responses
- Creates/updates/deletes Challenge and ChallengeInstance CRDs

### 2. Challenge Controller

**Purpose:** Reconcile Challenge CRDs.

**Responsibilities:**
- Validate challenge specifications
- Manage instance pool (create pre-provisioned instances)
- Update status with active instance count
- Handle challenge updates (rolling updates, blue/green)

**Reconciliation Logic:**
```go
func (r *ChallengeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    challenge := &ctfv1alpha1.Challenge{}
    r.Get(ctx, req.NamespacedName, challenge)
    
    // 1. Ensure pool has min instances
    pooledCount := countPooledInstances(challenge)
    if pooledCount < challenge.Spec.Pool.Min {
        createPooledInstance(challenge)
    }
    
    // 2. Cleanup expired instances
    cleanupExpiredInstances(challenge)
    
    // 3. Update status
    updateChallengeStatus(challenge)
    
    return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}
```

### 3. Instance Controller

**Purpose:** Reconcile ChallengeInstance CRDs.

**Responsibilities:**
- Create Kubernetes resources (Deployment, Service, Ingress)
- Generate flags and inject into containers
- Create Cilium Network Policies
- Monitor instance health
- Handle timeout/expiry
- Update status with connection info

**Reconciliation Logic:**
```go
func (r *InstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    instance := &ctfv1alpha1.ChallengeInstance{}
    r.Get(ctx, req.NamespacedName, instance)
    
    // 1. Fetch Challenge template
    challenge := &ctfv1alpha1.Challenge{}
    r.Get(ctx, instance.Spec.ChallengeRef, challenge)
    
    // 2. Create/Update Deployment
    deployment := buildDeployment(instance, challenge)
    controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
        // Inject flags, env vars
        injectFlags(deployment, instance)
        return nil
    })
    
    // 3. Create/Update Service
    service := buildService(instance, challenge)
    controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
        return nil
    })
    
    // 4. Create/Update NetworkPolicy (Cilium CNP)
    cnp := buildCiliumNetworkPolicy(instance, challenge)
    controllerutil.CreateOrUpdate(ctx, r.Client, cnp, func() error {
        return nil
    })
    
    // 5. Check expiry
    if time.Now().After(instance.Spec.Until) {
        r.Delete(ctx, instance)
        return ctrl.Result{}, nil
    }
    
    // 6. Update status with connection info
    updateInstanceStatus(instance, service)
    
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

### 4. Janitor Controller

**Purpose:** Cleanup expired instances.

**Responsibilities:**
- Watch ChallengeInstance CRDs for expiry
- Delete instances past `spec.until`
- Clean up orphaned resources

### 5. Flag Generator

**Purpose:** Generate unique flags per instance.

**Features:**
- Template-based generation: `FLAG{{{.InstanceID}}_{{randomString 32}}}`
- Cryptographically secure random generation
- Support for multiple flags per instance
- Flag rotation on renewal (optional)

---

## Key Improvements Over chall-manager

### 1. Cloud-Native Architecture
- **No Pulumi runtime needed** - Pure Kubernetes resources
- **Declarative** - CRDs are YAML, easy to version control
- **GitOps ready** - Flux/ArgoCD can sync challenges
- **Native reconciliation** - Kubernetes controller pattern

### 2. No Timeout Issues
- **Direct resource creation** - No waiting for Pulumi
- **Fast instance creation** - < 10 seconds typical
- **Predictable behavior** - Standard Kubernetes object lifecycle

### 3. Better Network Isolation
- **Automatic CNP creation** - Cilium Network Policies per instance
- **Fine-grained control** - Egress/ingress rules per challenge
- **AttackBox integration** - Isolated environments with controlled access

### 4. Enhanced Observability
- **Kubernetes events** - Standard event stream
- **Metrics** - Prometheus metrics on instances, pools
- **Status conditions** - Clear Ready/Failed states
- **Easier debugging** - `kubectl describe` shows everything

### 5. Lighter Weight
- **Smaller images** - No Pulumi/Go runtime required
- **Faster builds** - Just challenge container image
- **Easier development** - Standard Kubernetes patterns

---

## Migration Path from chall-manager

### Phase 1: API Gateway
1. Deploy operator with API gateway
2. API gateway mimics chall-manager API
3. CTFd continues working without changes

### Phase 2: Challenge Migration
1. Convert existing Pulumi scenarios to Challenge CRDs
2. Migration tool: `pulumi-to-crd`
3. Test challenges in parallel

### Phase 3: Full Cutover
1. Point CTFd to new operator
2. Decommission chall-manager
3. Monitor for issues

---

## Implementation Checklist

### Operator Core
- [ ] Scaffold operator with Kubebuilder/Operator SDK
- [ ] Define CRD schemas (Challenge, ChallengeInstance, AttackBox)
- [ ] Implement Challenge controller
- [ ] Implement ChallengeInstance controller
- [ ] Implement Janitor controller
- [ ] Flag generator library

### API Gateway
- [ ] REST API server (Gin/Chi)
- [ ] gRPC server (optional, for compatibility)
- [ ] CRD translation layer
- [ ] Authentication/authorization middleware
- [ ] OpenAPI spec generation

### Resource Management
- [ ] Deployment builder
- [ ] Service builder (NodePort/LoadBalancer/Ingress)
- [ ] Cilium CNP builder
- [ ] ConfigMap/Secret injection
- [ ] Resource cleanup logic

### Features
- [ ] Instance pooling logic
- [ ] Flag generation and injection
- [ ] Connection info templating
- [ ] Timeout/renewal handling
- [ ] Metrics exporter (Prometheus)
- [ ] Event logging

### Testing
- [ ] Unit tests for controllers
- [ ] Integration tests with envtest
- [ ] E2E tests with real Kubernetes cluster
- [ ] CTFd integration tests

### Documentation
- [ ] Operator deployment guide
- [ ] Challenge CRD reference
- [ ] Migration guide from chall-manager
- [ ] Examples gallery

---

## Example Challenge Scenarios

### Simple Web Challenge
```yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: simple-web
spec:
  scenario:
    containers:
    - name: web
      image: nginx:alpine
      ports:
      - containerPort: 80
        expose:
          type: Ingress
          ingressClass: nginx
          host: "{{.InstanceID}}.ctf.dev.local"
  connectionInfo:
    template: "http://{{.IngressHost}}"
```

### Multi-Container with Database
```yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: sql-injection
spec:
  scenario:
    containers:
    - name: app
      image: vulnerable-webapp:latest
      ports:
      - containerPort: 3000
        expose:
          type: NodePort
      env:
      - name: DB_HOST
        value: "localhost"
      - name: FLAG
        valueFrom:
          flagRef:
            template: "FLAG{{{randomString 32}}}"
    
    - name: database
      image: mysql:8.0
      env:
      - name: MYSQL_ROOT_PASSWORD
        valueFrom:
          secretKeyRef:
            name: mysql-password
            key: password
```

### Pwn Challenge with AttackBox
```yaml
apiVersion: ctf.io/v1alpha1
kind: Challenge
metadata:
  name: buffer-overflow
spec:
  scenario:
    containers:
    - name: vulnerable-binary
      image: pwn-challenge:latest
      ports:
      - containerPort: 9999
        expose:
          type: NodePort
  
  attackBox:
    required: true
    template: kali-linux
  
  network:
    isolation: strict
    allowedIngress:
    - from:
      - namespaceSelector:
          matchLabels:
            type: attackbox
```

---

## Technical Specifications

### Language & Framework
- **Language:** Go 1.21+
- **Framework:** Kubebuilder v3 or Operator SDK
- **API:** Gin/Chi for REST, grpc-gateway for gRPC

### Dependencies
- `controller-runtime` - Kubernetes controller framework
- `client-go` - Kubernetes API client
- `cilium/cilium` - Network policy CRDs
- `gin-gonic/gin` or `go-chi/chi` - REST API
- `prometheus/client_golang` - Metrics

### Resource Requirements
- **Operator Pod:** 256Mi memory, 100m CPU
- **API Gateway Pod:** 512Mi memory, 200m CPU

### RBAC Requirements
```yaml
- apiGroups: ["ctf.io"]
  resources: ["challenges", "challengeinstances", "attackboxes"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["services", "configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses", "networkpolicies"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["cilium.io"]
  resources: ["ciliumnetworkpolicies"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

---

## Security Considerations

### Isolation
1. **Namespace isolation** - Each instance in dedicated namespace (optional)
2. **Network policies** - Cilium CNP enforces segmentation
3. **Resource quotas** - Prevent resource exhaustion
4. **Pod Security Standards** - Restricted profile

### Secrets Management
1. **Flag storage** - In-memory only, never persisted
2. **Credentials** - Sealed Secrets or External Secrets Operator
3. **RBAC** - Minimal permissions for operator

### DoS Protection
1. **Rate limiting** - API gateway enforces limits
2. **Instance limits** - Max instances per user
3. **Resource limits** - CPU/memory constraints

---

## Monitoring & Observability

### Metrics (Prometheus)
```
ctf_challenges_total
ctf_challenge_instances_active
ctf_challenge_instances_pooled
ctf_challenge_instance_creation_duration_seconds
ctf_flags_generated_total
ctf_api_requests_total
```

### Events
- `ChallengeCreated`
- `InstanceCreated`
- `InstanceExpired`
- `FlagCaptured`
- `PoolRefilled`

### Logs
- Structured logging (JSON)
- Log levels: DEBUG, INFO, WARN, ERROR
- Correlation IDs for tracing

---

## API Compatibility Matrix

| chall-manager Endpoint | Operator Endpoint | Status |
|------------------------|-------------------|--------|
| `POST /api/v1/challenge` | `POST /api/v1/challenge` | ✅ Compatible |
| `GET /api/v1/challenge/{id}` | `GET /api/v1/challenge/{id}` | ✅ Compatible |
| `PATCH /api/v1/challenge/{id}` | `PATCH /api/v1/challenge/{id}` | ✅ Compatible |
| `DELETE /api/v1/challenge/{id}` | `DELETE /api/v1/challenge/{id}` | ✅ Compatible |
| `POST /api/v1/instance` | `POST /api/v1/instance` | ✅ Compatible |
| `GET /api/v1/instance/{cid}/{sid}` | `GET /api/v1/instance/{cid}/{sid}` | ✅ Compatible |
| `PATCH /api/v1/instance/{cid}/{sid}` | `PATCH /api/v1/instance/{cid}/{sid}` | ✅ Compatible |
| `DELETE /api/v1/instance/{cid}/{sid}` | `DELETE /api/v1/instance/{cid}/{sid}` | ✅ Compatible |

---

## Roadmap

### v0.1.0 (MVP)
- Basic Challenge and ChallengeInstance CRDs
- Simple deployment creation
- API gateway with core endpoints
- Flag generation

### v0.2.0
- Instance pooling
- Janitor with timeout enforcement
- Cilium Network Policies
- Metrics

### v0.3.0
- AttackBox CRD
- Multi-container challenges
- Connection info templating

### v1.0.0 (Production Ready)
- Full CTFd integration
- Blue/green updates
- Advanced network policies
- HA operator deployment

---

## Conclusion

This operator will provide a **truly cloud-native** solution for dynamic CTF challenges, eliminating the Pulumi timeout issues while maintaining full compatibility with CTFd. The CRD-based approach enables GitOps workflows, better observability, and easier maintenance.

**Next Steps:**
1. Bootstrap operator with Kubebuilder
2. Define CRD schemas
3. Implement core controllers
4. Build API gateway
5. Test with existing challenges
6. Deploy alongside chall-manager for validation
7. Full migration

**Estimated Development Time:** 4-6 weeks for MVP, 8-12 weeks for production-ready v1.0.0.
