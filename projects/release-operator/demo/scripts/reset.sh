#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-team-5}"
CR_NAME="${CANARY_RELEASE_NAME:-demo-app}"
CANARY_DEPLOY="${CANARY_DEPLOY:-demo-app-canary}"
STABLE_DEPLOY="${STABLE_DEPLOY:-demo-app}"
STABLE_REPLICAS="${STABLE_REPLICAS:-10}"

kubectl delete canaryrelease "${CR_NAME}" -n "${NAMESPACE}" --ignore-not-found
kubectl delete deployment "${CANARY_DEPLOY}" -n "${NAMESPACE}" --ignore-not-found
kubectl scale deployment "${STABLE_DEPLOY}" -n "${NAMESPACE}" --replicas="${STABLE_REPLICAS}"
echo "Demo reset complete in namespace ${NAMESPACE}"
