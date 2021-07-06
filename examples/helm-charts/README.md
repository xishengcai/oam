# helm-charts

This is example about how to use helm-charts create component

below crd is the helm-operator wanted
```yaml
apiVersion: helm.fluxcd.io/v1
kind: HelmRelease
metadata:
  name: nginx
  namespace: default
spec:
  chart:
    repository: "https://charts.bitnami.com/bitnami"
    name: "nginx"
    version: "8.9.1"
```

