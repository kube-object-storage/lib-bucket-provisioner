package reconciler

import (
	"context"
	"fmt"
	"k8s.io/api/core/v1"
	"strings"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/reconciler/util"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
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

// Reconcile implementes the Reconciler interface.  This function contains the business logic of the
// OBC controller.  Currently, the process strictly serves as a POC for an OBC controller and is
// extremely fragile.
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

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.  Down the road, this will perform a broader
// set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(class *storagev1.StorageClass) bool {
	return class.Provisioner == r.provisionerName
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (r *objectBucketClaimReconciler) handelProvision(options *provisioner.BucketOptions) error {

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

	var (
		ob        *v1alpha1.ObjectBucket
		secret    *v1.Secret
		configMap *v1.ConfigMap
		err       error
	)
	defer func() {
		if err != nil {
			_ = r.provisioner.Delete(ob)
			_ = r.client.Delete(context.Background(), ob)
			_ = r.client.Delete(context.Background(), secret)
			_ = r.client.Delete(context.Background(), configMap)
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

	secret = util.NewCredentailsSecret(options.ObjectBucketClaim, s3keys)
	if err = util.CreateUntilDefaultTimeout(secret, r.client); err != nil {
		return fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}

	configMap = util.NewBucketConfigMap(ob, options.ObjectBucketClaim)
	if err = util.CreateUntilDefaultTimeout(configMap, r.client); err != nil {
		return fmt.Errorf("unable to create ConfigMap %q for claim %q: %v", configMap.Name, options.ObjectBucketClaim.Name)
	}

	return nil
}
