package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"
)

type objectBucketClaimReconciler struct {
	ctx             context.Context
	client          client.Client
	provisionerName string
	provisioner     api.Provisioner
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

func NewObjectBucketClaimReconciler(c client.Client, name string, provisioner api.Provisioner, options Options) *objectBucketClaimReconciler {
	if options.RetryInterval < util.DefaultRetryBaseInterval {
		options.RetryInterval = util.DefaultRetryBaseInterval
	}
	klog.V(util.DebugLogLvl).Infof("Retry base interval == %s", options.RetryInterval)
	if options.RetryTimeout < util.DefaultRetryTimeout {
		options.RetryTimeout = util.DefaultRetryTimeout
	}
	klog.V(util.DebugLogLvl).Infof("Retry timeout == %s", options.RetryTimeout)
	if options.RetryBackoff < util.DefaultRetryBackOff {
		options.RetryBackoff = util.DefaultRetryBackOff
	}
	klog.V(util.DebugLogLvl).Infof("Retry backoff == %d", options.RetryBackoff)
	return &objectBucketClaimReconciler{
		ctx:             context.Background(),
		client:          c,
		provisionerName: strings.ToLower(name),
		provisioner:     provisioner,
		retryInterval:   options.RetryInterval,
		retryTimeout:    options.RetryTimeout,
		retryBackoff:    options.RetryBackoff,
	}
}

// Reconcile implements the Reconciler interface.  This function contains the business logic of the
// OBC controller.  Currently, the process strictly serves as a POC for an OBC controller and is
// extremely fragile.
func (r *objectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	handleErr := func(format string, a ...interface{}) (reconcile.Result, error) {
		return reconcile.Result{}, fmt.Errorf(format, a...)
	}

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

	klog.V(util.DebugLogLvl).Infof("reconciling object %s", request.NamespacedName)

	obc, err := r.claimFromKey(request.NamespacedName)
	if err != nil {
		return handleErr("error getting claim for key %s: %v", err)
	}

	if !r.shouldProvision(obc) {
		return handleErr("skipping provisioning for claim %s", obc.Name)
	}

	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		return handleErr("unable to get storage class: %v", err)
	}

	bucketName := obc.Spec.BucketName
	if bucketName == "" {
		bucketName = util.GenerateBucketName(obc.Spec.GeneratBucketName)
	}

	options := &api.BucketOptions{
		ReclaimPolicy:     class.ReclaimPolicy,
		ObjectBucketName:  fmt.Sprintf("obc-%s-%s", obc.Namespace, obc.Name),
		BucketName:        bucketName,
		ObjectBucketClaim: obc,
		Parameters:        class.Parameters,
	}

	err = r.handelReconcile(options)
	if err != nil {
		return handleErr("failed Provisioning bucket %q for claim \"%s/%s\": %v", options.BucketName, obc.Namespace, obc.Name, err)
	}

	return reconcile.Result{}, nil
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (r *objectBucketClaimReconciler) handelReconcile(options *api.BucketOptions) error {

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

	if options == nil {
		return fmt.Errorf("error reconciling obj, got nil BucketOptions")
	}

	var (
		ob         *v1alpha1.ObjectBucket
		connection *v1alpha1.Connection
		secret     *corev1.Secret
		configMap  *corev1.ConfigMap
		err        error
	)

	// If any process of provisioning occurs, clean up all artifacts of the provision process
	// so we can start fresh in the next iteration
	defer func() {
		if err != nil {
			if ob != nil {
				_ = r.provisioner.Delete(ob)
				_ = r.client.Delete(context.Background(), ob)
			}
			if secret != nil {
				_ = r.client.Delete(context.Background(), secret)
			}
			if configMap != nil {
				_ = r.client.Delete(context.Background(), configMap)
			}
		}
	}()

	klog.V(util.DebugLogLvl).Infof("Provisioning bucket %q", options.BucketName)
	connection, err = r.provisioner.Provision(options)
	if err != nil {
		return fmt.Errorf("error provisioning bucket: %v", err)
	} else if connection == nil {
		return fmt.Errorf("error provisioning bucket.  got nil connection")
	}

	ob, err = util.NewObjectBucket(options.ObjectBucketClaim, connection)
	if err != nil {
		return fmt.Errorf("error composing object bucket: %v", err)
	}

	if err = util.CreateUntilDefaultTimeout(ob, r.client); err != nil {
		return fmt.Errorf("unable to create ObjectBucket %q: %v", ob.Name, err)
	}

	secret, err = util.NewCredentialsSecret(options.ObjectBucketClaim, connection.Authentication)
	if err != nil {
		return fmt.Errorf("error composing secret: %v", err)
	}
	if err = util.CreateUntilDefaultTimeout(secret, r.client); err != nil {
		return fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}

	configMap, err = util.NewBucketConfigMap(connection.Endpoint, options.ObjectBucketClaim)
	if err != nil {
		return fmt.Errorf("error composing configmap for ObjectBucketClaim \"%s\\%s\": %v", options.ObjectBucketClaim.Namespace, options.ObjectBucketClaim.Name, err)
	}
	if err = util.CreateUntilDefaultTimeout(configMap, r.client); err != nil {
		return fmt.Errorf("unable to create ConfigMap %q for claim %v: %v", configMap.Name, options.ObjectBucketClaim.Name, err)
	}

	return nil
}

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.
// Down the road, this will perform a broader set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(obc *v1alpha1.ObjectBucketClaim) bool {

	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		klog.Errorf("cannot provision: %v", err)
		return false
	}
	if class.Provisioner != r.provisionerName {
		return false
	}
	return true
}

func (r *objectBucketClaimReconciler) claimFromKey(key client.ObjectKey) (*v1alpha1.ObjectBucketClaim, error) {
	obc := &v1alpha1.ObjectBucketClaim{}
	if err := r.client.Get(r.ctx, key, obc); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("object for key %s does not exist. it may have been deleted before reonciliation started", key)
		}
	}
	return obc, nil
}
