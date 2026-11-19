#!/bin/sh
# Runs before an upgrade. Definitions owned by an older Helm release are marked
# keep so Helm does not delete them and their DefinitionRevisions.
set -eu

served=$(kubectl api-resources --api-group=core.oam.dev -o name)

for ref in $BUILTIN_DEFINITIONS; do
  resource=${ref%%/*}
  if ! printf '%s\n' "$served" | grep -qx "$resource"; then
    continue
  fi

  state=$(kubectl -n "$DEFINITION_NAMESPACE" get "$ref" --ignore-not-found \
    -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name} {.metadata.annotations.meta\.helm\.sh/release-namespace} {.metadata.annotations.helm\.sh/resource-policy}')
  if [ "$state" = "$RELEASE_NAME $CONTROLLER_NAMESPACE " ]; then
    kubectl -n "$DEFINITION_NAMESPACE" annotate "$ref" helm.sh/resource-policy=keep
  fi
done
