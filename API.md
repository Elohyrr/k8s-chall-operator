# API Gateway Documentation

L'API Gateway expose une API REST pour gérer les challenges et instances CTF.

## 📚 Documentation Interactive (Swagger UI)

Une documentation interactive complète est disponible via Swagger UI :

**URL locale** : http://localhost:8080/swagger/index.html

**URL dans le cluster** : 
```bash
kubectl port-forward -n chall-operator-system svc/api-gateway 8080:8080
```
Puis ouvrir : http://localhost:8080/swagger/index.html

## 📄 OpenAPI Specification

Le fichier OpenAPI (Swagger) est disponible à :
- **Fichier statique** : [`openapi.json`](./openapi.json) (à la racine du repo)
- **Endpoint live** : http://localhost:8080/swagger/doc.json

Vous pouvez importer ce fichier dans n'importe quel outil compatible OpenAPI (Postman, Insomnia, etc.)

## 🔄 Génération de la Documentation

Pour régénérer la documentation après avoir modifié l'API :

```bash
make swagger
```

Ceci va :
1. Installer `swag` si nécessaire
2. Parser les annotations dans le code Go
3. Générer `docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go`
4. Copier `openapi.json` à la racine

## 📝 Ajouter des Annotations

Pour documenter un nouvel endpoint, ajoutez des annotations Swagger au-dessus de la fonction handler :

```go
// CreateInstance godoc
// @Summary Create a new challenge instance
// @Description Create a new ChallengeInstance for a user/team
// @Tags instances
// @Accept json
// @Produce json
// @Param body body CreateInstanceRequest true "Instance creation request"
// @Success 201 {object} InstanceResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /instance [post]
func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
    // ...
}
```

Puis exécutez `make swagger` pour régénérer la documentation.

## 🚀 Endpoints Principaux

### Challenge Management

- `POST /api/v1/challenge` - Créer un challenge
- `GET /api/v1/challenge` - Lister les challenges
- `GET /api/v1/challenge/{challengeId}` - Obtenir un challenge
- `PATCH /api/v1/challenge/{challengeId}` - Modifier un challenge
- `DELETE /api/v1/challenge/{challengeId}` - Supprimer un challenge

### Instance Management

- `POST /api/v1/instance` - Créer une instance
- `GET /api/v1/instance` - Lister les instances (avec filtre `?source_id=`)
- `GET /api/v1/instance/{challengeId}/{sourceId}` - Obtenir une instance
- `DELETE /api/v1/instance/{challengeId}/{sourceId}` - Supprimer une instance
- `POST /api/v1/instance/{challengeId}/{sourceId}/validate` - Valider un flag
- `POST /api/v1/instance/{challengeId}/{sourceId}/renew` - Renouveler une instance

### Health & Monitoring

- `GET /health` - Health check
- `GET /healthz` - Health check (alias)
- `GET /healthcheck` - Health check (alias)

## 🔐 Authentification

L'API est protégée par OAuth2 via oauth2-proxy en production. En développement local, vous pouvez accéder directement aux endpoints.

## 📊 Exemples de Requêtes

Voir la documentation Swagger interactive pour des exemples complets avec corps de requête et réponses.

### Créer une Instance

```bash
curl -X POST http://localhost:8080/api/v1/instance \
  -H "Content-Type: application/json" \
  -d '{
    "challenge_id": "101",
    "source_id": "user@example.com"
  }'
```

### Lister les Instances d'un User

```bash
curl "http://localhost:8080/api/v1/instance?source_id=user@example.com"
```

### Obtenir une Instance

```bash
curl http://localhost:8080/api/v1/instance/101/user@example.com
```

## 🐛 Debug & Logs

Pour voir les logs de l'API Gateway :

```bash
kubectl logs -n chall-operator-system deployment/api-gateway -f
```

## 🔗 Ressources

- [Swagger/OpenAPI Specification](https://swagger.io/specification/)
- [swaggo/swag Documentation](https://github.com/swaggo/swag)
- [Swagger UI](https://swagger.io/tools/swagger-ui/)
