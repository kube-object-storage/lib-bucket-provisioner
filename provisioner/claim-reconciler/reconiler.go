package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/provisioner/provisioner"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strconv"
	"strings"
	"time"
)

type objectBucketClaimReconciler struct {
	client          client.Client
	provisionerName string
	provisioner     provisioner.Provisioner
	retryInterval   time.Duration
	retryTimeout    time.Duration
	retryBackoff    int
}

var _ reconcile.Reconciler = &objectBucketClaimReconciler{}

const (
	defaultRetryBaseInterval = time.Second * 10
	defaultRetryTimeout      = time.Second * 360
	defaultRetryBackOff      = 1

	finalizer = "objectbucket.io/finalizer"

	// Fields Names
	bucketName      = "S3_BUCKET_NAME"
	bucketHost      = "S3_BUCKET_HOST"
	bucketPort      = "S3_BUCKET_PORT"
	bucketAccessKey = "S3_BUCKET_ACCESS_KEY_ID"
	bucketSecretKey = "S3_BUCKET_SECRET_KEY"
	bucketURL       = "S3_BUCKET_URL"
)

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
	RetryBackoff  int
}

func NewObjectBucketClaimReconciler(c client.Client, name string, provisioner provisioner.Provisioner, options Options) *objectBucketClaimReconciler {
	if options.RetryInterval < defaultRetryBaseInterval {
		options.RetryInterval = defaultRetryBaseInterval
	}
	if options.RetryTimeout < defaultRetryTimeout {
		options.RetryTimeout = defaultRetryTimeout
	}
	if options.RetryBackoff < defaultRetryBackOff {
		options.RetryBackoff = defaultRetryBackOff
	}
	return &objectBucketClaimReconciler{
		client:          c,
		provisionerName: strings.ToLower(name),
		provisioner:     provisioner,
		retryInterval:   options.RetryInterval,
		retryTimeout:    options.RetryTimeout,
		retryBackoff:    options.RetryBackoff,
	}
}

// TODO this is the guts of our controller.
//   `request` is a 'namespace/name' object key.
func (r *objectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	handleErr := func(format string, a ...interface{}) (reconcile.Result, error) {
		return reconcile.Result{}, fmt.Errorf(format, a...)
	}

	// /   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// /   ///   ///   ///   ///   ///   ///
	// This process should basically
	// 1. get the claim resource
	// 2. get the class for the claim
	// 3. verify the class.provisioner matches this provisioner name
	// 4. attempt to provision the bucket
	// 5. create the OB returned from Provision()
	// 6. create the secret and config map in the OBC namespace

	obc := &v1alpha1.ObjectBucketClaim{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, obc); err != nil {
		return handleErr("error getting claim %q: %v", obc.Name, err)
	}

	className, err := extractClaimClass(obc)
	if err != nil {
		return handleErr("%v", err)
	}

	storageClass, err := getStorageClassByName(className, r.client)
	if err != nil && storageClass == nil {
		return handleErr("unable to get storageClass %q of claim %q: %v", className, obc.Name, err)
	}

	if !r.shouldProvision(storageClass) {
		return handleErr("claim failed shouldProvision check")
	}

	reclaimPolicy, err := translateReclaimPolicy(*storageClass.ReclaimPolicy)
	if err != nil {
		return handleErr("error translating core.PersistentVolumeReclaimPolicy %q to v1alpha1.ReclaimPolicy: %v", storageClass.ReclaimPolicy, err)
	}

	options := &provisioner.BucketOptions{
		ReclaimPolicy:     reclaimPolicy,
		ObjectBucketName:  obc.Namespace + "-" + obc.Name,
		BucketName:        "", // TODO name generator function
		ObjectBucketClaim: obc,
		Parameters:        storageClass.Parameters,
	}

	// TODO a failure on any process below should call Delete()
	ob, s3keys, err := r.provisioner.Provision(options)
	if err != nil {
		return handleErr("%v", err)
	}

	if s3keys.AreEmpty() {
		msg := "s3 access key or secret key is nil"
		err = r.provisioner.Delete(ob)
		if err != nil {
			msg = fmt.Sprintf("%s, error deleting bucket: %v", msg, err)
		}
		return handleErr("%v", err)
	}

	if err = createUntilDefaultTimeout(ob, r.client); err != nil {
		return handleErr("unable to create ObjectBucket %q: %v", ob.Name, err)
	}

	secret := newCredentailsSecret(obc, s3keys)
	if err = createUntilDefaultTimeout(secret, r.client); err != nil {
		return handleErr("unable to create Secret %q: %v", secret.Name, err)
	}

	bucketConfigMap := newBucketConfigMap(ob, obc)
	if err = createUntilDefaultTimeout(bucketConfigMap, r.client); err != nil {
		return handleErr("unable to create ConfigMap %q, ")
	}

	return reconcile.Result{}, nil
}

// A simplistic check on whether this obc is a concern for this provisioner.  Down the road, this will perform a broader
// set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(class *storagev1.StorageClass) bool {
	return class.Provisioner == r.provisionerName
}

func getStorageClassByName(name string, c client.Client) (*storagev1.StorageClass, error) {
	sc := &storagev1.StorageClass{}
	err := c.Get(context.TODO(), client.ObjectKey{Name: name}, sc)
	if err != nil {
		return nil, fmt.Errorf("could not get storage class: %v", err)
	}
	return sc, nil
}

func newCredentailsSecret(obc *v1alpha1.ObjectBucketClaim, keys *provisioner.S3AccessKeys) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			GenerateName: obc.Name,
			Namespace:    obc.Namespace,
			Finalizers:   []string{finalizer},
		},
		StringData: map[string]string{
			bucketAccessKey: keys.AccessKey,
			bucketSecretKey: keys.SecretKey,
		},
	}
}

func newBucketConfigMap(ob *v1alpha1.ObjectBucket, obc *v1alpha1.ObjectBucketClaim) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name:      obc.Name,
			Namespace: obc.Namespace,
		},
		Data: map[string]string{
			bucketName: obc.Spec.BucketName,
			bucketHost: ob.Spec.BucketHost,
			bucketPort: strconv.Itoa(ob.Spec.BucketPort),
		},
	}
}

func createUntilDefaultTimeout(obj runtime.Object, c client.Client) error {
	return wait.PollImmediate(defaultRetryBaseInterval, defaultRetryTimeout, func() (done bool, err error) {
		err = c.Create(context.Background(), obj)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return false, err
		}
		return true, nil
	})
}

func extractClaimClass(obc *v1alpha1.ObjectBucketClaim) (string, error) {
	if obc.Spec.StorageClassName == "" {
		return "", fmt.Errorf("no class for claim %q", obc.Name)
	}
	return obc.Spec.StorageClassName, nil
}

func translateReclaimPolicy(rp corev1.PersistentVolumeReclaimPolicy) (v1alpha1.ReclaimPolicy, error) {
	switch v1alpha1.ReclaimPolicy(rp) {
	case v1alpha1.ReclaimPolicyDelete:
		return v1alpha1.ReclaimPolicyDelete, nil
	case v1alpha1.ReclaimPolicyRetain:
		return v1alpha1.ReclaimPolicyRetain, nil
	}
	return "", fmt.Errorf("unrecognized reclaim policy %q", rp)
}
