## Generic Bucket Provisioning
Kubernetes natively supports dynamic provisioning for many types of file and block storage, but lacks support for object bucket provisioning. 
This repo is a placeholder for an object store bucket provisioning library, very similar to the Kubernetes [sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go) library.

### Table Of Contents
1. [Assumptions](#assumptions)
1. [Design](#design)
1. [Binding](#binding)
1. [Bucket Deletion](#bucket-deletion)
1. [Bucket Sharing](#bucket-sharing)
1. [Quota](#quota)
1. [Watches](#watches)
1. [Limitations](#limitations)
1. [API Specifications](#api-specifications)
1. [Interfaces](#interfaces)

### Assumptions
1. The object store is represented by a Kubernetes service.
1. _Brownfield_, meaning existing buckets, is not supported (yet). _New_, dynamic bucket provisioning is the focus of this proposal.

### Design
The time has come where we can support a bucket provisioning API similar to that used for Persistent Volumes.
We propose two new Custom Resources to abstract an object store bucket and a claim/request for such a bucket.
It's important to keep in mind that this proposal  only defines bucket and bucket claim APIs and related library code.
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

### Binding
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. generation of a random bucket name when requested (performed by bucket lib).
1. the creation of the physical bucket with owner credentials (performed by provisioner).
1. the creation of a Secret, based on the provisioner's returned credentials, residing in the OBC's namespace (performed by bucket lib).
1. the creation of a ConfigMap, based on the provisioner's returned OB, residing in the OBC's namespace (performed by bucket lib).

`Bound` is one of the supported phases of an OB and an OBC.
`Bound` indicates that a bucket and all related artifacts have been created on behalf of the OBC. Once a bucket claim is bound the app pod can run, meaning the Secret (containing access credentials) and the ConfigMap (containing the bucket endpoint) are mounted and consumable by the pod.

**Note:** bucket provisioners that wish to prevent the OBC author from creating buckets outside of the Kubernetes cluster should return credentials lacking bucket CREATE access.

### Bucket Deletion
When an OBC is deleted the `Delete()` method is always called regardless of the OB's _reclaimPolicy_.
This differs from the Kubernetes external lib implementation which only invokes `Delete()` when the _reclaimPolicy_ == "Delete".
The reason to always call `Delete()`is so that provisioners can perform any needed bucket cleanup, even when the storage class dictates that the underlying bucket should be retained.
For example, ACLs and related user cleanup could be done if desired by the provisioner.
But it also places an extra burden on provisioners to support the _reclaimPolicy_ (which resides in the OB).

The bucket library always deletes all generated artifacts upon an OBC delete.
It is possible that an OBC delete event is missed and therefore an OB remains but its associated OBC has been deleted.
This scenario is handled by the OB watch which will delete orphaned OBs (and Secrets and ConfigMaps, if needed).
However, since there are no watches for Secrets and ConfigMaps, it could be possible for an OBC and OB to be deleted but not the related Secret and/or ConfigMap.
This scenario is not expected since the library will delete the Secret and ConfigMap before deleting the OB.

To reduce the chances of an admin deleting a _bound_ OB, and/or Secret and/or ConfigMap, a finalizer is added to these resources. 
The library's OBC controller will remove the finalizers when an OBC is deleted.

**Note:** the bucket library has no mechanism to prevent an OBC from being deleted when one or more pods indirectly reference the OBC via the Secret and ConfigMap.
This concept came late for PVCs, see [merged pr](https://github.com/kubernetes/community/pull/1174/files), and may be even more difficult to implement for OBCs.

### Bucket Sharing
A bucket can be shared, at least within the same namespace.
The reason for this is that the app pods never reference the OBC (or OB) directly, but instead consume a Secret and ConfigMap in order to access the bucket.
Since more than one pod can ingest the same Secert and ConfigMap, a bucket can be shared. _TODO: verify._

### Quota
S3 bucket size cannot be specified; however, bucket size can be monitored in S3.
The number of buckets can be controlled by a resource quota once [this k8s pr](https://github.com/kubernetes/kubernetes/pull/72384) is merged.
Until then, Resource Quotas cannot yet be defined for CRDs and, thus, there is no quota on the number of buckets.

### Watches
The bucket provisioning library provides watches for OBCs across all namespaces, and for OBs.
Each binary importing the lib is performing the same watches; however, the OBC watch quickly skips OBCs that do not target the specific provisioner.
On the other hand, all provisioners watch all OBs. 

The OBC watch performs the following:
+ detects a new OBC:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + generate random name if requested
  + invokes the `Provision()` method for the provisioner defined in the OBC's storage class
  + if the provisioning is successful:
    + create a global OB which references the OBC and storage class and contains store-specific bucket info
    + create a Secret, in the namespace as the OBC, containing the bucket credentials returned by the provisioner
    + create the ConfigMap, in the namespace as the OBC, containing the bucket's endpoint info
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

The OB watch performs the following:
+ detects a new OB:
  + ensures a matching OBC exists
    + if yes, sets status to "pending"
    + if no, sets status to "released" since this OB is orphaned
+ detects OB update events:
  + if status == "released" then delete OB (and ConfigMap and/or Secret if needed)
+ ignores OB delete events (deletes events can be lost)

### Limitations
This proposal differs from the Kubernetes external provisioner lib in that there is no centralized, _core_ bucket/claim controller to handle missed events by performing periodic syncs.
For example, in the bucket lib, each provisioner watches OBs and updates orphaned OBs when its OBC is not found.
With an understanding of the Kubernetes approach, it is reasonable to suggest that we also need a centralized
bucket controller in addition to/or in lieu of the library.
However, the cost to the cluster of each provisioner performing OB watches is mitigated by:
+  OBs, OBCs and associated Storage Classes being cached for fast access, bypassing the API,
+  the number of bucket provisioners per cluster is anticipated as being relatively small.

There is an edge case where if only a single provisioner is running, an OBC is deleted, the provisioner dies before deleting the OB, and **no** provisioner is run again, then that OB remains orphaned with no change to its status.
If something similar happened in Kubernetes the central controller would sync and detect the orphaned OB.
A simple solution is to run each provisioner in a Deployment so that a provisioner is always running.
When a provisioner restarts it will fetch all OBs and thus detect this orphan case.

Lastly, if OB watches (which don't skip out early like OBC watches) are too resource hungry then a possible
solution could be to use [_leader election_](https://github.com/kubernetes/client-go/blob/master/tools/leaderelection/example/main.go) when more than one provisioner is running.
The "leader" provisioner will watch OBs (in addition to OBCs) while the non-leaders only watch OBCs.

## API Specifications

### OBC Custom Resource (User Defined)
```yaml
apiVersion: objectbucket.s3.io/v1alpha1
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
1. additionalConfig gives non-AWS S3 providers a location to set proprietary config values (tenant, namespace...).
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
  S3_BUCKET_HOST: http://MY-STORE-URL [6]
  S3_BUCKET_PORT: 80 [7]
  S3_BUCKET_NAME: MY-BUCKET-1 [8]
  S3_BUCKET_URL: http://MY-STORE-URL/MY_BUCKET_1:80 [9]
```
1. same name as the OBC. Unique since the configMap is in the same namespace as the OBC.
1. determined by the namespace of the ObjectBucketClaim.
1. label here associates all artifacts under a spoecific provisioner.
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of the ConfigMap.
1. ownerReference sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap.
1. host URL.
1. host port.
1. unique bucket name.
1. full URL endpoint, aware of SSL flag and port.

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
    envFrom:
    - configMapRef:
        name: MY-BUCKET-1 [1]
    - secretRef:
        name: MY-BUCKET-1 [2]
```
1. makes available to the pod as env variables: S3_BUCKET_HOST, S3_BUCKET_PORT, S3_BUCKET_NAME, S3_BUCKET_URL
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
    provider: ceph.rook.io/object
  storageClassName: OBCs-STORAGE-CLASS [5]
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
parameters:
  objectStoreRef: [3]
    serviceName: MY-STORE
    serviceNamespace: MY-STORE-NAMESPACE
    region: LOCATION [4]
  secretRef: [5]
    name: OBJECT-STORE-ADMIN-SECRET-NAME
    namespace: OBJECT-STORE-ADMIN-SECRET-NAMESPACE
```
1. (optional) the label here associates this StorageClass to a specific provisioner.  
1. provisioner responsible to handling OBCs referencing this StorageClass.
1. objectStore used by the operator to derive the object store Service name.
1. region is optional and defines a region of the object store.
1. an optional admin-level secret containing the provisioner's key-pairs to be used for bucket creation.

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
	Provision(ObjectBucketOptions) (*v1alpha1.ObjectBucket, *auth.S3AccessKeys, error)
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

##### S3AccessKeys
```golang
type S3AccessKeys struct {
	AccessKey, SecretKey string
}
```

### Notes/Future Considerations

+ Brownfield - what if user creates a namespaced OB. The OB's storage class still defines the store name/ns and could
be the same SC used in an OBC, but the provisioner is ignored. Namespaced OBs never have OBCs and thus are never
considered orphaned. Since no provisoner is needed we'd have to have some other controller running that watched OBs.

+ Interfaces - we could define 2 additional, optional interface types. A `Credential()` method accepts an OB and returns a credential struct. A `Bind()` method accepts an OB and credentials and binds the bucket to the creds.

