# To devs

Clone this repo with 

`go get -d github.com/yard-turkey/lib-bucket-provisioner`

Then install the dependencies

`dep ensure -v`

## Update generated code

  NOTE: do this whenever you make changes to the OBC and OB APIs in pkg/apis/objectbucket.io/v1alpha1/*types.go

```bash
./vendor/k8s.io/code-generator/generate-groups.sh all \
github.com/yard-turkey/lib-bucket-provisioner/pkg/client \
github.com/yard-turkey/lib-bucket-provisioner/pkg/apis \
"objectbucket.io:v1alpha1"
```

# TODO

- P0: solidify and implement the APIs in pkg/apis.  Until we do that, we can't deserialize our workload.
- P1 some basic Reconciler logic to execute the provisioner interfaces passed in
- P2 Robustify! (retry loops)
- P? profit
