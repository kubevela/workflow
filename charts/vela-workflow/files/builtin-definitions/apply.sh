#!/bin/sh
# Runs after install, upgrade, and rollback. It waits for this release's
# controller before applying the built-in WorkflowStepDefinitions.
set -eu

kubectl -n "$CONTROLLER_NAMESPACE" rollout status "deployment/$CONTROLLER_DEPLOYMENT" \
  --timeout="${ROLLOUT_TIMEOUT_SECONDS}s"

# cert-manager injects the CA asynchronously. Runtime cert management sets it
# during controller startup, before the Deployment becomes ready.
if [ -n "$WEBHOOK_CONFIGURATION" ]; then
  waited=0
  while kubectl get validatingwebhookconfiguration "$WEBHOOK_CONFIGURATION" \
    -o jsonpath='{range .webhooks[*]}{.name}={.clientConfig.caBundle}{"\n"}{end}' | grep -qE '^[^=]+=(Cg==)?$'; do
    if [ "$waited" -ge "$ROLLOUT_TIMEOUT_SECONDS" ]; then
      echo "Webhook $WEBHOOK_CONFIGURATION still has no CA bundle after ${waited}s" >&2
      exit 1
    fi
    sleep 5
    waited=$((waited + 5))
  done
fi

attempt=1
until kubectl apply --server-side --force-conflicts \
  --field-manager=vela-workflow-builtin-definitions -f /definitions; do
  if [ "$attempt" -ge "$APPLY_RETRIES" ]; then
    echo "Giving up after $attempt attempts" >&2
    exit 1
  fi
  attempt=$((attempt + 1))
  sleep 10
done
