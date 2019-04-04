## Generic Bucket Provisioning
Kubernetes natively supports dynamic provisioning for many types of file and block storage, but lacks support for object bucket provisioning. 
This repo is a placeholder for an object store bucket provisioning library, very similar to the Kubernetes [sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go) library.
The (stretch) goal is to eventually move this library to a Kubernetes repo within sig-storage/.

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
1. [API Specifications](#api-specifications)
1. [Interfaces](#interfaces)
1. [Future Considerations](#future-considerations)

### Goals
+ Provide a generic, dynamic bucket provision API _similar_ to Persistent Volumes and Claims so that users familiar with the PV-PVC
model will see bucket provisioning as intuitive.
As a result, `kubectl` will be easy to use to create, list, and manage buckets and claims.
+ Create an external library, similar to what exists today in Kubernetes, to ensure the contract between the app pod and bucket store is guaranteed.
+ Rely on native Storage Classes to define the object-store and provisioner.
+ Be unopinionated about the underlying object-store.
+ Give provisioners a simple interface with minimal constraints.
+ Cause the app pod to wait until the target bucket has been created and is accessible.
+ Present similar user and admin experiences for both _greenfield_ (new) and _brownfield_ (existing) bucket provisioning.

### Non-Goals
+ Update Kubernetes PVC-PV API to support object buckets.
+ Handle the small percentage of apps that will not be portable due to use of non-compatible object-store features.
For example, an app that uses a feature in object-store-1 that is not provided in object-store-2, and the app now is tied to an object-store-2 endpoint.

### Assumptions
1. There is no "_best match_" binding between a bucket claim and a bucket resource, even for brownfield use cases. Thus, pre-creating object bucket resources is _not_ necessary for brownfield.
1. Apps need to be designed for object-store portability.
Just like there can be portability issues when an app exploits specialized features of a file system, an app accessing buckets, where portability matters, must be designed for that purpose.

### Design
We propose two new Custom Resources to abstract an object store bucket and a claim/request for such a bucket.
It's important to keep in mind that this proposal only defines bucket and bucket claim APIs and related library code.
The lib ensures that  the _contract_ made to app developers regarding the artifacts of bucket creation is guaranteed.
The actual creation of physical buckets belongs to each object store provisioner.
The bucket library handles watches on bucket claims and the (generated) bucket objects, reconciles state-of-the-world, creates the artifacts (Secret, ConfigMap) consumed by app pods, and deletes resources generated on behalf of the claim.

An `ObjectBucketClaim` (OBC) is similar in usage to a Persistent Volume Claim and an `ObjectBucket` (OB) is the Persistent Volume equivalent. 
An OBC is namespaced and references a storage class which defines the object store provisioner. The name of the bucket may also defined in the storage class for brownfield uses, removing knowledge of the bucket name (often random) from OBC authors.
Other provisioner specific fields may appear in the storage class depending on the needs of the provisioner.
An OB is non-namespaced, typically not visible to end users, and will contain info pertinent to the provisioned bucket.
Like PVs, there is a 1:1 relationship between an OBC and an OB.
The details of the object store are not visible to the app pod.

As is true for dynamic PV provisioning, a bucket provisioner needs to be running for each object store supported by the Kubernetes cluster.
For example, if the underlying object store is AWS S3, the developer will create an OBC, referencing
a Storage Class which references the S3 store.
The cluster has the S3 provisioner running which is watching (via the bucket lib) for OBCs that it knows how to handle, while
other OBCs are ignored.
Additionally, the same cluster can have a rook-ceph RGW provisioner running which also watches OBCs (again via the lib).
Like the S3 proivisioner, it only handles OBCs that it knows how to provision and skips the rest.

In this proposal, the bucket provisioners should be simple to write because the bucket provisioning library handles the bulk of the work. For example, the library handles all watches, informers, reconcilation, creation of the OB, ConfigMap, Secert, retry logic and error recovery.
Each provisioner is responsible for writing `Provision`, `Delete` `Suspend`(TBD) `Erase`(TBD), `Grant`, and `Revoke` methods.

To provision a _new_ bucket, the provisioner's `Provision` method is called by the lib, and to grant access to an existing bucket the provisioner's `Grant` method is called.
`Provision` and `Grant` return an OB which the library uses to create the Secret and ConfigMap.
The Secret and ConfigMap have deterministic names, namespaces, and property keys.
They also have an extra _map[string]string_ list to support provisioner specific endpoint and credential needs.
An app pod consuming a bucket need only be aware of the Secret name and keys, and the ConfigMap name and fields.
The app pod will not run until the ConfigMap and Secret have been mounted, indicating that the bucket can be accessed.

**Note:** even though the PV-PVC design supports static provisioning, only dynamic provisioning is supported by the bucket lib at this time.
("Provision" here includes creating a new bucket or wrapping Kubernetes artifacts around an existing bucket.)

### Alternatives
A couple of alternative designs were considered before reaching the design described here:
1. Using a service broker to provision buckets.
This doesn't alleviate the pod from consuming env variables which define the endpoint and secret keys.
It also feels too far removed from basic Kubernetes storage -- there is no claim and no object to represent the bucket.
1. A Rook-Ceph only provisioner with built-in watches, reconcilation, etc, **but** no bucket library. The main problem here is that each provisioner would need to write all of the controller code themselves.
This could easily result in different _contracts_ for different provisioners, meaning one provisioners might create the Secret of ConfigMap differently than another.
This could result in the app pod being coupled to the provisioner.
1. Rook-Ceph repo and a centralized controller.
Initially we considered a somewhat generic bucket provisioner living in the Rook repo and embedded in their existing operator (which is used to provision a ceph object store, an object user, etc).
Feedback from the Rook community was that it didn't make sense for a generic (non-rook focused) controller to live inside Rook.
1. Bucket library (as described here) but with the object bucket controller being invoked by each provisioner.
This approach is fully decentralized with each provisioner running watches on its own OBCs and on all OBs.
There has been concern expressed about overhead having N provisioners all running the same OB controller watching the same OBs, and behaving the same for all OBs.
Another issue was that a decentralized design didn't support a reasonable separation of concerns: namely, _Provisioner-1_, when reconciling orphaned OBs, could end up deleting an OB for a different provisioner.
**Note:** this alternative in not relevant to Phase-0 of this proposal since there will be no OB controller until a later phase.

### Binding
Bucket binding refers to the steps necessary to make the target bucket available to pods.
For greenfield cases, a new bucket is physically created, whereas for existing buckets this step is skipped.
In both new and existing bucket use cases Kubernetes resources are created by the library: a secret containing access credentials, a configMap containing bucket endpoint info, and an OB describing the bucket.
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. (greenfield) generation of a random bucket name when requested (performed by bucket lib).
1. (greenfield) the creation of the physical bucket with owner credentials (performed by provisioner).
1. the creation of a ConfigMap, based on the provisioner's returned OB, residing in the OBC's namespace (performed by bucket lib).
1. the creation of a Secret, based on the provisioner's returned credentials, residing in the OBC's namespace (performed by bucket lib).
This step usually requires the provisioner to create an object store user with the appropriate bucket access credentials.

`Bound` is one of the supported phases of an OB and an OBC.
`Bound` indicates that a bucket and all related artifacts have been created on behalf of the OBC. Once a bucket claim is bound the app pod can run, meaning the Secret (containing access credentials) and the ConfigMap (containing the bucket endpoint) are mounted and consumable by the pod.

### Bucket Deletion
For greenfield buckets, when an OBC is deleted, the provisioner's `Delete`, `Suspend`, or `Erase` methods will be called depending on the OB's _reclaimPolicy_.
If the storage class's reclaim policy is "Delete" then the provisioner's `Delete` method is called and the bucket is expected to be physically removed.
If the reclaim policy is "Retain" then the provisioner's `Suspend` method is called and the bucket is expected to remain with all its data (objects) intact.
If the reclaim policy is "Erase" then the provisioner's `Erase` method is called and the bucket is expected to remain with all its data (objects) removed. This policy allows the bucket name to be preserved while deleting its contents.

For brownfield buckets, when an OBC is deleted, the provisioner's `Revoke` method is called.
The provisioner decides whether or not to recognize the reclaimPolicy.
It is anticipated that most provisioners will chose to ignore the reclaimPolicy and simply cleanup up credentials, users, etc.

In all delete cases store-specific clean up, such as deleting credentials, detach, archive, etc. can be performed by any of the delete related methods.
In all delete cases, the library attempts to delete _all_ generated Kubernetes artifacts: OB, Secret, and ConfigMap.


#### Orphaned Object Buckets (post phase-0)
The implementation of Kubernetes _informers_ recognizes that sometimes Delete events are missed.
Controllers (and this lib), therefore, need to be robust enough to infer deletes via other mechanisms, such as Status or existence of a binding related property.
For example, an OBC can be deleted from the cluster, but the delete event is missed and thus the controller doesn't know the delete occurred.
In this scenario, the associated OB (and possibly the Secret and/or ConfigMap) remain, resulting in an _orphaned_ OB.
An orphaned OB is an OB with no matching OBC, and thus the state of the cluster is inconsistent.

**Note:** this definition assumes no static OBs for brownfield use.

The solution is to delete orphaned OBs, but how are they detected since OBC watches are triggered by OBC events and there are no "events" associated with orphaned OBs.
A solution is to run a centralized, provisioner-agnostic OB controller that watches all OBs, and detects and deletes orphans.

**Note:** this section is subject to change as we consider brownfield bucket implementation details.

There will be only one OB controller running per cluster, not the N controllers needed to support N bucket provisioners.
There is still no event to trigger the detection of orphaned OBs, so a reasonably short sync period will cause the OB controller to re-fetch all OBs and look for orphans.
When an orphaned OB is found the controller will check to see if the Secret and ConfigMap exist, and if so they will also be deleted.
Even though there are no watches on Secrets and ConfigMaps, there is a lower (no?) probability for orphaned Secrets and/or ConfigMaps since they are deleted prior to the OB, and orphan OB cleanup also delete Secrets and ConfigMaps.

Furthermore, to reduce the chances of an admin deleting a _bound_ OB, and/or Secret and/or ConfigMap, a finalizer is added to these resources. 
The library's OBC controller will remove the finalizers when an OBC is deleted.
Note: if an orphaned OB must be deleted manually, the finalizer must first be removed manually.

**Note:** the bucket library has no mechanism to prevent an OBC from being deleted when one or more pods indirectly reference the OBC via the Secret and ConfigMap.
This feature came late for PVCs, see [merged pr](https://github.com/kubernetes/community/pull/1174/files), and may be even more difficult to implement for OBCs.

### Bucket Sharing
A bucket can be shared, at least within the same namespace.
The reason for this is that the app pods never reference the OBC (or OB) directly, but instead consume a Secret and ConfigMap in order to access the bucket.
Since more than one pod can ingest the same Secert and ConfigMap, a bucket can be shared.

### Quota
(applicable only to new buckets)

Bucket size generally cannot be specified; however, the current size of a bucke can usually be monitored.
The number of buckets can be controlled by a resource quota once [this k8s pr](https://github.com/kubernetes/kubernetes/pull/72384) is merged.
Until then, Resource Quotas cannot yet be defined for CRDs and, thus, there is no quota on the number of buckets.

### Watches

#### OBC Watches
All provisioners importing the bucket library watch all OBCs across all namespaces.
OBCs that match the provisioner are further processed and OBCs not matching are quickly skipped.

The OBC watch performs the following:
+ detects a new OBC:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + generate random name if requested (greenfield)
  + invokes the `Provision` or `Grant` method for the provisioner defined in the OBC's storage class, depending on the presence/absence of a bucket name in the referenced storage class
  + if the provisioning is successful:
    + create a global OB which references the OBC and storage class and contains store-specific bucket info
    + create a Secret, in the namespace as the OBC, containing the bucket credentials returned by the provisioner
    + create the ConfigMap, in the namespace as the OBC, containing the bucket's endpoint info
    + in the above order!
  + if the provisioner returns an error:
    + retry:
      + (greenfield) call `Delete` in case the bucket was created (want idempotency for next try)
      + call `Provision` or `Grant` again
+ detects OBC update events: (post phase-0)
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + ensure the expected OB, Secret and ConfigMap are present
  + if all present:
    + update OBC status to "Bound"
  + sync the OB's status to match the OBC's status
+ detects OBC delete events:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + invoke the `Delete` method when the reclaim policy is "delete"
  + invoke the `Suspend` method when the reclaim policy is "retain"
  + delete the related Secret, ConfigMap and the OB (in that order)

#### OB Watches (post phase-0)
There is a single, central controller, separate from provisioners, that watches all OBs.
The main (only?) purpose of an OB watch is to detect and handle orphaned OBs -- see above.
The OB controller runs separately from provisioners and thus requires an extra setup step.

The OB watch performs the following:
+ detects a new OB:
  + ensures a matching OBC exists
    + if yes, sets status to "pending"
    + if no, this OB is orphaned so delete it and associated Secet and ConfigMap
+ detects OB update events:
  + if status == "released" then delete OB (and ConfigMap and/or Secret if needed)
+ detects OB delete events (but deletes events can be lost)
  + if the associated OBC is present the OB's Status is set to "released" (the OBC is orphaned but **not** deleted)
  + if the associated Secret and/or ConfigMap are present they are deleted
+ periodically syncs all OBs:
  + fetch all OBs
  + add OBs to the work queue so that an Add event is triggered

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
  #SSL: true | false [6]       # post phase-0, if at all
  #cannedBucketACL: [7]        # post phase-0, if at all
  #versioned: true | false [8] # post phase-0, if at all
  additionalConfig: [9]
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
1. SSL defines whether the connection to the bucket requires SSL authentication.
1. predefined bucket ACLs:
{"BucketCannedACLPrivate", "BucketCannedACLPublicRead", "BucketCannedACLPublicReadWrite", "BucketCannedACLAuthenticatedRead".
1. versioned determines if versioning is enabled.
1. additionalConfig gives providers a location to set proprietary config values (tenant, namespace...).
The value is a list of 1 or more key-value pairs.

### OBC Custom Resource (after updated by lib)
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
spec:
  ... 
  bucketName: photo-booth-62PrQ [1]
  objectBucketRef: objectReference{} [2]
  configMapRef: objectReference{} [3]
  secretRef: objectReference{} [4]
status:
  phase: {"pending", "bound", "released", "failed"}  [5]
```
1. the generated, unique bucket name for the new bucket.
1. objectReference to the generated OB.
1. objectReference to the generated ConfigMap.
1. objectReference to the generated Secret.
1. phases of bucket creation, mutually exclusive:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _released_: the OB has been deleted, leaving the OBC unclaimed but unavailable.

### Generated Secret (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: MY-BUCKET-1 [1]
  namespace: OBC-NAMESPACE [2]
  labels:
    objectbucket.io/PROVISIONER-NAME: [3]
  finalizers: [4]
  - objectbucket.io/finalizer
  ownerReferences:
  - name: MY-BUCKET-1 [5]
    ...
type: Opaque
data:
  ACCESS_KEY_ID: BASE64_ENCODED-1
  SECRET_ACCESS_KEY: BASE64_ENCODED-2
  ... [6]
```
1. same name as the OBC. Unique since the secret is in the same namespace as the OBC.
1. namespce of the originating OBC.
1. label may be used to associate all artifacts under a particular provisioner.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the Secret.
1. ownerReference makes this secret a child of the originating OBC for clean up purposes.
1. ACCESS_KEY_ID and SECRET_ACCESS_KEY are the only secret keys defined by the library.
Provisioners are able to cause the lib to create additional keys by returning  the `AdditionalConfig` field.
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
  labels:
    objectbucket.io/PROVISIONER-NAME: [3]
  finalizers: [4]
  - objectbucket.io/finalizer
  ownerReferences: [5]
  - name: MY-BUCKET-1
    ...
data: 
  BUCKET_HOST: http://MY-STORE-URL [6]
  BUCKET_PORT: 80 [7]
  BUCKET_NAME: MY-BUCKET-1 [8]
  BUCKET_REGION: us-west-1
  ... [9]
```
1. same name as the OBC. Unique since the configMap is in the same namespace as the OBC.
1. determined by the namespace of the ObjectBucketClaim.
1. label here associates all artifacts under a spoecific provisioner.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the ConfigMap.
1. ownerReference sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap.
1. host URL.
1. host port.
1. unique bucket name.
1. the above data keys are defined by the library.
Provisioners are able to cause the lib to create additional data keys by returning the `AdditionalConfig` field.

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
metadata:
  name: OBC-NAMESPACE-MY-BUCKET-1 [1]
  finalizers: [2]
  - objectbucket.io/finalizer
  labels:
    objectbucket.io/PROVISIONER-NAME: [3]
spec:
  objectBucketSource: [4]
    provider: ceph.rook.io/objectTOT
  storageClassName: OBCs-SC-NAME [5]  # TODO: or StorageClassRef and thus no need for separate reclaimPolicy?
  claimRef: objectreference [6]
  reclaimPolicy: {"Delete", "Retain"} [7]
status:
  phase: {"pending", "bound", "released", "failed"} [8]
```
1. name consists of the OBC's namespace + "-" + the OBC's metadata.Name (must be unique).
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of an OB.
1. label here associates all artifacts under the particular provisioner.
1. objectBucketSource is a struct containing metadata of the object store provider.
1. name of the storage class, referenced by the OBC, containing the provisioner and object store service name.
1. objectReference to the associated OBC.
1. reclaim policy from the Storge Class referenced in the OBC.
1. phase is the current state of the ObjectBucket:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _released_: the OBC has been deleted, leaving the OB unclaimed.

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
Unlike greenfield provisioning, the brownfield bucket name appears in the storge class, not the OBC.
1. each provisioner decides how to treat the _reclaimPolicy_ when an OBC is deleted. Supported values are:
+ _Delete_ = (typically) physically delete the bucket.
Depending on new vs. existing bucket, the provisioner's `Delete` or `Revoke` methods are called.
+ _Retain_ = (typically) do not physically delete the bucket.
Depending on the provisioner, various clean up steps can be performed, such as deleting users, revoking credentials, etc.
Depending on new vs. existing bucket, the provisioner's `Suspend` or `Revoke` methods are called.
+ _Erase_ = keep the bucket but remove all of its objects and (typically) perform similar cleanup steps as above. 
This is useful since bucket names generally must be unique across the entire object store, erasing a bucket removes content but preserves its (unique) name.
Depending on new vs. existing bucket, the provisioner's `Erase` or `Revoke` methods are called.

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

### Interfaces
#### Provision, Delete, Suspend, Erase, Grant, Revoke
The bucket provisioning library defines several interfaces which all provsioners must support.

TODO: for now, since interface and lib structs are changing no further info is provided in this doc.

### Future Considerations

(TBD)
