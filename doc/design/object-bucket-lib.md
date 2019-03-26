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

### Non-Goals
+ Update Kubernetes PVC-PV API to support object buckets.
+ Handle the small percentage of apps that will not be portable due to use of non-compatible object-store features.
For example, an app that uses a feature in object-store-1 that is not provided in object-store-2, and the app now is tied to an object-store-2 endpoint.

### Assumptions
1. _Brownfield_, meaning existing buckets, is not supported.
Only **new** dynamic bucket provisioning is addressed by this proposal, thus there is no support for _best-match_ binding, like Kubernetes has for PVs <-> PVCs.
1. Apps need to be designed for object-store portability.
Just like there can be portability issues when an app exploits specialized features of a file system, an app accessing buckets, where portability matters, must be designed for that purpose.

### Design
We propose two new Custom Resources to abstract an object store bucket and a claim/request for such a bucket.
It's important to keep in mind that this proposal only defines bucket and bucket claim APIs and related library code.
The lib ensures that  the _contract_ made to app developers regarding the artifacts of bucket creation is guaranteed.
The actual creation of physical buckets belongs to each object store provisioner.
The bucket library handles watches on bucket claims and the (generated) bucket objects, reconciles state-of-the-world, creates the artifacts (Secret, ConfigMap) consumed by app pods, and deletes resources generated on behalf of the claim.

An `ObjectBucketClaim` (OBC) is similar in usage to a Persistent Volume Claim and an `ObjectBucket` (OB) is the Persistent Volume equivalent. 
An OBC is namespaced and references a storage class which defines the object store and provisioner.
An OB is non-namespaced (global), typically not visible to end users, and will contain info pertinent to the provisioned bucket.
Like PVs, there is a 1:1 binding of an OBC to an OB.
Bucket binding refers to the actual bucket being created by the underlying object store provider, and the generation of artifacts which will be consumed by application pods.
The details of the object store (ceph, minio, cloud, on-prem) are not visible to the app pod.
The same app can consume AWS S3 in the cloud or Ceph-RGW on-prem with no changes.

As is true for dynamic PV provisioning, a bucket provisioner needs to be running for each object store supported by the Kubernetes cluster.
For example, if the underlying object store is AWS S3, the developer will create an OBC, referencing
a Storage Class which references the S3 store.
The cluster has the S3 provisioner running which is watching (via the bucket lib) for OBCs that it knows how to handle, while
other OBCs are ignored.
Additionally, the same cluster can have a rook-ceph RGW provisioner running which also watches OBCs (again via the lib).
Like the S3 proivisioner, it only handles OBCs that it knows how to provision and skips the rest.
In this proposal, the bucket provisioners will be simple-to-write binaries because the bucket provisioning lib
handles the bulk of the work.
Each provisioner is only responsible for writing `Provision()` and `Delete()`functions and a short `main()` function.

The `Provision()` and `Delete()`functions are interfaces defined in the bucket library.
To provision a bucket, all provisioners are required to return an OB struct (which is used to construct the ConfigMap) and the bucket credentials (which are used to create the Secret).
The Secret and ConfigMap have deterministic names, namespaces, and property keys.
An app pod consuming a bucket need only be aware of the Secret name and keys, and the ConfigMap name and fields.
The app pod will not run until the bucket has been provisioned and can be accessed.
This is true even if the pod is created prior to the OBC.

**Note:** even though the PV-PVC design supports static provisioning, only dynamic provisioning is supported by the bucket lib at this time.

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
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. generation of a random bucket name when requested (performed by bucket lib).
1. the creation of the physical bucket with owner credentials (performed by provisioner).
1. the creation of a Secret, based on the provisioner's returned credentials, residing in the OBC's namespace (performed by bucket lib).
1. the creation of a ConfigMap, based on the provisioner's returned OB, residing in the OBC's namespace (performed by bucket lib).

`Bound` is one of the supported phases of an OB and an OBC.
`Bound` indicates that a bucket and all related artifacts have been created on behalf of the OBC. Once a bucket claim is bound the app pod can run, meaning the Secret (containing access credentials) and the ConfigMap (containing the bucket endpoint) are mounted and consumable by the pod.

### Bucket Deletion
When an OBC is deleted the provisioner's`Delete()` method is always called regardless of the OB's _reclaimPolicy_.
This differs from the Kubernetes external lib implementation which only invokes `Delete()` when the _reclaimPolicy_ == "Delete".
The reason to always call `Delete()`is so that provisioners can perform any bucket cleanup, even when the storage class dictates that the underlying bucket should be retained.
For example, ACLs and related user cleanup could be done if desired by the provisioner.
But it also places an extra burden on provisioners to support the _reclaimPolicy_ (resides in the OB).

The bucket library always attempts to delete all generated artifacts upon an OBC delete.
The implementation of Kubernetes _informers_ recognizes that sometimes Delete events are missed.
Controllers, therefore, need to be robust enough to infer deletes via other mechanisms, such as Status.
For example, an OBC can be deleted from the cluster, but the delete event is missed and thus the controller doesn't know the delete occurred.
In this scenario, the associated OB (and possibly the Secret and/or ConfigMap) remain, resulting in an _orphaned_ OB.

#### Orphaned Object Buckets (post phase-0)
An orphaned OB is an OB with no matching OBC, and thus the state of the cluster is inconsistent.
The solution is to delete orphaned OBs, but how are they detected since OBC watches are triggered by OBC events and there are no "events" associated with orphaned OBs.
The solution is to run a centralized, provisioner-agnostic OB controller that watches all OBs, and detects and deletes orphans.

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
Since more than one pod can ingest the same Secert and ConfigMap, a bucket can be shared. _TODO: verify._

### Quota
Bucket size generally cannot be specified; however, the current size of a bucke can usually be monitored.
The number of buckets can be controlled by a resource quota once [this k8s pr](https://github.com/kubernetes/kubernetes/pull/72384) is merged.
Until then, Resource Quotas cannot yet be defined for CRDs and, thus, there is no quota on the number of buckets.

### Watches

#### OBC Watches
All provisioners importing the bucket library watch OBCs across all namespaces.
Each binary importing the lib performs the same OBC watch.
OBCs that match the provisioner are further processed and OBCs not matching are quickly skipped.

The OBC watch performs the following:
+ detects a new OBC:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + generate random name if requested
  + invokes the `Provision()` method for the provisioner defined in the OBC's storage class
  + if the provisioning is successful:
    + create a global OB which references the OBC and storage class and contains store-specific bucket info
    + create a Secret, in the namespace as the OBC, containing the bucket credentials returned by the provisioner
    + create the ConfigMap, in the namespace as the OBC, containing the bucket's endpoint info
    + in the above order!
  + if the provisioner returns an error:
    + retry:
      + call `Delete()` in case the bucket was created (want idempotency for next try)
      + call `Provision()`
+ detects OBC update events:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + ensure the expected OB, Secret and ConfigMap are present
  + if all present:
    + update OBC status to "Bound"
  + sync the OB's status to match the OBC's status
+ detects OBC delete events:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + invoke the `Delete()` method for the provisioner defined in the OBC's storage class
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
  SSL: true | false [6]
  cannedBucketACL: [7]
  versioned: true | false [8]
  additionalConfig: [9]
    ANY_KEY: VALUE ...
```
1. name of the ObjectBucketClaim. This name becomes the Secret and ConfigMap names.
1. namespace of the ObjectBucketClaim, which is also the namespace of the ConfigMap and Secret.
1. name of the bucket. If used then `generateBucketName` is ignored. **Not** recommended
for new buckets -- expected to be used for brownfield buckets. Bucket names must be unique within
an object store, but an object store can store buckets for OBCs across multiple namespaces.
1. if used then `bucketName` must be empty. The value here is the prefix in a random name
and `bucketName` will be set to this generated name.
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
1. the generated, unique bucket name for the new bucket (standard Kubernetes generated name).
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
data: [6]
  ACCESS_KEY_ID: BASE64_ENCODED-1
  SECRET_ACCESS_KEY: BASE64_ENCODED-2
```
1. same name as the OBC. Unique since the secret is in the same namespace as the OBC.
1. namespce of the originating OBC.
1. label may be used to associate all artifacts under a paeticular provisioner.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the Secret.
1. ownerReference makes this secret a child of the originating OBC for clean up purposes.
1. **Note:** the library will create the Secret using `stringData:` and let the Secret API base64 encode the values.
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
```
1. same name as the OBC. Unique since the configMap is in the same namespace as the OBC.
1. determined by the namespace of the ObjectBucketClaim.
1. label here associates all artifacts under a spoecific provisioner.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the ConfigMap.
1. ownerReference sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap.
1. host URL.
1. host port.
1. unique bucket name.

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

### StorageClass (sample for rook-ceph-rgw provider)
```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: SOME-OBJECT-STORE
  labels: 
    ceph.rook.io/object: [1]
provisioner: rgw-ceph-rook.io [2]
parameters: [3]
  ...
```
1. (optional) the label here associates this StorageClass to a specific provisioner.  
1. provisioner responsible to handling OBCs referencing this StorageClass.
1. **all** parameter keys and values are specific to a provisioner, are optional, and are not validated by the StorageClass API.
Fields to consider are object-store endpoint, version, possibly a secretRef containing info about credential for new bucket owners, etc.

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
#### Provision() and Delete()
The bucket provisioning library defines two interfaces which all provsioners must support.
```golang
// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(ObjectBucketOptions) (*v1alpha1.ObjectBucket, *auth.AccessKeys, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(claim *v1alpha1.ObjectBucketClaim) error
}
```
##### ObjectBucketOptions
```golang
type ObjectBucketOptions struct {
	// Reclamation policy for a object volume
	ReclaimPolicy v1.PersistentVolumeReclaimPolicy
	// Suggested bucket name. Has been randomized if `generateBucketName` was defined.
	BucketName string
	// OBC is reference to the claim that lead to provisioning of a new bucket.
	OBC *v1alpha1.ObjectBucketClaim
	// Bucekt provisioning parameters from StorageClass
	Parameters map[string]string
}
```

##### ObjectBucketClaim
```golang
type ObjectBucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketClaimSpec   `json:"spec,omitempty"`
	Status ObjectBucketClaimStatus `json:"status,omitempty"`
}
```
```golang
type ObjectBucketClaimSpec struct {
	StorageClass string
}
```

##### ObjectBucket
```golang
type ObjectBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketSpec   `json:"spec,omitempty"`
	Status ObjectBucketStatus `json:"status,omitempty"`
}
```
```golang
type ObjectBucketSpec struct {
	// BucketName the base name of the bucket
	BucketName string `json:"bucketName"`
	// Host the host URL of the object store with
	Host string `json:"host"`
	// Region the region of the bucket within an object store
	Region string `json:"region"`
	// Port the insecure port number of the object store, if it exists
	Port int `json:"port"`
	// SecurePort the secure port number of the object store, if it exists
	SecurePort int `json:"securePort"`
	// SSL true if the connection is secured with SSL, false if it is not.
	SSL bool `json:"ssl"`

	// Versioned true if the object store support versioned buckets, false if not
	Versioned bool `json:"versioned,omitempty"`
}
```

##### AccessKeys
```golang
type AccessKeys struct {
	AccessKey, SecretKey string
}
```

### Future Considerations

+ Brownfield - the use case is narrower than general brownfield.
Namely, it's
desired that new credentials are generated for each existing bucket and the lib will wrap these credentials in a secret consumed by the app pod.
If new credentials are not needed then there is really no need for the lib to be involved.
We could add a `CreateCredentials()` interface so that object store provisioners can return brownfield bucket access credentials, which the lib can convert into a secret.
A confimap could also be generated by the lib.

+ If the use case of many app pods consuming the same secret/configMaps becomes important, we could support field mapping in the OBC. In other words, the OBC could define the names of the keys in the secret and data field names in the configMap such that the pod specs wouldn't need to define each env variable separately.

+ The configmap and secret can be more flexible by the adition of `AdditionalConfig` fields defined in the Provision() returned Connection struct.
The idea is that providers can define their own key-values which are exposed to the app pod via the secret and configmap.
