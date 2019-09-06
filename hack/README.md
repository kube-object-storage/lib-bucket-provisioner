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
8. add the _s3-provisioner_ namespace to the s3's deployment file, _examples/awss3provisioner-deployment.yaml_:
```
apiVersion: v1
kind: Namespace
metadata:
  name: s3-provisioner
---
```
   and create the namespace, roles and service accounts:
   `kubectl create -f awss3provisioner-deployment.yaml`

9. create the CRs:
   ```
   kubectl create -f https://raw.githubusercontent.com/kube-object-storage/lib-bucket-provisioner/master/deploy/crds/objectbucket_v1alpha1_objectbucket_crd.yaml
   kubectl create -f https://raw.githubusercontent.com/kube-object-storage/lib-bucket-provisioner/master/deploy/crds/objectbucket_v1alpha1_objectbucketclaim_crd.yaml
   ```

10. edit s3-provisioner's OWNER secret yaml (_examples/greenfield/_):
   - change `data` `to stringData` so keys don't have to be base64 encoded
   - add your non-base64 keys:
```
      AWS_ACCESS_KEY_ID: xyzzy  # unencoded
      AWS_SECRET_ACCESS_KEY: xyzzy # unencoded
```
   - create the owner secret:
      `kubectl create -f examples/greenfield/owner-secret.yaml`
11. create the storageclass (_examples/greenfield/_)
12. create the obc (_examples/greenfield/_)
13. finally, run the s3-provisioner:
   `bin/awss3provisioner -alsologtostderr -v=2`
14. [clean up](cleanup.sh) resources to test the next change.

# TODO

- P0: solidify and implement the APIs in pkg/apis.  Until we do that, we can't deserialize our workload.
- P1 some basic Reconciler logic to execute the provisioner interfaces passed in
- P2 Robustify!
- P? profit
