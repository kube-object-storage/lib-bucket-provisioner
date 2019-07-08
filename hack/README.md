# To devs

Clone this repo with 

`go get -d github.com/github.com/kube-object-storage/lib-bucket-provisioner`

Then install the dependencies

`dep ensure -v`

## Format and Imports

Before merging code into master, be sure to run

```bash
./hack/verify-imports.sh
```

## Update generated code

  NOTE: **ONLY** do this whenever you make changes to the OBC and OB APIs in pkg/apis/objectbucket.io/v1alpha1/*types.go

```bash
./hack/update-codegen.sh
```

# TODO

- P0: solidify and implement the APIs in pkg/apis.  Until we do that, we can't deserialize our workload.
- P1 some basic Reconciler logic to execute the provisioner interfaces passed in
- P2 Robustify!
- P? profit
