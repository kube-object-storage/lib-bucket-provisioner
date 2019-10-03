# To devs

Clone this repo with 

`go get -d github.com/github.com/kube-object-storage/lib-bucket-provisioner`

Then install the dependencies

`dep ensure -v`

## Build Scripts

### golangci-lint

Linting and vetting are handled with [golangci-lint](https://github.com/golangci/golangci-lint), which must be installed prior to executing 
the respective operations with `./hack/go.sh`.

Developers should use golangci-lint version v1.16.0.  Installation instructions recommend against managing this
dependency through dep.

Download the golangci-lint v1.16.0 binary from the [releases](https://github.com/golangci/golangci-lint/releases/tag/v1.16.0) or follow the [installation instructions](https://github.com/golangci/golangci-lint#local-installation) for your OS.

Some basic developer workflows are wrapped up in the [./hack/go.sh](./go.sh) script.  Because of certain quirks with Kubernetes' generated code,
it's **highly recommended** that script be used instead of the common golang workflows (build, test, vet, etc.).  The script is written
with these quirks in mind and prevents the false-negatives that occur otherwise.

Each operation is triggered via a command line argument passed to the script.  One or more arguments may be passed at once, separated by whitespace.
Here is a rundown of each.

###### `./hack/go.sh help`

Surprise! It prints the help menu.

###### `./hack/go.sh build`

Executes a `go build` of `./pkg/...`. Since there is no binary to produce, this is only a test of compilability.  This differs from `vet` in that it incorporates generated code.

###### `./hack/go.sh vet`

Runs `go vet` against all non-generated code under `./pkg/...`.  This is a workaround for known issues with generated Kubernetes code.

###### `./hack/go.sh test`

Executes unit tests under `./pkg/...`.

###### `./hack/go.sh imports`

Iterates over all non-generated packages to organize imports according a predefined pattern.

###### `./hack/go.sh imports-check`

The same as `imports` but only reports errors or diffs, does not write to files.  Useful in CI.

###### `./hack/go.sh lint`

Runs the pre-configured golangci-lint binary.

###### `./hack/go.sh linters`

Lists the enabled and disabled linters aggregated by golangci-lint.

###### `./hack/go.sh ci-checks`

Aggregates operations for execution by CI.  Right now this is `lint`, `test`, and `build`.  `vet` is executed by the linter.

## Update generated code

  NOTE: **ONLY** do this whenever you make changes to the OBC and OB APIs in pkg/apis/objectbucket.io/v1alpha1/*types.go


`./hack/update-codegen.sh`


## Library testing
The easist way to test the library is via the [AWS S3 provisioner](https://github.com/yard-turkey/aws-s3-provisioner) and [minikube](https://github.com/kubernetes/minikube). This approach runs the s3 provisioner as a binary (no need for containers, pods, etc).
Here are the steps:

1. clone the s3-provisioner repo
2. vendor in dependencies:
   `[v]go mod vendor`  # only needs to be done once
3. from the lib repo, copy changed lib file(s) to the target s3-provisioner location under _vendor/_. Eg:
   `cp pkg/provisioner/controller.go $HOME/go/src/github.com/yard-turkey/aws-s3-provisioner/vendor/github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/`
4. build the s3 provisioner (from the s3-provisioner dir):
   `go build -a -o ./bin/aws-s3-provisioner ./cmd/...`
5. in another window, start minikube:
   `minikube start --vm-driver=kvm2 --kubernetes-version=v1.14.0 --memory=5000 --cpus=4`
6. [optional?] update the k8s context:
   `minikube update-context`
7. set KUBECONFIG env variable:
   `export KUBECONFIG=/home/jvance/.kube/config`
8. create the CRs:
   ```
   kubectl create -f https://raw.githubusercontent.com/kube-object-storage/lib-bucket-provisioner/master/deploy/crds/objectbucket_v1alpha1_objectbucket_crd.yaml
   kubectl create -f https://raw.githubusercontent.com/kube-object-storage/lib-bucket-provisioner/master/deploy/crds/objectbucket_v1alpha1_objectbucketclaim_crd.yaml
   ```
9. edit s3-provisioner's OWNER secret yaml (_examples/greenfield/_):
   - change `data` `to stringData` so keys don't have to be base64 encoded
   - add your non-base64 keys:
```
      AWS_ACCESS_KEY_ID: xyzzy  # unencoded
      AWS_SECRET_ACCESS_KEY: xyzzy # unencoded
```
   - create the owner secret:
      `kubectl create -f examples/greenfield/owner-secret.yaml`
10. create the storageclass (_examples/greenfield/_)
11. create the obc (_examples/greenfield/_)
12. finally, run the s3-provisioner:
   `bin/aws-s3-provisioner -alsologtostderr -v=2`
13. [clean up](cleanup.sh) resources to test the next change.

# TODO

- P0: solidify and implement the APIs in pkg/apis.  Until we do that, we can't deserialize our workload.
- P1 some basic Reconciler logic to execute the provisioner interfaces passed in
- P2 Robustify!
- P? profit
