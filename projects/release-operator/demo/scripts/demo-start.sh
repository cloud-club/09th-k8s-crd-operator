#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-team-5}"
CR_NAME="${CANARY_RELEASE_NAME:-demo-app}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE_OPERATOR_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

kubectl get namespace "${NAMESPACE}" >/dev/null 2>&1 || kubectl create namespace "${NAMESPACE}"

kubectl apply -k "${SCRIPT_DIR}/../manifests"

if kubectl get canaryrelease "${CR_NAME}" -n "${NAMESPACE}" >/dev/null 2>&1; then
  echo "CanaryRelease ${CR_NAME} already exists in ${NAMESPACE}; skipping create"
else
  kubectl apply -f "${RELEASE_OPERATOR_ROOT}/config/samples/deploy_v1alpha1_canaryrelease.yaml"
  echo "Created CanaryRelease ${CR_NAME} in ${NAMESPACE}"
fi

echo "Demo dashboard applied. Port-forward example:"
echo "  kubectl port-forward -n ${NAMESPACE} svc/canary-demo-dashboard 8080:8080"
