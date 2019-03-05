package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/reconciler/util"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
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

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

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

	err = r.handelProvision(options)
	if err != nil {
		return handleErr("failed Provisioning bucket %q for claim %q after %d attempts: %v", options.BucketName, options.ObjectBucketClaim.Namespace+"/"+options.ObjectBucketClaim.Name, err)
	}

	return reconcile.Result{}, nil
}

// A simplistic check on whether this obc is a concern for this provisioner.  Down the road, this will perform a broader
// set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(class *storagev1.StorageClass) bool {
	return class.Provisioner == r.provisionerName
}

func (r *objectBucketClaimReconciler) handelProvision(options *provisioner.BucketOptions) error {
	var (
		ob  *v1alpha1.ObjectBucket
		err error
	)
	defer func() {
		if err != nil {
			_ = r.provisioner.Delete(ob)
		}
	}()

	ob, s3keys, err := r.provisioner.Provision(options)
	if err != nil {
		return fmt.Errorf("%v", err)
	}

	if s3keys.AreEmpty() {
		return fmt.Errorf("got non-nil string(s) for access key id and/or secret key")
	}

	if err = util.CreateUntilDefaultTimeout(ob, r.client); err != nil {
		return fmt.Errorf("unable to create ObjectBucket %q: %v", ob.Name, err)
	}

	secret := util.NewCredentailsSecret(options.ObjectBucketClaim, s3keys)
	if err = util.CreateUntilDefaultTimeout(secret, r.client); err != nil {
		return fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}

	bucketConfigMap := util.NewBucketConfigMap(ob, options.ObjectBucketClaim)
	if err = util.CreateUntilDefaultTimeout(bucketConfigMap, r.client); err != nil {
		return fmt.Errorf("unable to create ConfigMap %q for claim %q: %v", bucketConfigMap.Name, options.ObjectBucketClaim.Name)
	}

	return nil
}
