package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/reconciler/util"
	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
	RetryBackoff  int
}

func NewObjectBucketClaimReconciler(c client.Client, name string, provisioner provisioner.Provisioner, options Options) *objectBucketClaimReconciler {
	if options.RetryInterval < util.DefaultRetryBaseInterval {
		options.RetryInterval = util.DefaultRetryBaseInterval
	}
	if options.RetryTimeout < util.DefaultRetryTimeout {
		options.RetryTimeout = util.DefaultRetryTimeout
	}
	if options.RetryBackoff < util.DefaultRetryBackOff {
		options.RetryBackoff = util.DefaultRetryBackOff
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

	className, err := util.ExtractClaimClass(obc)
	if err != nil {
		return handleErr("%v", err)
	}

	storageClass, err := util.GetStorageClassByName(className, r.client)
	if err != nil && storageClass == nil {
		return handleErr("unable to get storageClass %q of claim %q: %v", className, obc.Name, err)
	}

	if !r.shouldProvision(storageClass) {
		return handleErr("claim failed shouldProvision check")
	}

	reclaimPolicy, err := util.TranslateReclaimPolicy(*storageClass.ReclaimPolicy)
	if err != nil {
		return handleErr("error translating core.PersistentVolumeReclaimPolicy %q to v1alpha1.ReclaimPolicy: %v", storageClass.ReclaimPolicy, err)
	}

	options := &provisioner.BucketOptions{
		ReclaimPolicy:     reclaimPolicy,
		ObjectBucketName:  obc.Namespace + "-" + obc.Name,
		BucketName:        util.GenerateBucketName(obc.Spec.BucketName),
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

	if err = util.CreateUntilDefaultTimeout(ob, r.client); err != nil {
		return handleErr("unable to create ObjectBucket %q: %v", ob.Name, err)
	}

	secret := util.NewCredentailsSecret(obc, s3keys)
	if err = util.CreateUntilDefaultTimeout(secret, r.client); err != nil {
		return handleErr("unable to create Secret %q: %v", secret.Name, err)
	}

	bucketConfigMap := util.NewBucketConfigMap(ob, obc)
	if err = util.CreateUntilDefaultTimeout(bucketConfigMap, r.client); err != nil {
		return handleErr("unable to create ConfigMap %q, ")
	}

	return reconcile.Result{}, nil
}

// A simplistic check on whether this obc is a concern for this provisioner.  Down the road, this will perform a broader
// set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(class *storagev1.StorageClass) bool {
	return class.Provisioner == r.provisionerName
}
