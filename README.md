## Generic Bucket Provisioning
Kubernetes natively supports dynamic provisioning for many types of file and
block storage, but lacks support for object bucket provisioning. 
This repo is a temporary placeholder for an object store bucket provisioning library,
very similar to the Kubernetes 
[sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/blob/master/controller/controller.go)
library. The goal is to eventually move this repo to a Kubernetes repo within _sig-storage/_.

### Assumptions
1. The object store is represented by a Kubernetes service.
1. _Brownfield_, meaning existing, legacy buckets, is not supported (yet). _New_, dynamic
bucket provisioning is the focus of this proposal.

### Design
The time has come where we can support a bucket provisioning API similar to that used for
Persistent Volumes. We propose two new Custom Resources to abstract an object store bucket
and a claim/request for such a bucket.  It's important to keep in mind that this proposal 
only defines bucket and bucket claim APIs and related library code. The lib ensures that 
the _contract_ made to app developers regarding the artifacts of bucket creation (Secrets,
ConfigMaps) are guaranteed. The actual creation of buckets belongs to each object store
provisioner. However, as is true for dynamic Persistent Volume provisioning, each bucket
provisioner needs to adhere to the inferfaces defined in the bucket library.

An `ObjectBucketClaim` (OBC) is similar in usage to a Persistent Volume Claim
and an `ObjectBucket` (OB) is the Persistent Volume equivalent. 
Bucket binding refers to the actual bucket being created by the underlying object
store provider, and the generation of artifacts which will be consumed by application pods.
An OBC is namespaced and references a storage class which defines the object store. The
details of the object store (ceph, minio, cloud, on-prem) are not visible to the app pod.
The same app can consume AWS S3 in the cloud or Ceph-RGW on-prem with no changes.
An OB is non-namespaced (global), typically not visible to end users, and will contain
info pertinent to the provisioned bucket. Like PVs, there is a 1:1 binding of an OBC to an OB.

As is true for dynamic PV provisioning, a bucket provisioner needs to be running
for each object store supported by the Kubernetes cluster. For example, if the
underlying object store is AWS S3, the developer will create an OBC, referencing
a Storage Class which references the S3 store. The cluster has the S3 provisioner
running which is watching for OBCs that it knows how to handle. Other OBCs are ignored
by the S3 provisioner. Additionally, the same cluster can have a rook-ceph RGW
provisioner running which also watches OBCs. Like the S3 proivisioner, it only handles
OBCs that it knows how to provision and skips the rest. In this proposal, the bucket
provisioners will be simple-to-write operators because the bucket provisioning lib
handles the bulk of the work. Each provisioner is only responsible for writing
`Provision()` and `Delete()`functions (the _business logic_) and a short `main()`
function.

**Note:** even though the PV-PVC design supports static provisioning, only
dynamic provisioning is supported by the bucket lib.

The `Provision()` and `Delete()`functions are interfaces defined in the bucket library.
All bucket provisioners are required to return an OB struct (which is used to
construct the ConfigMap) and the bucket credentials (which are used to create the
Secret).The Secret and ConfigMap have deterministic names, namespaces, and property keys.
An app pod consuming a bucket need only be aware of the Secret name and keys, and the
ConfigMap name and fields. The app pod will not run until the bucket has been provisioned
and can be accessed. This is true even if the pod is created prior to the OBC.

### Binding
Bucket binding requires these steps before the bucket is accessible to an app pod:
1. the creation of the physical bucket with owner credentials. This step is required
by each object store provisioner,
1. the creation of a Secret, based on the provisioner's returned credentials, residing
in the OBC's namespace. This is done by the bucket library,
1. the creation of a ConfigMap which contains the endpoint of the bucket. Also done
by the bucket lib,

`Bound` is one of the supported phases of an OB and OBC. `Bound` indicates that a
bucket and all related artifacts have been created on behalf of the OBC.

**Note:** bucket provisioners that wish to prevent the OBC author from creating
buckets outside of the Kubernetes cluster should return credentials lacking
CREATE access.

### Bucket Deletion
Contsistent with PVCs, an OBC can be deleted but the underlying bucket is not removed due
to concerns with deleting objects that cannot be easily recovered. However, OBC deletion
triggers cleanup of the Kubernetes resources created on behalf of the bucket: the
Secret and ConfigMap. Since the physical bucket is not deleted neither is the OB,
which represents this bucket. The OB's status will indicate that the related OBC
has been deleted so that an admin has better visibility into buckets that are missing
their connection information.

The generated OB's `ownerReference` is set to the object store service, not to the OBC.
This allows an OBC to be deleted while preserving the OB. When the object store
service is deleted all OBs belonging to that object store will be automatically
deleted by Kubernetes due to the `ownerReference` setting.

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

### OBC Custom Resource (User Defined)
```yaml
apiVersion: objectbucket.s3.io/v1alpha1
kind: ObjectBucketClaim
metadata:
  name: MY-BUCKET-1 [1]
  namespace: USER-NAMESPACE [2]
spec:
  storageClassName: AN-OBJECT-STORE-STORAGE-CLASS [3]
  tenant: MY-TENANT [4]
```
1. name of the ObjectBucketClaim. This name becomes part of the bucket and ConfigMap names.
1. namespace of the ObjectBucketClaim. Determines the namespace of the ConfigMap and Secret.
Also becomes part of the unique bucket name.
1. storageClassName is used to target the desired Object Store. Used by the operator to get
the Object Store service URL.
1. tenant allows users to define a tenant in an object store in order to namespace their buckets
and access keys.

### OBC Custom Resource (Status Updated)
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucketClaim
...
status:
  phase: {"pending", "bound", "lost"}  [1]
  objectBucketRef: objectReference{}  [2]
  configMapRef: objectReference{}  [3]
  secretRef: objectReference{}  [4]
```
1 `phase` 3 possible phases of bucket creation, mutually exclusive:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _lost_: the OB has been deleted, leaving the OBC unclaimed but unavailable
    TODO: describe how user/admin deals with _lost_ OBs/OBCs
1. `objectBucketRef` is an objectReference to the bound ObjectBucket 
1. `configMapRef` is an objectReference to the generated ConfigMap 
1. `secretRef` is an objectReference to the generated Secret

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

 ### Generated OB Custom Resource
```yaml
apiVersion: objectbucket.io/v1alpha1
kind: ObjectBucket
metadata:
  name: object-bucket-claim-MY-BUCKET-1
  ownerReferences: [1]
  - name: CEPH-CLUSTER
    ...
  labels:
    ceph.rook.io/object: [2]
spec:
  objectBucketSource: [3]
    provider: ceph.rook.io/object
status:
  claimRef: objectreference [4]
  phase: {"pending", "bound", "lost"} [5]
```
1. `ownerReferences` marks the OB as a child of the object store. If the store is deleted, the bucket will be 
 automatically deleted
1. (optional per provisioner) The label here associates all artifacts under the Rook-Ceph object provisioner
1. `objectBucketSource` is a struct containing metadata of the object store provider
1. `claimRef` is an objectReference to the associated OBC
1. `phase` is the current state of the ObjectBucket:
    - _pending_: the operator is processing the request
    - _bound_: the operator finished processing the request and linked the OBC and OB
    - _lost_: the OBC has been deleted, leaving the OB unclaimed

### Generated Secret for User Access (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: object-bucket-claim-MY-BUCKET-1 [1]
  namespace: USER-NAMESPACE [2]
  labels:
    ceph.rook.io/object: [3]
  ownerReferences:
  - name: MY-BUCKET-1 [4]
    ...
data:
  ACCESS_KEY_ID: BASE64_ENCODED-1
  SECRET_ACCESS_KEY: BASE64_ENCODED-2
```
1. `name` is composed from the OBC's `metadata.name`
1. `namespce` is that of a originating OBC
1. (optional per provisioner) The label here associates all artifacts under the Rook-Ceph object provisioner
1. `ownerReference` makes this secret a child of the originating OBC for clean up purposes

### Generated ConfigMap (sample for rook-ceph provider)
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: rook-ceph-object-bucket-MY-BUCKET-1 [1]
  namespace: USER-NAMESPACE [2]
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
```
1. `name` composed from `rook-ceph-object-bucket-` and ObjectBucketClaim `metadata.name` value concatenated
1. `namespace` determined by the namespace of the ObjectBucketClaim
1. (optional per provisioner) The label here associates all artifacts under the Rook-Ceph object provisioner
1. `ownerReference` sets the ConfigMap as a child of the ObjectBucketClaim. Deletion of the ObjectBucketClaim causes the deletion of the ConfigMap
1. `S3_BUCKET_HOST` host URL
1. `S3_BUCKET_PORT` host port
1. `S3_BUCKET_NAME` bucket name
1. `S3_BUCKET_SSL` boolean representing SSL connection

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
1. Label `ceph.rook.io/object/claims` associates all artifacts under the ObjectBucketClaim operator.  Defined in example StorageClass and set by cluster admin.  
1. `provisioner` the provisioner responsible to handling OBCs referencing this StorageClass
1. `objectStore` used by the operator to derive the object store Service name.
1. `objectStoreNamespace` the namespace of the object store
1. `region` (optional) defines a region of the object store

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
