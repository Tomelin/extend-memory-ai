# extended-memory Helm chart

This chart is the Helm equivalent of `deploy/kubernetes/base`. It installs the
API Deployment and Service, migration init container, ConfigMap, HPA, PDB,
purge CronJob and Gateway API HTTPRoute.

The default setup expects a Secret named `memory-api` in the release namespace,
with these keys:

- `MEMORY_DATABASE_URL`
- `MEMORY_ADMIN_DATABASE_URL`
- `MEMORY_MIGRATOR_DATABASE_URL`
- `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_HEADERS`

Install the chart in the `argocd` namespace used by the supplied HTTPRoute:

```shell
helm upgrade --install memory-api ./deploy/kubernetes/extended-memory \
  --namespace argocd \
  --create-namespace \
  --set image.tag=<image-tag>
```

For a Helm-managed Secret, store the values in a private values file and set
`secret.create=true`. Do not commit credentials to this repository.

The Gateway API CRDs and the referenced Gateways (`envoy-gateway/https` and
`envoy-gateway/http`) must already exist in the cluster.

To reproduce the attached route name and its explicit backend namespace, add
`--set httpRoute.nameOverride=argocd-https` and
`--set httpRoute.backendNamespace=argocd` to the install command.
