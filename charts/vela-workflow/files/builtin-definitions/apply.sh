#!/bin/sh
# Runs after install, upgrade, and rollback. It waits for this release's
# controller before applying the built-in WorkflowStepDefinitions.
set -eu

kubectl -n "$CONTROLLER_NAMESPACE" rollout status "deployment/$CONTROLLER_DEPLOYMENT" \
  --timeout="${ROLLOUT_TIMEOUT_SECONDS}s"

case "$APPLY_RETRIES" in
  ''|0|0[0-9]*|*[!0-9]*)
    echo "APPLY_RETRIES must be a positive integer" >&2
    exit 1
    ;;
esac

# cert-manager injects the CA asynchronously. Runtime cert management sets it
# during controller startup, before the Deployment becomes ready.
if [ -n "$WEBHOOK_CONFIGURATION" ]; then
  waited=0
  while :; do
    ready=true
    for kind in validatingwebhookconfiguration mutatingwebhookconfiguration; do
      if ! bundles=$(kubectl get "$kind" "$WEBHOOK_CONFIGURATION" \
        -o jsonpath='{range .webhooks[*]}{.name}={.clientConfig.caBundle}{"\n"}{end}'); then
        echo "Waiting for $kind/$WEBHOOK_CONFIGURATION" >&2
        ready=false
        break
      fi
      if [ -z "$bundles" ] || printf '%s\n' "$bundles" | grep -qE '^[^=]+=(Cg==)?$'; then
        ready=false
        break
      fi
    done
    if [ "$ready" = true ]; then
      break
    fi
    if [ "$waited" -ge "$ROLLOUT_TIMEOUT_SECONDS" ]; then
      echo "Webhook $WEBHOOK_CONFIGURATION still has an unavailable CA bundle after ${waited}s" >&2
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
