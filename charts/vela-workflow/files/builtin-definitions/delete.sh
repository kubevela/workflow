#!/bin/sh
# Runs before uninstall and removes definitions that are no longer regular Helm
# resources. A rendered chart applied directly must not trigger deletion.
set -eu

uninstalling=$(kubectl -n "$CONTROLLER_NAMESPACE" get secrets,configmaps \
  -l "owner=helm,name=$RELEASE_NAME,status=uninstalling" -o name)
if [ -z "$uninstalling" ]; then
  echo "No Helm uninstall of $RELEASE_NAME in progress, leaving the builtin definitions in place"
  exit 0
fi

set -- /definitions/*.yaml
if [ ! -e "$1" ]; then
  exit 0
fi

kubectl delete -f /definitions --ignore-not-found --wait=false
