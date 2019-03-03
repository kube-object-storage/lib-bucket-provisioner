## Bucket Provisioning Library
This repo is a temporary placeholder for a general purpose, object-store bucket provisioning library, very similar to the Kubernetes [sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go) library.
The goal is to eventually move this repo to a Kubernetes repo within _sig-storage/_.

### Repo Layout
The overall [bucket provisioning library design](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/object-bucket-lib.md) describes the Custom Resource Definitions, interfaces, and workflows of an `ObjectBucketClaim` and an `ObjectBucket`.
There are examples showing how object-store provisioners can use this library [here](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/doc/examples/).

Library contributors look [here](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/hack/README.md) for `make` and directions.


### Feedback and Community Input
Please submit PRs against any section of this repo, especially the library design.
Also, feel free to reach out to the initial authors: Jon Cope (jcope@redhat.com), Scott Creeley (screeley@redhat.com) and Jeff Vance (jvance@redhat.com).
