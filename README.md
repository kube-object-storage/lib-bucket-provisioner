## Bucket Provisioning Library
This repo is a temporary placeholder for a general purpose object-store bucket provisioning library, very similar to the Kubernetes [sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go) library.
The goal is to eventually move this repo to a Kubernetes repo within _sig-storage/_.

### Repo Layout
The overall [bucket provisioning library design](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/doc/design/object-bucket-lib.md) describes the Custom Resource Definitions, interfaces, and workflows of an `ObjectBucketClaim` and an `ObjectBucket`.
This documents is kept up-to-date and reflects the current design and implementation of bucket provisioning library.
Future designs and considerations have been removed from the design document, and are tracked as _Issues_ with the `enhancement` label.

There are [examples](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/doc/examples/) showing how object-store provisioners can use this library.

Library contributors should look [here](https://github.com/yard-turkey/lib-bucket-provisioner/blob/master/hack/README.md) for `make` and directions.

Provisioner Developers should look [here](https://github.com/yard-turkey/examples-and-blogs/blob/master/examples/sample-how-to-write-provisioner.md) for some guidance/recommendations/tips on how to get started with creating your own Provisioner. 


### Feedback and Community Input
Please submit PRs against any section of this repo, especially the library design.
