## Generic Bucket Provisioning
Kubernetes natively supports dynamic provisioning for many types of file and block storage, but lacks support for object bucket provisioning. 
This repo contains the object store bucket provisioning library, very similar to the Kubernetes [sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go) library.
The goal is to eventually move this library to a Kubernetes repo within sig-storage/.

A note about this design document: the _current_ bucket provisioning design and implementation is reflected in this document,
and an effort is made to keep it up-to-date. Future considerations are tracked as _Issues_ in this repo and tagged with the `enhancement` label.

#### UPDATE (Feb 2020): 
DEPRECATION NOTICE:
This repo is not active. There is a [_Kubernetes Enhancement Proposal (KEP)_](https://github.com/kubernetes/enhancements/pull/1383) presenting this bucket provisioning enhancement. It is currently under review and it is likely that the design document below will change significantly to support a CSI-like interface.

### Table Of Contents
1. [Goals](#goals)
1. [Non-Goals](#non-goals)
1. [Assumptions](#assumptions)
1. [Design](#design)
1. [Alternatives](#alternatives)
1. [Binding](#binding)
1. [Bucket Deletion](#bucket-deletion)
1. [Bucket Sharing](#bucket-sharing)
1. [Quota](#quota)
1. [Watches](#watches)
1. [Current Restrictions](#current-restrictions)
1. [API Specifications](#api-specifications)
1. [Library - Provisioner Touch Points](#touch-points)

### Goals
+ Provide a generic, dynamic bucket provision API _similar_ to Persistent Volumes and Claims so that users familiar with the PV-PVC
model will see bucket provisioning as intuitive.
As a result, `kubectl` will be easy to use to create, list, and manage buckets and claims.
+ Create an external library, similar to what exists today in Kubernetes, to ensure the contract between the app pod and bucket store is guaranteed.
+ Rely on native Storage Classes to define the object-store and provisioner.
+ Be unopinionated about the underlying object-store and at the same time provide a flexible API such that provisioner specific features can be supported. Note: the `Parameters` stanza of StorageClasses provide this flexibility today for Kubernetes external storage providers.
+ Cause the app pod to wait until the target bucket has been created and is accessible.
+ Present similar user and admin experiences for both _greenfield_ (new) and _brownfield_ (existing) bucket provisioning.

### Non-Goals
+ Update native Kubernetes PVC-PV API to support object buckets for the following reasons:
  + very long acceptance cycle to get core API enhanements into Kubernetes (2 years+).
  + Kubernetes is strategically reducing the size of its core. CSI is an example of moving the storage "data plane" outside of Kuernetes, and is analgous in concept to this bucket provisioning design.
  + buckets are inherently different from files/directories and blocks in that there are no "attach" nor "mount" steps that would need to be performed by the kubelet.
+ Handle the small percentage of apps that will not be portable due to use of non-compatible object-store features.
For example, an app that uses a feature in object-store-1 that is not provided in object-store-2, and the app now is tied to an object-store-2 endpoint.

### Assumptions
1. There is no reasonable chance to change the PV-PVC core API (see non-goals above).
1. There is no "_best match_" binding between a bucket claim and a bucket resource, even for brownfield use cases. Thus, pre-creating object bucket resources is _not_ necessary for brownfield.
1. Apps need to be designed for object-store portability.
Just like there can be portability issues when an app exploits specialized features of a file system, an app accessing buckets, where portability matters, must be designed for that purpose.

### Design
The bucket provisioning library utilizes two new Custom Resources to abstract an object store bucket and a claim/request for such a bucket.
It's important to keep in mind that this design only defines bucket claim APIs and related library code.
The lib ensures that  the _contract_ made to app developers regarding the artifacts of bucket creation is guaranteed.
The actual creation of physical buckets and the generation of appropriate credentials belong to each object store provisioner.
The bucket library handles watches on bucket claims, reconciles state-of-the-world, creates the artifacts (Secret, ConfigMap, etc.) consumed by app pods, and deletes Kubernetes resources generated on behalf of the claim. Object store specific resources need to be cleaned up by the provisioners.

An `ObjectBucketClaim` (OBC) is similar in usage to a Persistent Volume Claim and an `ObjectBucket` (OB) is the Persistent Volume equivalent. 
An OBC is namespaced and references a storage class which defines the object store provisioner.
An OB is non-namespaced, typically not visible to end users, and will contain info pertinent to the provisioned bucket.
OBs maintain persistent _state_ information that may be needed by provisioners.
Like PVs, there is a 1:1 relationship between an OBC and an OB.
The storage class referenced by the OBC may contain provisioner specific keys, including region, bucket owner credentials, etc.
For brownfield usage the storage class _must_ contain the name of the existing bucket, thus removing knowledge of the bucket name (often random) from OBC authors.
The details of the object store and OB are typically not visible to the app pod.

As is true for dynamic PV provisioning, a bucket provisioner needs to be running for each object store supported by the Kubernetes cluster.
For example, if the underlying object store is AWS S3, the developer will create an OBC, referencing a Storage Class which references the S3 store.
The cluster has the S3 provisioner running, via a Deployment, which watches for OBCs that it knows how to handle, while other OBCs are ignored.
Additionally, the same cluster can have a Rook-Ceph RGW provisioner running which also watches OBCs, and like the S3 proivisioner, it only handles OBCs that it knows how to provision and skips the rest.

The bucket provisioners should be simple and efficient to write because the bucket provisioning library handles the bulk of the work. For example, the library performs all OBC watches, informers, reconcilation, creation of the OB, ConfigMap, Secert, finalizers and labels, retry logic and error recovery.
Each provisioner is responsible for writing `Provision`, `Delete`, `Grant`, and `Revoke` methods (with more possible in a future release).

To provision a _new_ bucket, the provisioner's `Provision` method is called by the lib, and to grant access to an existing bucket the provisioner's `Grant` method is called.
`Provision` and `Grant` return an OB which the library uses to create the Secret and ConfigMap.
The Secret and ConfigMap have deterministic names, namespaces and keys.
They also have an extra config area (_map[string]string_) to support provisioner specific endpoint and credential needs.
An app pod consuming a bucket need only be aware of the Secret and ConfigMap names and their keys.
The app pod will not run until the ConfigMap and Secret have been mounted, indicating that the bucket can be accessed.

**Note:** even though the PV-PVC design supports static provisioning, only _dynamic_ provisioning and granting access are supported by the bucket lib at this time.

### Alternatives
Various alternative designs were considered before reaching the design described here:
1. Using a service broker to provision buckets.
This doesn't alleviate the pod from consuming env variables which define the endpoint and secret keys.
It also feels too far removed from basic Kubernetes storage -- there is no claim and no object to represent the bucket.
1. A Rook-Ceph only provisioner with built-in watches, reconcilation, etc, **but** no bucket library. The main problem here is that each provisioner would need to write all of the controller code themselves.
This could easily result in different _contracts_ for different provisioners, meaning one provisioners might create the Secret of ConfigMap differently than another.
This could result in the app pod being coupled to the provisioner.
1. Rook-Ceph repo and a centralized controller.
Initially we considered a somewhat generic bucket provisioner living in the Rook repo and embedded in their existing operator (which is used to provision a ceph object store, an object user, etc).
Feedback from the Rook community was that it didn't make sense for a generic (non-rook focused) controller to live inside Rook.

### Binding
Bucket binding refers to the steps necessary to make the target bucket available to pods.
For greenfield cases a new bucket is physically created.
In both new and existing bucket use cases Kubernetes resources are created by the library: a secret containing access credentials, a configMap containing bucket endpoint info, and an OB describing the bucket.
And, in addition to these Kubernetes resources, each provisioner will likely have to create and delete their own store-specific resources.
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. (greenfield) generation of a random bucket name when requested (performed by bucket lib).
1. (greenfield) the creation of the physical bucket with owner credentials (performed by provisioner).
1. creation of the necessary object store specific resources, e.g. IAM user, policy, etc.
1. creation of an OB resource based on the provisioner's returned OB structure (performed by bucket lib).
1. creation of a ConfigMap based on the provisioner's returned OB, residing in the OBC's namespace (performed by bucket lib).
1. creation of a Secret based on the provisioner's returned credentials, residing in the OBC's namespace (performed by bucket lib).

`Bound` is one of the supported phases of an OB and an OBC.
`Bound` indicates that a bucket and all related artifacts have been created on behalf of the OBC. Once a bucket claim is bound the app pod can run, meaning the Secret (containing access credentials) and the ConfigMap (containing the bucket endpoint) are mounted and consumable by the pod.

### Bucket Deletion
The library adds a _finalizer_ to all generated resources (secret, configmap, etc.) and to the user's OBC. This is similar to current Kubernetes behavior where a PVC is "protected" from accidental deletion and to keep PV-PVCs in sync.
In the case of bucket provisioning, the finalizers help keep Kubernetes bucket related resources orchestrated consistently to prevent orphaned OBs, etc.

For greenfield buckets, when an OBC is deleted, the provisioner's `Delete` or `Revoke` method is called depending on the OB's _reclaimPolicy_ (which reflects the assoicated storage class's reclaim policy).
If the storage class's reclaim policy is "Delete" then the `Delete` method is called and the bucket is expected to be physically removed.
If the reclaim policy is "Retain" then the `Revoke` method is called and the bucket is expected to remain with all its data (objects) intact.
Future reclaim policy support is proposed in issue #53.

For brownfield buckets, when an OBC is deleted, the provisioner's `Revoke` method is called.
The provisioner decides whether or not to recognize the reclaimPolicy.
It is anticipated that most provisioners will choose to ignore the reclaimPolicy and simply cleanup up credentials, users, etc.
However, a provisoner is free to implemement whatever best suites the needs of the object store and its users.
This will likely include store-specific clean up such as deleting credentials, detach, archive, etc. at the discretion of the provisioner.

In both brownfield and greenfield delete cases, the library attempts to delete _all_ generated Kubernetes artifacts: OB, Secret and ConfigMap.

### Bucket Sharing
Within the same object store a bucket can be shared, via the same OBC within the same namespace, or even across namespaces.
The reason for this is that the app pods never reference the OBC (or OB) directly, but instead consume a Secret and ConfigMap in order to access the bucket.
If OBCs in different namespaces reference the same brownfield storage class then sharing can occur across namespaces.
Each namespace will have its own Secret and ConfigMap which will be identical to the other secrets and config maps sharing the bucket, other than the namespace name.

### Quota
(applicable only to new buckets)

Bucket size generally cannot be specified; however, the current size of a bucket can usually be monitored.
The number of buckets can be controlled by a resource quota once [this k8s pr](https://github.com/kubernetes/kubernetes/pull/72384) is merged.
Until then, Resource Quotas cannot yet be defined for CRDs and, thus, there is no quota on the number of buckets.

### Watches

#### OBC Watches
Provisioners importing the bucket library watch all OBCs across a designated namespace or across all namespaces.
OBCs that match the provisioner are further processed and OBCs not matching are quickly skipped.

The OBC watch performs the following:
+ detects a new OBC:
  + skip if the OBC's StorageClass's provisioner != the provisioner doing this watch
  + generate random name if requested (greenfield)
  + invokes the `Provision` or `Grant` method for the provisioner defined in the OBC's storage class, depending on the presence/absence of a bucket name in the referenced storage class
  + if the provisioning is successful, create in the following order:
    + a Secret, in the namespace as the OBC, containing the bucket credentials returned by the provisioner
    + a ConfigMap, in the namespace as the OBC, containing the bucket's endpoint info
    + a global OB which references the OBC and storage class and contains store-specific bucket info
    + add finalizers and labels to the resources above and to the OBC
  + if the provisioner returns an error:
    + retry:
      + (greenfield) call `Delete` in case the bucket was created (want idempotency for next try). **Note**: this is subject to change per issue #151.
      + call `Provision` or `Grant` again
+ detects OBC delete events:
  + skip if the OBC's StorageClass's provisioner != the provisioner doing this watch
  + invoke the `Delete` method when the reclaim policy is "delete" (greenfield)
  + invoke the `Revoke` method when the reclaim policy is "retain"
  + delete the related Secret, ConfigMap and the OB (in that order)

### Current Restrictions
+ there is no event recording thus events are not shown in commands like `kubectl describe obc`.
+ there is no ability to _cancel_ bucket provisioning
+ there is no way to define a _reclaimPolicy_ that supports erasing or suspending a bucket
+ there are no bucket metrics
+ there is no bucket lifecycle management (e.g. ability to define expiration, archive, migration, etc. policies)
+ security relies soley on RBAC, thus there is no way to distinguish bucket access within the same namespace
+ there is no HA due to no leader election in the lib -- if the provisioner is running in a goroutine (e.g. rook-ceph provisioner) and it fails the lib cannot be restarted
+ logging verbosity levels are somewhat arbitrary

## API Specifications

### OBC Custom Resource (User Defined)
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
metadata:
  name: MY-BUCKET-1 [1]
  namespace: USER-NAMESPACE [2]
spec:
  bucketName: [3]
  generateBucketName: "photo-booth" [4]
  storageClassName: AN-OBJECT-STORE-STORAGE-CLASS [5]
  additionalConfig: [6]
    ANY_KEY: VALUE ...
```
1. name of the ObjectBucketClaim. This name becomes the name of the Secret and ConfigMap.
1. namespace of the ObjectBucketClaim, which is also the namespace of the ConfigMap and Secret.
1. name of the bucket. If supplied then `generateBucketName` is ignored.
**Not** recommended for new buckets since names must be unique within
an entire object store.
1. if supplied then `bucketName` must be empty. This value becomes the prefix for a randomly generated name.
After `Provision` returns `bucketName` is set to this random name.
If both `bucketName` and `generateBucketName` are supplied then `BucketName` has precedence and `GenerateBucketName` is ignored. 
If both `bucketName` and `generateBucketName` are blank or omitted then the storage class is expected to contain the name of an _existing_ bucket. It's an error if all three bucket related names are blank or omitted.
1. storageClass which defines the object-store service and the bucket provisioner.
1. additionalConfig gives providers a location to set proprietary config values (tenant, namespace...).
The value is a list of 1 or more key-value pairs.

### OBC Custom Resource (after update by lib)
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
metadata:
  finalizers: [1]
  - objectbucket.io/finalizer
  labels: [2]
    bucket-provisioner: aws-s3.io-bucket [3]
spec:
  bucketName: photo-booth-62PrQ [4]
  objectBucketRef: objectReference{} [5]
  configMapRef: objectReference{} [6]
  secretRef: objectReference{} [7]
status:
  phase: {"Pending", "Bound", "Released", "Failed"} [8]
```
1. the finalizer added by the library, the name is a constant.
1. the library adds a label (seen here) but each provisioner can
   supply their own labels.
1. the label value shown is the name of the provisioner but due to
   Kubernetes restrictions slash (/) is replaced by a dash (-). 
   In this example the provisioner name is `aws-s3.io/bucket`.
1. the generated, unique bucket name for the new bucket.
1. objectReference to the generated OB.
1. objectReference to the generated ConfigMap.
1. objectReference to the generated Secret.
1. phases of bucket creation:
    - _Pending_: the operator is processing the request
    - _Bound_: the operator finished processing the request and linked the OBC and OB
    - _Released_: the OB has been deleted, leaving the OBC unclaimed but unavailable.
    - _Failed_: not currently set.

### Generated Secret (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: MY-BUCKET-1 [1]
  namespace: OBC-NAMESPACE [2]
  finalizers: [3]
  - objectbucket.io/finalizer
  labels: [4]
    bucket-provisioner: aws-s3.io-bucket [5]
  ownerReferences:
  - name: MY-BUCKET-1 [6]
    ...
type: Opaque
data:
  ACCESS_KEY_ID: BASE64_ENCODED-1
  SECRET_ACCESS_KEY: BASE64_ENCODED-2
  ... [5]
```
1. same name as the OBC. Unique since the secret is in the same namespace as the OBC.
1. namespce of the originating OBC.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the Secret.
1. the library adds a label (seen here) but each provisioner can supply their own labels.
1. the label value shown is the name of the provisioner but due to Kubernetes restrictions slash (/) is
   replaced by a dash (-). In this example the provisioner name is `aws-s3.io/bucket`.
1. ownerReference makes this secret a child of the originating OBC for clean up purposes.
1. ACCESS_KEY_ID and SECRET_ACCESS_KEY are the only secret keys defined by the library.
Provisioners are able to cause the lib to create additional keys by returning  the `AdditionalSecretConfig` field.
**Note:** the library will create the Secret using `stringData:` and let the Secret API base64 encode the values.
Eg: 
```
stringData:
  endpoint: |-
    ACCESS_KEY_ID: NON-BASE64-STRING
    SECRET_ACCESS_KEY: NON-BASE64-STRING
```

### Generated ConfigMap (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: MY-BUCKET-1 [1]
  namespace: OBC-NAMESPACE [2]
  finalizers: [3]
  - objectbucket.io/finalizer
  labels: [4]
    bucket-provisioner: aws-s3.io-bucket [5]
  ownerReferences: [6]
  - name: MY-BUCKET-1
    ...
data: 
  BUCKET_HOST: http://MY-STORE-URL [7]
  BUCKET_PORT: 80 [8]
  BUCKET_NAME: MY-BUCKET-1 [9]
  BUCKET_REGION: us-west-1
  ... [10]
```
1. same name as the OBC. Unique since the configMap is in the same namespace as the OBC.
1. determined by the namespace of the ObjectBucketClaim.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the ConfigMap.
1. the library adds a label (seen here) but each provisioner can supply their own labels.
1. the label value shown is the name of the provisioner but due to Kubernetes restrictions slash (/) is
   replaced by a dash (-). In this example the provisioner name is `aws-s3.io/bucket`.
1. ownerReference sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap.
1. host URL.
1. host port.
1. unique bucket name.
1. the above data keys are defined by the library.
Provisioners are able to cause the lib to create additional data keys by returning the `AdditionalConfigData` field.

### App Pod (independent of provisioner)
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: app-pod
  namespace: dev-user
spec:
  containers:
  - name: mycontainer
    image: redis
    envFrom: [1]
    - configMapRef: 
        name: MY-BUCKET-1 [2]
    - secretRef:
        name: MY-BUCKET-1 [3]
```
1. use `env:` if mapping of the defined key names to the env var names used by the app is needed.
1. makes available to the pod as env variables: BUCKET_HOST, BUCKET_PORT, BUCKET_NAME
1. makes available to the pod as env variables: ACCESS_KEY_ID, SECRET_ACCESS_KEY

 ### Generated OB Custom Resource
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucket
Metadata:
  name: obc-my-ns-my-bucket [1]
  labels:
    bucket-provisioner: AN-OBJECT-STORE-STORAGE-CLASS [2]
  finalizers:
  - "objectbucket.io/finalizer" [3]
spec:
  storageClassName: example-obj-prov [4]
  claimRef: *v1.objectreference [5]
  reclaimPolicy: {"Delete", "Retain"} [6]
  endpoint:
    bucketHost: foo.bar.com
    bucketPort: 8080
    bucketName: my-photos-1xj4a
    region: # provisioner dependent
    subRegion: # provisioner dependent
    additionalConfigData: [] #string:string
  additionalState: [] #string:string
status:
  phase: {"Bound", "Released", "Failed"} [7]

```
1. name is constructed in the pattern: obc-OBC_NAMESPACE-OBC_NAME
1. the label value shown is the name of the provisioner but due to Kubernetes restrictions slash (/) is
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of an OB.
   replaced by a dash (-). In this example the provisioner name is `aws-s3.io/bucket`.
1. name of the storage class, referenced by the OBC, containing the provisioner and object store service name.
1. objectReference to the associated OBC.
1. reclaim policy from the Storge Class referenced in the OBC.
1. phase is the current state of the ObjectBucket:
    - _Bound_: the operator finished processing the request and linked the OBC and OB
    - _Released_: the OBC has been deleted, leaving the OB unclaimed.
    - _Failed_: not currently set.

### StorageClass (sample for an S3 provider)
```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: MyAwsS3Class
  labels: 
    aws-s3/object [1]
provisioner: aws-s3.io/bucket [2]
parameters: [3]
  region: us-west-1
  secretName: s3-bucket-owner
  secretNamespace: s3-provisioner
  bucketName: existing-bucket [4]
reclaimPolicy: Delete [5]
```
1. (optional) the label here associates this StorageClass to a specific provisioner.
1. provisioner responsible for handling OBCs referencing this StorageClass.
1. **all** parameter keys and values are specific to a provisioner, are optional, and are not validated by the StorageClass API.
Fields to consider are object-store endpoint, version, possibly a secretRef containing info about credential for new bucket owners, etc.
1. bucketName is required for access to existing buckets.
Unlike greenfield provisioning, the brownfield bucket name appears in the storage class, not the OBC.
1. each provisioner decides how to treat the _reclaimPolicy_ when an OBC is deleted. Supported values are:
+ _Delete_ = (typically) physically delete the bucket.
Depending on new vs. existing bucket, the provisioner's `Delete` or `Revoke` methods are called.
+ _Retain_ = (typically) do not physically delete the bucket.
Depending on the provisioner, various clean up steps can be performed, such as deleting users, revoking credentials, etc.
For both new and existing buckets the provisioner's `Revoke` method is called.

### OBC Custom Resource Definition
```yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: objectbucketclaims.objectbucket.io
spec:
  group: objectbucket.io
  names:
    kind: ObjectBucketClaim
    listKind: ObjectBucketClaimList
    plural: objectbucketclaims
    singular: objectbucketclaim
  scope: Namespaced
  version: v1alpha1
  subresources:
    status: {}
```

### OB Custom Resource Definition
```yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: objectbuckets.objectbucket.io
spec:
  group: objectbucket.io
  names:
    kind: ObjectBucket
    listKind: ObjectBucketList
    plural: objectbuckets
    singular: objectbucket
  scope: Namespaced
  version: v1alpha1
  subresources:
    status: {}
```

### Touch Points
These are the only interactions between the library and a provisioner:

- **`NewProvisioner`** is a required function called by provisioners to create the library's controller struct which is returned to the provisioner.
Each provisioner defines their own struct, passed to `NewProvision`, which implements the Interfaces below.
The returned struct supports the `Run` and `SetLabels` methods.

- **`Run`** is a required controller method called by provisioners to start the OBC controller.

- **`SetLabels`** is an optional controller method called by provisioners to define the labels applied to the Kubernetes resrources created by the library.

#### Interfaces
The following interfaces must be implemented on the provisioner-defined structure which is passed to `NewProvisioner`:

- **`Provision`** is a method called by the library when a new OBC is detected and its storage class does not contain the bucket name, meaning "greenfield" provisioning.
In this case the OBC contains a bucket name or a name is generated.
Provisioners are expected to create a new bucket and related artifacts such as user, policies, credentials, etc.
Provisioners return a skeleton OB structure.

- **`Grant`** is a method called by the library when a new OBC is detected and its storage class contains the bucket name, meaning "brownfield" provisioning.
In this case the OBC does not contain the bucket name.
Provisioners are expected to create artifacts such as user, policies, credentials, etc., but not to create a new bucket.
Provisioners return a skeleton OB structure.

- **`Delete`** is a method called by the library when an OBC is deleted, and its storage class does not contain the bucket name (meaning "greenfield" provisioning had occurred), and the storage class's `reclaimPolicy` is "Delete".
Provisioners are expected to remove the bucket and related artifacts.

- **`Revoke`** is a method called by the library when an OBC is deleted and one of the following situations exists.
Provisioners are expected to retain the bucket but delete the related artifacts.
  - the OBC's storage class contains the bucket name, meaning "brownfield" provisioning had occurred.
  In this case the storage class's `reclaimPolicy` is ignored
  - "greenfield" provisioning occurred and the storage class's `reclaimPolicy` is "Retain".
  

