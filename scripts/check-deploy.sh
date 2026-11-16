#!/bin/bash
# Validates deploy/ (the production manifests Flux applies from this public
# repository). Needs kubectl and kubeconform.
set -euo pipefail
cd "$(dirname "$0")/.."
out=$(kubectl kustomize deploy)
# Public repository: a Secret here would be published.
if grep -qE '^kind: Secret$' <<<"$out"; then
  echo "deploy/ must not contain a Secret: this repository is public" >&2
  exit 1
fi
# Flux applies everything into the vibetype namespace (targetNamespace) with a
# service account that may only touch that namespace; an explicit namespace
# here would only fail at apply time, so catch it now.
if grep -qE '^  namespace:' <<<"$out"; then
  echo "deploy/ must not set metadata.namespace (Flux sets the target namespace)" >&2
  exit 1
fi
# Only kinds the deploy service account may manage (see deploy/README.md).
kinds=$(grep -E '^kind: ' <<<"$out" | sort -u | sed 's/^kind: //' | tr '\n' ' ')
for k in $kinds; do
  case $k in
    Deployment|Service|IngressRoute|Middleware) ;;
    *) echo "deploy/ contains kind $k, which the Flux service account cannot apply; extend its Role in guettli/gitops first" >&2; exit 1;;
  esac
done
# Traefik CRDs have no schema in kubeconform's default catalogue.
kubeconform -strict -summary -ignore-missing-schemas <<<"$out"
echo "deploy/ ok: $kinds"
