#!/usr/bin/env bash
{
  echo '            - "-test.coverprofile=/workspace/data/e2e-profile.out"'
  echo '            - "__DEVEL__E2E"'
  echo '            - "-test.run=E2EMain"'
  echo '            - "-test.coverpkg=$(go list ./pkg/...| tr '"'"'\n'"'"' '"'"','"'"'| sed '"'"'s/,$//g'"'"')"'
} > tmp_add.txt
sed '/          args:/r tmp_add.txt' ./charts/vela-workflow/templates/workflow-controller.yaml > tmp.yaml
rm ./charts/vela-workflow/templates/workflow-controller.yaml
cat tmp.yaml
mv tmp.yaml ./charts/vela-workflow/templates/workflow-controller.yaml
