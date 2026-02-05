# MaaS Controller

Control plane for the Models-as-a-Service (MaaS) subscription model. It reconciles **MaaSModel**, **MaaSAuthPolicy**, and **MaaSSubscription** custom resources and creates the corresponding Kuadrant AuthPolicies and TokenRateLimitPolicies, plus HTTPRoutes where needed.

## Prerequisites

- OpenShift or Kubernetes with **Gateway API** and **Kuadrant** installed
- **Open Data Hub** operator (for the `opendatahub` namespace and optional integration)
- `kubectl` or `oc`

## Authentication (inference flow)

Until API token minting is in place, the controller and inference flow **temporarily bypass MaaS API token minting**. You can call inference using your OpenShift token directly:

```bash
export TOKEN=$(oc whoami -t)
curl -H "Authorization: Bearer $TOKEN" "https://<gateway-host>/v1/models/<model-name>/infer" -d '...'
```

Use `oc whoami -t` to obtain a token for the current user. The Kuadrant AuthPolicy (created from MaaSAuthPolicy) validates the OpenShift token via Kubernetes TokenReview.

## Objects created by the controller

When you install the controller and create MaaS CRs, the following cluster objects are created or updated.

### Custom Resource Definitions (cluster-scoped)

| CRD | Group | Description |
|-----|--------|-------------|
| MaaSModel | maas.opendatahub.io | Registers a model (LLMInferenceService or external) with MaaS and exposes it via HTTPRoute. |
| MaaSAuthPolicy | maas.opendatahub.io | Defines who can access which models; controller creates one Kuadrant AuthPolicy per model. |
| MaaSSubscription | maas.opendatahub.io | Defines token rate limits per model and owner; controller creates one Kuadrant TokenRateLimitPolicy per model. |

### RBAC (cluster and namespace)

| Object | Scope | Description |
|--------|--------|-------------|
| ClusterRole `maas-controller-role` | Cluster | Permissions for MaaS CRs, Gateway API (HTTPRoute, Gateway), Kuadrant (AuthPolicy, TokenRateLimitPolicy), KServe LLMInferenceService. |
| ClusterRoleBinding `maas-controller-rolebinding` | Cluster | Binds the controller ServiceAccount to the ClusterRole. |
| Role `maas-controller-leader-election-role` | opendatahub | ConfigMaps and Leases for leader election. |
| RoleBinding `maas-controller-leader-election-rolebinding` | opendatahub | Binds ServiceAccount to leader-election Role. |
| ServiceAccount `maas-controller` | opendatahub | Identity for the controller pod. |

### Workload

| Object | Namespace | Description |
|--------|-----------|-------------|
| Deployment `maas-controller` | opendatahub | Single-replica controller; image `quay.io/maas/maas-controller:latest` (temporary – see Build and push image). |

### Objects created by the controller at runtime

- **HTTPRoute** (Gateway API): For each MaaSModel (e.g. ExternalModel), the controller creates an HTTPRoute. For LLMInferenceService it only validates the route created by KServe.
- **AuthPolicy** (Kuadrant): One per model referenced in a MaaSAuthPolicy; targets that model’s HTTPRoute.
- **TokenRateLimitPolicy** (Kuadrant): One per model referenced in a MaaSSubscription; targets that model’s HTTPRoute.

These runtime objects are labeled with `app.kubernetes.io/managed-by: maas-controller`.

## Install (default: opendatahub namespace)

1. Ensure the `opendatahub` namespace exists (e.g. created by the Open Data Hub operator). The install script will validate it exists and will not create it.

2. **Disable the shared gateway-auth-policy** before using MaaSAuthPolicy (so the controller can manage auth per HTTPRoute). The policy cannot be removed because another operator may own it; annotate it and point it at a non-existing gateway:

   ```bash
   ./hack/disable-gateway-auth-policy.sh
   ```

   This annotates the `gateway-auth-policy` AuthPolicy in `openshift-ingress` with `opendatahub.io/managed: "false"` and sets its `targetRef.name` to a non-existing gateway so it no longer applies.

3. Install the controller:

   ```bash
   ./scripts/install-maas-controller.sh
   ```

   To install into another namespace:

   ```bash
   ./scripts/install-maas-controller.sh my-namespace
   ```

## Build and push image

The default deployment uses `quay.io/maas/maas-controller:latest`. **For now you can use that image, but it is temporary**; plan to build and push to your own registry when ready.

Build with **podman**, **buildah**, or **docker** (auto-detected):

```bash
make image-build
# or with custom image/tag
make image-build IMAGE=quay.io/myorg/maas-controller IMAGE_TAG=v0.1.0
```

Push (default is `quay.io/maas/maas-controller`, or override `IMAGE` / `IMAGE_TAG`):

```bash
podman login quay.io   # or docker login quay.io
make image-push        # builds and pushes IMAGE:IMAGE_TAG (default quay.io/maas/maas-controller:latest)
make image-push IMAGE=quay.io/myorg/maas-controller IMAGE_TAG=v0.1.0
```

## Deploy to OpenShift

1. **Log in to the cluster**: `oc login ...` (or set `KUBECONFIG`).

2. **Disable the shared gateway-auth-policy** (see Install step 2 above): `./hack/disable-gateway-auth-policy.sh`

3. **Install the controller**: `./scripts/install-maas-controller.sh`

4. **Verify**:
   ```bash
   kubectl get pods -n opendatahub -l app=maas-controller
   kubectl get crd | grep maas.opendatahub.io
   ```

## Examples

Example MaaS CRs and a script to install them (plus sample LLMInferenceServices from the docs) are in `examples/`. See [examples/README.md](examples/README.md) and `./scripts/install-examples.sh`.

## Development

```bash
make build    # build binary to bin/manager
make run      # run locally (uses kubeconfig)
make test     # run tests
make install  # apply config/default to cluster
make uninstall
```

## Configuration

- **Controller namespace**: Default is `opendatahub`. Override by passing a namespace to `install-maas-controller.sh` or by editing `config/manager/manager.yaml` and RBAC in `config/rbac/`.
- **Image**: Default is `quay.io/maas/maas-controller:latest`. Override in the deployment or via Kustomize.
