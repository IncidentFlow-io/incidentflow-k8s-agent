# incidentflow-k8s-agent

`incidentflow-k8s-agent` is a lightweight outbound-only Kubernetes cluster agent for IncidentFlow. It runs inside your cluster, authenticates with the IncidentFlow platform, opens a persistent WebSocket to the Agent Gateway, and executes a small set of read-only Kubernetes inspection commands on behalf of the platform.

## How it works

```text
MCP Client
  -> IncidentFlow MCP Server
  -> IncidentFlow Platform API
  -> IncidentFlow Agent Gateway
  -> incidentflow-k8s-agent  (inside your cluster)
  -> Kubernetes API
```

The agent never exposes a public endpoint and never requires inbound traffic. All communication is initiated outbound from the agent. Kubernetes access uses in-cluster service account credentials via `rest.InClusterConfig()`.

## Installation

### Prerequisites

- Kubernetes 1.24+
- Helm 3.10+
- A registration token from the IncidentFlow platform (`incidentflow cluster install` handles this automatically)

### Install via Helm OCI

```sh
helm install incidentflow-k8s-agent \
  oci://ghcr.io/incidentflow-io/charts/incidentflow-k8s-agent \
  --version 1.0.6 \
  --namespace incidentflow-agent \
  --create-namespace \
  --set agent.clusterName=prod-us-east \
  --set agent.registrationToken=<your-token>
```

### Install via IncidentFlow CLI

The recommended way. The CLI handles token issuance, helm diff preview, and confirmation before making any changes:

```sh
incidentflow cluster install --name prod-us-east
```

## Configuration

All values are documented in [`deploy/helm/README.md`](deploy/helm/README.md).

Key environment variables injected by the Helm chart:

| Variable | Required | Description |
|---|---|---|
| `INCIDENTFLOW_PLATFORM_URL` | yes | Platform API base URL |
| `INCIDENTFLOW_GATEWAY_URL` | yes | WebSocket gateway URL |
| `INCIDENTFLOW_REGISTRATION_TOKEN` | on first start | One-time registration token |
| `INCIDENTFLOW_AGENT_TOKEN` | after registration | Persistent agent token (stored in `/var/lib/incidentflow/agent-token`) |
| `INCIDENTFLOW_CLUSTER_NAME` | yes | Cluster identifier shown in the dashboard |
| `INCIDENTFLOW_LOG_LEVEL` | no | Log level — `debug`, `info`, `warn`, `error`. Defaults to `info` |
| `INCIDENTFLOW_NAMESPACE_ALLOWLIST` | no | Comma-separated namespace allowlist. Empty means all non-system namespaces |

## Token persistence

On first start the agent exchanges the registration token for a persistent agent token and stores it in `/var/lib/incidentflow/agent-token`.

By default `persistence.enabled=false` — the token store uses an `emptyDir` and the token is lost on pod restart. Enable a PVC for production clusters where you do not want to re-register after restarts:

```sh
helm upgrade incidentflow-k8s-agent ... --set persistence.enabled=true
```

## Supported commands

| Command | Description |
|---|---|
| `k8s.list_namespaces` | List all namespaces |
| `k8s.list_pods` | List pods in a namespace |
| `k8s.get_pod` | Get a single pod |
| `k8s.get_pod_logs` | Fetch pod logs |
| `k8s.list_events` | List events in a namespace |
| `k8s.list_deployments` | List deployments |
| `k8s.list_services` | List services |
| `k8s.get_rollout_status` | Get rollout status for a deployment |

## Security model

- Read-only Kubernetes RBAC — no write permissions of any kind.
- No access to Secrets.
- No exec, apply, delete, patch, or mutation commands.
- System namespaces are always blocked: `kube-system`, `kube-public`, `kube-node-lease`.
- Optional namespace allowlist further restricts scope.
- Log output is capped by `INCIDENTFLOW_MAX_TAIL_LINES` and `INCIDENTFLOW_MAX_LOG_BYTES`.
- WebSocket sessions are authenticated with the persistent agent token.

## Kubernetes resources created

| Resource | Name |
|---|---|
| Deployment | `incidentflow-k8s-agent` |
| ServiceAccount | `incidentflow-k8s-agent` |
| ClusterRole | `incidentflow-k8s-agent` |
| ClusterRoleBinding | `incidentflow-k8s-agent` |
| ConfigMap | `incidentflow-k8s-agent-config` |
| Secret | `incidentflow-agent-credentials` |
| PVC _(optional)_ | `incidentflow-k8s-agent-token-store` |

No Service object is created — the agent is outbound-only.

## Verifying release signatures

All release artifacts — Docker images and Helm charts — are signed with [cosign](https://github.com/sigstore/cosign) keyless signing via GitHub Actions OIDC. No long-lived private keys are used.

```sh
brew install cosign
```

**Verify Docker image:**

```sh
cosign verify \
  --certificate-identity-regexp="https://github.com/IncidentFlow-io/incidentflow-k8s-agent" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/incidentflow-io/incidentflow-k8s-agent:vX.Y.Z
```

**Verify Helm chart:**

```sh
cosign verify \
  --certificate-identity-regexp="https://github.com/IncidentFlow-io/incidentflow-k8s-agent" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  ghcr.io/incidentflow-io/charts/incidentflow-k8s-agent:X.Y.Z
```

A successful verification prints:

- `Subject` — the exact workflow and tag that produced the artifact
- `githubWorkflowRepository` — `IncidentFlow-io/incidentflow-k8s-agent`
- `githubWorkflowRef` — the git tag (e.g. `refs/tags/vX.Y.Z`)

All signatures are recorded in the [Sigstore Rekor transparency log](https://rekor.sigstore.dev).

## Local development

```sh
make build
make test
make run
```

Running outside a pod requires a valid kubeconfig. In-cluster service account credentials are not available outside Kubernetes.

## Helm chart development

### Regenerate values schema

After editing `values.yaml`, regenerate `values.schema.json` so Helm validates inputs on install:

```sh
cd deploy/helm
helm schema
```

Requires [helm-schema](https://github.com/dadav/helm-schema):

```sh
helm plugin install https://github.com/dadav/helm-schema --verify=false
```

### Regenerate values documentation

After editing `values.yaml` comments, regenerate `deploy/helm/README.md`:

```sh
cd deploy/helm
helm-docs
```

Requires [helm-docs](https://github.com/norwoodj/helm-docs):

```sh
brew install helm-docs
```
