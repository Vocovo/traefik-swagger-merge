# traefik-swagger-merge

A Middleware plugin for Traefik allow merge multiply swagger doc endpoints to a single one.
Perhaps you'll find it usable for multiply microservices which served to one traefik balancer.

This is currently used for merging Swagger docs on the LocalAPI and Customer Middleware services.

This is based on a fork of https://github.com/usalko/swagger-ring/tree/v0.1.9

## Use case

docs.yaml

```yaml
http:
  routers:
    docs-router:
      rule: PathPrefix(`/api/v1/docs`)
      service: docs-service
      middlewares:
        - swagger

  services:
    docs-service:
      loadBalancer:
        servers:
          - url: http://whoami
  
  middlewares:
    swagger:
      plugin:
        swagger-merge:
          path: /api/v1/docs
          docs:
            - path: http://service1:3000/swagger.yaml
            - path: http://service2:3000/swagger.yaml
```

traefik.yaml

```yaml
experimental:
  localPlugins:
    swagger-merge:
      moduleName: "github.com/vocovo/traefik-swagger-merge"
```
