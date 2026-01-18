# ArgoCD Deployment Guide

Ce guide explique comment déployer l'operator avec ArgoCD pour différents environnements.

## Architecture

L'operator est maintenant variabilisé pour supporter différents environnements via Kustomize overlays :

- **Dev** : `devleo.local` (environnement local)
- **Prod** : `ctf.rokhnir.dev` (cloud)

## Variables configurables

Les variables suivantes sont configurables via ConfigMap :

| Variable | Description | Dev | Prod |
|----------|-------------|-----|------|
| `BASE_DOMAIN` | Domaine de base pour les ingress | `devleo.local` | `ctf.rokhnir.dev` |
| `AUTH_URL` | URL du service d'authentification OAuth2 | `auth.devleo.local` | `auth.ctf.rokhnir.dev` |
| `DEFAULT_HOST_TEMPLATE` | Template pour les hostnames d'instance | `ctf.{{.InstanceName}}.{{.Username}}.{{.ChallengeID}}.devleo.local` | `ctf.{{.InstanceName}}.{{.Username}}.{{.ChallengeID}}.ctf.rokhnir.dev` |

## Déploiement avec ArgoCD

### Prérequis

- ArgoCD installé dans le cluster
- Accès au repo GitHub : `https://github.com/Elohyrr/k8s-chall-operator.git`

### Déploiement Dev (devleo.local)

```bash
kubectl apply -f config/argocd/application-dev.yaml
```

### Déploiement Prod (ctf.rokhnir.dev)

```bash
kubectl apply -f config/argocd/application-prod.yaml
```

## Déploiement manuel avec Kustomize

Si vous ne voulez pas utiliser ArgoCD :

### Dev
```bash
kubectl apply -k config/overlays/dev
```

### Prod
```bash
kubectl apply -k config/overlays/prod
```

## Personnalisation

Pour créer un nouvel environnement, dupliquez `config/overlays/dev` et modifiez les valeurs dans le `configMapGenerator`.

### Exemple : staging

```bash
mkdir -p config/overlays/staging
cp config/overlays/dev/kustomization.yaml config/overlays/staging/
```

Puis modifiez `config/overlays/staging/kustomization.yaml` :

```yaml
configMapGenerator:
- name: chall-operator-config
  behavior: merge
  literals:
  - BASE_DOMAIN=ctf.staging.example.com
  - AUTH_URL=auth.ctf.staging.example.com
  - DEFAULT_HOST_TEMPLATE=ctf.{{.InstanceName}}.{{.Username}}.{{.ChallengeID}}.ctf.staging.example.com
```

## Vérification

Après déploiement, vérifiez :

```bash
# ConfigMap
kubectl get configmap -n chall-operator-system chall-operator-config -o yaml

# Pods
kubectl get pods -n chall-operator-system

# CRDs
kubectl get challenges,challengeinstances -A
```

## Structure des fichiers

```
config/
├── argocd/                    # Applications ArgoCD
│   ├── application-dev.yaml
│   ├── application-prod.yaml
│   └── README.md
├── overlays/                  # Overlays Kustomize par environnement
│   ├── dev/
│   │   └── kustomization.yaml
│   └── prod/
│       └── kustomization.yaml
├── default/                   # Configuration de base
│   └── kustomization.yaml
├── manager/                   # Controller deployment
│   ├── manager.yaml
│   └── manager-config.yaml
└── gateway/                   # API Gateway deployment
    ├── deployment.yaml
    └── kustomization.yaml
```
