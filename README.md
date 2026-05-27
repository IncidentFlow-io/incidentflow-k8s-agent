# incidentflow-k8s-agent

`incidentflow-k8s-agent` is a lightweight outbound-only Kubernetes cluster agent for IncidentFlow. It runs inside a customer cluster, authenticates with the IncidentFlow platform, opens a WebSocket to the Agent Gateway, and executes a small set of read-only Kubernetes inspection commands.

## Architecture

```text
MCP Client
  -> IncidentFlow MCP Server
  -> IncidentFlow Platform API
  -> IncidentFlow Agent Gateway
  -> incidentflow-k8s-agent
  -> Kubernetes API
```

The agent never exposes a public HTTP endpoint and never requires inbound traffic. It uses Kubernetes in-cluster authentication through `rest.InClusterConfig()`.

## Commands

Supported actions:

- `k8s.list_namespaces`
- `k8s.list_pods`
- `k8s.get_pod`
- `k8s.get_pod_logs`
- `k8s.list_events`
- `k8s.list_deployments`
- `k8s.list_services`
- `k8s.get_rollout_status`

Example command:

```json
{
  "id": "req_123",
  "type": "command",
  "action": "k8s.get_pod_logs",
  "params": {
    "namespace": "production",
    "pod": "checkout-api-7c9d6f",
    "tail_lines": 200
  }
}
```

Example response:

```json
{
  "id": "req_123",
  "type": "response",
  "status": "success",
  "data": {
    "logs": "...",
    "truncated": false
  }
}
```

## Configuration

Environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `INCIDENTFLOW_PLATFORM_URL` | yes | Platform API base URL. |
| `INCIDENTFLOW_GATEWAY_URL` | yes | WebSocket gateway URL. |
| `INCIDENTFLOW_REGISTRATION_TOKEN` | if no agent token | One-time registration token. |
| `INCIDENTFLOW_AGENT_TOKEN` | if already registered | Persistent agent token. |
| `INCIDENTFLOW_CLUSTER_NAME` | no | Human-readable cluster name. |
| `INCIDENTFLOW_LOG_LEVEL` | no | Zap log level, defaults to `info`. |
| `INCIDENTFLOW_NAMESPACE_ALLOWLIST` | no | Comma-separated namespace allowlist. |

If `INCIDENTFLOW_AGENT_TOKEN` is absent, the agent calls:

```text
POST {INCIDENTFLOW_PLATFORM_URL}/api/v1/agents/register
Authorization: Bearer {INCIDENTFLOW_REGISTRATION_TOKEN}
```

and stores the returned token in `/var/lib/incidentflow/agent-token`. In production, prefer passing `agentToken` through the Helm Secret after registration.

## Local Development

Build and test:

```sh
make build
make test
```

Run:

```sh
make run
```

Real runtime requires Kubernetes in-cluster service account credentials. Running locally without a pod-mounted service account will fail while loading `rest.InClusterConfig()`.

## Kubernetes Deployment

Install with Helm:

```sh
helm install incidentflow-k8s-agent ./deploy/helm \
  --namespace incidentflow-agent \
  --create-namespace \
  --set clusterName=prod-us-east \
  --set registrationToken=...
```

## Kind Smoke Test

Run a local end-to-end deployment into `kind`:

```sh
make kind-smoke-test
```

The script creates or reuses a `kind` cluster named `incidentflow-agent`, builds the Docker image, loads it into the cluster, installs the Helm chart, and checks pod status, logs, and RBAC.

With the default fake gateway URL, the agent is expected to run and log reconnect attempts.

Override values when needed:

```sh
KIND_CLUSTER_NAME=incidentflow-agent \
INCIDENTFLOW_PLATFORM_URL=https://api.example.com \
INCIDENTFLOW_GATEWAY_URL=wss://gateway.example.com/agents/ws \
INCIDENTFLOW_AGENT_TOKEN=test-token \
make kind-smoke-test
```

The chart creates:

- Deployment
- ServiceAccount
- read-only ClusterRole
- ClusterRoleBinding
- Secret
- ConfigMap

It intentionally creates no Service object because the agent is outbound-only.

## Security Model

- Read-only Kubernetes RBAC only.
- No permissions for Secrets.
- No exec, apply, delete, patch, or mutation commands.
- Dangerous namespaces are always denied: `kube-system`, `kube-public`, `kube-node-lease`.
- Optional namespace allowlist narrows command scope.
- Pod log output is capped by `INCIDENTFLOW_MAX_TAIL_LINES` and `INCIDENTFLOW_MAX_LOG_BYTES`.
- WebSocket authentication uses the persistent agent token.
