#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

CLUSTER_NAME="${KIND_CLUSTER_NAME:-incidentflow-agent}"
NAMESPACE="${NAMESPACE:-incidentflow-agent}"
RELEASE_NAME="${RELEASE_NAME:-incidentflow-k8s-agent}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-incidentflow-k8s-agent}"
IMAGE_TAG="${IMAGE_TAG:-dev}"
PLATFORM_URL="${INCIDENTFLOW_PLATFORM_URL:-https://api.example.com}"
GATEWAY_URL="${INCIDENTFLOW_GATEWAY_URL:-wss://gateway.example.com/agents/ws}"
AGENT_TOKEN="${INCIDENTFLOW_AGENT_TOKEN:-kind-smoke-test-token}"
CLUSTER_CONFIG="${CLUSTER_CONFIG:-${ROOT_DIR}/deploy/kind/cluster.yaml}"

log() {
  printf '\n==> %s\n' "$*"
}

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'missing required command: %s\n' "$1" >&2
    exit 127
  fi
}

require docker
require kind
require kubectl
require helm

cd "${ROOT_DIR}"

log "Checking Docker daemon"
docker info >/dev/null

if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  log "Reusing kind cluster ${CLUSTER_NAME}"
else
  log "Creating kind cluster ${CLUSTER_NAME}"
  kind create cluster --config "${CLUSTER_CONFIG}" --name "${CLUSTER_NAME}"
fi

log "Selecting kubectl context"
kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null
kubectl cluster-info --context "kind-${CLUSTER_NAME}"

log "Building image ${IMAGE_REPOSITORY}:${IMAGE_TAG}"
docker build -t "${IMAGE_REPOSITORY}:${IMAGE_TAG}" .

log "Loading image into kind"
kind load docker-image "${IMAGE_REPOSITORY}:${IMAGE_TAG}" --name "${CLUSTER_NAME}"

log "Rendering Helm chart"
helm template "${RELEASE_NAME}" ./deploy/helm \
  --namespace "${NAMESPACE}" \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  --set clusterName="${CLUSTER_NAME}" \
  --set platformUrl="${PLATFORM_URL}" \
  --set gatewayUrl="${GATEWAY_URL}" \
  --set agentToken="${AGENT_TOKEN}" >/tmp/incidentflow-k8s-agent-rendered.yaml

log "Installing Helm release"
helm upgrade --install "${RELEASE_NAME}" ./deploy/helm \
  --namespace "${NAMESPACE}" \
  --create-namespace \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  --set clusterName="${CLUSTER_NAME}" \
  --set platformUrl="${PLATFORM_URL}" \
  --set gatewayUrl="${GATEWAY_URL}" \
  --set agentToken="${AGENT_TOKEN}"

log "Waiting for Deployment availability"
if ! kubectl -n "${NAMESPACE}" rollout status "deployment/${RELEASE_NAME}-${RELEASE_NAME}" --timeout=90s; then
  printf '\nDeployment did not become Available within timeout. Showing diagnostics.\n' >&2
fi

log "Pods"
kubectl -n "${NAMESPACE}" get pods -o wide

log "Recent logs"
kubectl -n "${NAMESPACE}" logs -l app.kubernetes.io/name=incidentflow-k8s-agent --tail=80 || true

SERVICE_ACCOUNT="${RELEASE_NAME}-${RELEASE_NAME}"
AS_USER="system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT}"

log "RBAC checks"
check_can_i() {
  local expected="$1"
  shift
  local actual
  local status
  set +e
  actual="$(kubectl auth can-i "$@" --as "${AS_USER}")"
  status=$?
  set -e
  printf 'can %s: %s\n' "$*" "${actual}"
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'expected "%s" for kubectl auth can-i %s, got "%s" with exit code %s\n' "${expected}" "$*" "${actual}" "${status}" >&2
    exit 1
  fi
}

check_can_i yes list pods
check_can_i yes get pods/log
check_can_i no get secrets
check_can_i no delete pods

log "Smoke test complete"
printf 'Expected with fake gateway: pod runs and logs reconnect attempts to %s\n' "${GATEWAY_URL}"
