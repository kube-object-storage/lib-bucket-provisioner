## Generic Bucket Provisioning
Kubernetes natively supports dynamic provisioning for many types of file and
block storage, but lacks support for object bucket provisioning. 
This repo is a temporary placeholder for an object store bucket provisioning library,
very similar to the Kubernetes 
[sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go)
library. The goal is to eventually move this repo to a Kubernetes repo within _sig-storage/_.

### Assumptions
1. The object store is represented by a Kubernetes service.
1. _Brownfield_, meaning existing buckets, is not supported (yet). _New_, dynamic
bucket provisioning is the focus of this proposal.

### Design
The time has come where we can support a bucket provisioning API similar to that used for
Persistent Volumes. We propose two new Custom Resources to abstract an object store bucket
and a claim/request for such a bucket.  It's important to keep in mind that this proposal 
only defines bucket and bucket claim APIs and related library code. The lib ensures that 
the _contract_ made to app developers regarding the artifacts of bucket creation is guaranteed.
The actual creation of physical buckets belongs to each object store provisioner.
The bucket library handles watches on bucket claims and the (generated) bucket objects, reconciles
state-of-the-world, creates the artifacts (Secret, ConfigMap) consumed by app pods, and
deletes resources generated on behalf of the claim.

An `ObjectBucketClaim` (OBC) is similar in usage to a Persistent Volume Claim and an `ObjectBucket`
(OB) is the Persistent Volume equivalent. 
An OBC is namespaced and references a storage class which defines the object store.
An OB is non-namespaced (global), typically not visible to end users, and will contain
info pertinent to the provisioned bucket. Like PVs, there is a 1:1 binding of an OBC to an OB.
Bucket binding refers to the actual bucket being created by the underlying object store provider,
and the generation of artifacts which will be consumed by application pods.
The details of the object store (ceph, minio, cloud, on-prem) are not visible to the app pod.
The same app can consume AWS S3 in the cloud or Ceph-RGW on-prem with no changes.

As is true for dynamic PV provisioning, a bucket provisioner needs to be running
for each object store supported by the Kubernetes cluster. For example, if the
underlying object store is AWS S3, the developer will create an OBC, referencing
a Storage Class which references the S3 store. The cluster has the S3 provisioner
running which is watching (via the bucket lib) for OBCs that it knows how to handle, while
other OBCs are ignored. Additionally, the same cluster can have a rook-ceph RGW provisioner
running which also watches OBCs (again via the lib). Like the S3 proivisioner, it only
handles OBCs that it knows how to provision and skips the rest. In this proposal, the
bucket provisioners will be simple-to-write binaries because the bucket provisioning lib
handles the bulk of the work. Each provisioner is only responsible for writing `Provision()`
and `Delete()`functions and a short `main()` function.

The `Provision()` and `Delete()`functions are interfaces defined in the bucket library.
To provision a bucket, all provisioners are required to return an OB struct (which is used to
construct the ConfigMap) and the bucket credentials (which are used to create the
Secret). The Secret and ConfigMap have deterministic names, namespaces, and property keys.
An app pod consuming a bucket need only be aware of the Secret name and keys, and the
ConfigMap name and fields. The app pod will not run until the bucket has been provisioned
and can be accessed. This is true even if the pod is created prior to the OBC.

**Note:** even though the PV-PVC design supports static provisioning, only
dynamic provisioning is supported by the bucket lib at this time.

### Binding
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. the creation of the physical bucket with owner credentials (performed
by each provisioner),
1. the creation of a Secret, based on the provisioner's returned credentials, residing
in the OBC's namespace (performed by the bucket library),
1. the creation of a ConfigMap which contains the endpoint of the bucket (performed
by the bucket library).

`Bound` is one of the supported phases of an OB and an OBC. `Bound` indicates that a
bucket and all related artifacts have been created on behalf of the OBC. Once a bucket claim
is bound the app pod can run, meaning the Secret (containing access credentials) and the
ConfigMap (containing the bucket endpoint) are mounted and consumable by the pod.

**Note:** bucket provisioners that wish to prevent the OBC author from creating
buckets outside of the Kubernetes cluster should return credentials lacking bucket
CREATE access.

### Bucket Deletion
Contsistent with PVCs, when an OBC is deleted **and** the OB's _reclaimPolicy_ is set
to "delete", the assoicated OB is also deleted. This is true regardless of the `Delete()`
implementation by the provisioner. For example, the provisioner could decide to not
delete the underlying bucket storage, but the OB will still be deleted by the library.
If the _reclaimPolicy_ is not set to "delete" then the provisioner's `Delete()` method is
not invoked and the associated OB is not deleted. In this case, the remaining OB's status will
be set to "retained", indicating that the OBC has been deleted but the bucket is still available.
Independent of the _reclaimPolicy_, the generated Secret and ConfigMap are always deleted
by the bucket lib when an OBC is deleted. 

To reduce the chances of an admin deleting a _bound_ OB, a finalizer is added to the OB. 
The library's OBC controller will remove the finalizer when an OBC is deleted. Additionally,
the lib's OB controller will detect bound OBs missing their OBC. The status in these OBs
will be set to "lost" to reflect that they are orphaned.

**Note:** the bucket library has no mechanism to prevent an OBC from being deleted when one or
more pods indirectly reference the OBC via the Secret and ConfigMap. This concept came late
for PVCs, see
[merged pr](https://github.com/kubernetes/community/pull/1174/files), and may be even more
difficult to implement for OBCs.

### Bucket Sharing
A bucket can be shared, at least within the same namespace. The reason for this is that
the app pods never reference the OBC (or OB) directly, but instead consume a Secret and
ConfigMap in order to access the bucket. Since more than one pod can ingest the same 
Secert and ConfigMap, a bucket can be shared. _TODO: verify._

### Quota
S3 bucket size cannot be specified; however, bucket size can be monitored in S3. The number
of buckets can be controlled by a resource quota once
[this k8s pr](https://github.com/kubernetes/kubernetes/pull/72384) is merged. Until then, 
Resource Quotas cannot yet be defined for CRDs and, thus, there is no quota on the 
number of buckets.

### Watches
The bucket provisioning library provides watches for OBCs across all namespaces, and for OBs.
Each binary importing the lib is performing the same watches; however, the OBC watch quickly
skips OBCs that do not target the specific provisioner. On the other hand, all provisioners
watch all OBs. 

The OBC watch performs the following:
+ detects a new OBC:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + invokes the `Provision()` method for the provisioner defined in the OBC's storage class
  + if the provisioning is successful:
    + creates a global OB which references the OBC and storage class and contains store-specific bucket info
    + creates a Secret, in the namespace as the OBC, containing the bucket credentials returned by the provisioner
    + creates the ConfigMap, in the namespace as the OBC, containing the bucket's endpoint info
+ detects OBC update events:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + ensures the expected OB, Secret and ConfigMap are present
  + syncs the OB's status to reflect the OBC's status
+ detects OBC delete events:
  + skip if the OBC's StorageClass' provisioner != the provisioner doing this watch
  + if the associated OB's _reclaimPolicy_ == "Delete":
    + invokes the `Delete()` method for the provisioner defined in the OBC's storage class
  + deletes the related Secret, ConfigMap (in the OBC's namespace), and OB

The OB watch performs the following:
+ detects a new OB:
  + ensures a matching OBC exists
    + if yes, sets status to "pending"
    + if no, sets status to "lost" since this OB is orphaned
+ detects OB update events:
  + same OBC check and status update as for _new_ OB
  + syncs the OBC's status to reflect the OB's status
+ detects and ignores OB delete events 

### Limitations
The nature of a Go library implies that there will be redundancy in the object bucket provisioners
and _fatness_ in the provisioner binaries. Cluster resource usage doesn't scale well as the
number of provisioners increases. And, fixing a bug in the library necessitates a recompile of all
provisioners, if they want the fix.

This proposal also differs from the Kubernetes external provisioner lib in that there is no centralized,
_core_ bucket/claim controller to handle missed events by performing periodic syncs. For example, in the
bucket lib, each provisioner watches OBs and updates orphaned OBs when its OBC is not found.
With an understanding of the Kubernetes approach, it is reasonable to suggest that we also need a centralized
bucket controller in addition to/or in lieu of the library. However, the cost to the cluster of each 
provisioner performing OB watches is mitigated by:
+  OBs, OBCs and associated Storage Classes being cached for fast access, bypassing the API,
+  the number of bucket provisioners per cluster is anticipated as being relatively small.

There is an edge case where if only a single provisioner is running, an OBC is deleted, the provisioner
dies before deleting the OB (and/or Secret, and/or ConfigMap), and **no** provisioner is run again, then
that OB remains orphaned with no change to its status.  If something similar happened in Kubernetes the
central controller would sync and detect the orphaned OB. A simple solution is to run each provisioner
in a Deployment so that a provisioner is always running. When a provisioner restarts it will fetch all OBs
and thus detect this orphan case.

Lastly, if OB watches (which don't skip out early like OBC watches) are too resource hungry then a possible
solution could be to use
[_leader election_](https://github.com/kubernetes/client-go/blob/master/tools/leaderelection/example/main.go)
when more than one provisioner is running. The "leader" provisioner will watch OBs (in addition to OBCs)
while the non-leaders only watch OBCs.

## API Specifications (subject to change)

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
  storageClassName: AN-OBJECT-STORE-STORAGE-CLASS [4]
  SSL: true | false [5]
  cannedBucketACL: [6]
  versioned: [7]
  additionalConfig: [8]
    ANY_KEY: VALUE ...
```
1. name of the ObjectBucketClaim. This name becomes the Secret and ConfigMap names.
1. namespace of the ObjectBucketClaim, which is also the namespace of the ConfigMap and Secret.
1. name of the bucket. If used then `generateBucketName` is ignored. **Not** recommended
for new buckets -- expected to be used for brownfield buckets. Bucket names must be unique within
an object store, but an object store can store buckets for OBCs across multiple namespaces.
1. if used then `bucketName` must be empty. The value here is the prefix in a random name
and `bucketName` will be set to this generated name.
1. storageClassName is used to target the desired Object Store. Used by the operator to get
the object-store service URL.
1. SSL defines whether the connection to the bucket requires SSL authentication.
1. predefined bucket ACL. Values:
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
  bucketName: photo-booth-7aB620PrQ123 [1]
  objectBucketName:  [2]
status:
  phase: {"pending", "bound", "lost"}  [3]
  objectBucketRef: objectReference{}  [4]
  configMapRef: objectReference{}  [5]
  secretRef: objectReference{}  [6]
```
1. the generated, unique bucket name for the new bucket.
1. the name of the OB bound to this OBC (may become input for brownfield)
1. phases of bucket creation, mutually exclusive:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _lost_: the OB has been deleted, leaving the OBC unclaimed but unavailable.
1. objectReference to the generated ConfigMap .
1. objectReference to the generated Secret.

### Generated Secret (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: MY-BUCKET-1 [1]
  namespace: OBC-NAMESPACE [2]
  labels:
    ceph.rook.io/object: [3]
  ownerReferences:
  - name: MY-BUCKET-1 [4]
    ...
data:
  ACCESS_KEY_ID: BASE64_ENCODED-1
  SECRET_ACCESS_KEY: BASE64_ENCODED-2
```
1. same name as the OBC. Unique since the secret is in the same namespace as the OBC.
1. namespce of the originating OBC.
1. (optional per provisioner) the label may be used to associate all artifacts under the Rook-Ceph object provisioner.
1. ownerReference makes this secret a child of the originating OBC for clean up purposes.

### Generated ConfigMap (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: MY-BUCKET-1 [1]
  namespace: OBC-NAMESPACE [2]
  labels:
    ceph.rook.io/object: [3]
  ownerReferences: [4]
  - name: MY-BUCKET-1
    ...
data: 
  S3_BUCKET_HOST: http://MY-STORE-URL [5]
  S3_BUCKET_PORT: 80 [6]
  S3_BUCKET_NAME: MY-BUCKET-1 [7]
  S3_BUCKET_SSL: no [8]
  S3_BUCKET_URL: http://MY-STORE-URL/MY_BUCKET_1:80 [9]
```
1. same name as the OBC. Unique since the configMap is in the same namespace as the OBC.
1. determined by the namespace of the ObjectBucketClaim.
1. (optional per provisioner) the label here associates all artifacts under the Rook-Ceph object provisioner
1. ownerReference sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap.
1. host URL.
1. host port.
1. unique bucket name.
1. boolean representing SSL connection.
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
    env:
      # Bucket credentials
      - name: MY_ACCESS_KEY_ID [1]
        valueFrom:
          secretKeyRef:
            name: MY-BUCKET-1 [2]
            key: ACCESS_KEY_ID [3]
      - name: MY_SECRET_KEY [1]
        valueFrom:
          secretKeyRef:
            name: MY-BUCKET-1 [2]
            key: SECRET_ACCESS_KEY [3]
      # Bucket endpoint data
      - name: BUCKET_URL [1]
          valueFrom:
            configMapKeyRef:
              name: MY-BUCKET-1 [4]
              key: S3_BUCKET_URL [5]
```
1. the name of the environment variable that will be referenced in the pod.
1. the name of the generated secret, which is the OBC name.
1. the key name defined in the secret.
1. the name of the generated configMap, which is the OBC name.
1. the key name defined in the configMap.

 ### Generated OB Custom Resource
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucket
metadata:
  name: OBC-NAMESPACE-MY-BUCKET-1 [1]
  finalizers: [2]
  - objectbucket.lib/ob-protection
  labels:
    ceph.rook.io/object: [3]
spec:
  objectBucketSource: [4]
    provider: ceph.rook.io/object
  storageClassName: OBCs-STORAGE-CLASS [5]
  claimRef: objectreference [6]
  reclaimPolicy: {"Delete", "Retain"} [7]
status:
  phase: {"pending", "bound", "lost"} [8]
```
1. name consists of the OBC's namespace + "-" + the OBC's metadata.Name (must be unique).
1. finalizers set and cleared by the lib's OBC controller. Prevents accidental deletion of an OBC.
1. (optional per provisioner) the label here associates all artifacts under the Rook-Ceph object provisioner.
1. objectBucketSource is a struct containing metadata of the object store provider.
1. name of the storage class, referenced by the OBC, containing the provisioner and object store service name.
1. objectReference to the associated OBC.
1. reclaim policy from the Storge Class referenced in the OBC.
1. phase is the current state of the ObjectBucket:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _lost_: the OBC has been deleted, leaving the OB unclaimed.


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
  objectStoreService: MY-STORE [3]
  objectStoreServiceNamespace: MY-STORE-NAMESPACE [4]
  region: LOCATION [5]
```
1. label `ceph.rook.io/object/claims` associates all artifacts under the ObjectBucketClaim operator. Defined in example StorageClass and set by cluster admin.  
1. provisioner responsible to handling OBCs referencing this StorageClass.
1. objectStore used by the operator to derive the object store Service name.
1. objectStore is namespace the namespace of the object store.
1. region is optional qnd defines a region of the object store.

## Bucket Methods and Structs
```golang
// Provisioner the interface to be implemented by users of this
// library and executed by the Reconciler
type Provisioner interface {
	// Provision should be implemented to handle bucket creation
	// for the target object store
	Provision(*v1alpha1.ObjectBucketClaim) (*v1alpha1.ObjectBucketClaim, *S3AccessKeys, error)
	// Delete should be implemented to handle bucket deletion
	// for the target object store
	Delete(claim *v1alpha1.ObjectBucketClaim) error
}
```

```golang
// ObjectBucket is the Schema for the objectbuckets API
// +k8s:openapi-gen=true
type ObjectBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketSpec   `json:"spec,omitempty"`
	Status ObjectBucketStatus `json:"status,omitempty"`
}
```

```golang
// ObjectBucketSpec defines the desired state of ObjectBucket.
// Fields defined here should be normal among all S3 providers.
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

```golang
// ObjectBucketStatus defines the observed state of ObjectBucket
type ObjectBucketStatus struct {
	Controller *v1.ObjectReference
}
```

```golang
// ObjectBucketClaim is the Schema for the objectbucketclaims API
// +k8s:openapi-gen=true
type ObjectBucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketClaimSpec   `json:"spec,omitempty"`
	Status ObjectBucketClaimStatus `json:"status,omitempty"`
}
```
```golang
// ObjectBucketClaimSpec defines the desired state of ObjectBucketClaim
type ObjectBucketClaimSpec struct {
	StorageClass string
}
```

```golang
// ObjectBucketClaimStatus defines the observed state of ObjectBucketClaim
type ObjectBucketClaimStatus struct {
	Phase           ObjectBucketClaimStatusPhase
	ObjectBucketRef *v1.ObjectReference
	ConfigMapRef    *v1.ObjectReference
	SecretRef       *v1.SecretReference
}
```

```golang
type ObjectBucketClaimStatusPhase string

const (
	ObjectBucketClaimStatusPhasePending = "pending"
	ObjectBucketClaimStatusPhaseBound   = "bound"
	ObjectBucketClaimStatusPhaseLost    = "lost"
)
```
