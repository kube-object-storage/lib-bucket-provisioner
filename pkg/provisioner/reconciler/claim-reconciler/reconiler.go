package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/go-logr/logr"
	"k8s.io/klog/klogr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	pErr "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api/errors"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"
)

type objectBucketClaimReconciler struct {
	ctx    context.Context
	client client.Client
	scheme *runtime.Scheme

	provisionerName string
	provisioner     api.Provisioner

	retryInterval time.Duration
	retryTimeout  time.Duration
	retryBackoff  int

	logD logr.InfoLogger
	logI logr.InfoLogger
}

var _ reconcile.Reconciler = &objectBucketClaimReconciler{}

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
	RetryBackoff  int
}

func NewObjectBucketClaimReconciler(c client.Client, scheme *runtime.Scheme, name string, provisioner api.Provisioner, options Options) *objectBucketClaimReconciler {
	locallogD := klogr.New().WithName(util.DomainPrefix + "/reconciler/" + name).V(util.DebugLogLvl)
	locallogI := klogr.New().WithName(util.DomainPrefix + "/reconciler/" + name)

	locallogI.Info("constructing new reconciler", "provisioner", name)

	if options.RetryInterval < util.DefaultRetryBaseInterval {
		options.RetryInterval = util.DefaultRetryBaseInterval
	}
	locallogD.Info("retry loop setting", "RetryBaseInterval", options.RetryInterval)
	if options.RetryTimeout < util.DefaultRetryTimeout {
		options.RetryTimeout = util.DefaultRetryTimeout
	}
	locallogD.Info("retry loop setting", "RetryTimeout", options.RetryTimeout)
	if options.RetryBackoff < util.DefaultRetryBackOff {
		options.RetryBackoff = util.DefaultRetryBackOff
	}
	locallogD.Info("retry loop setting", "RetryBackOff", options.RetryBackoff)
	return &objectBucketClaimReconciler{
		ctx:             context.Background(),
		client:          c,
		scheme:          scheme,
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

	// Generate new loggers each request for descriptive messages
	r.logD = klogr.New().WithName(util.DomainPrefix+"/reconciler").WithValues("req", request.String()).V(util.DebugLogLvl)
	r.logI = klogr.New().WithName(util.DomainPrefix+"/reconciler").WithValues("req", request.String())

	r.logD.Info("reconciling request")

	handleErr := func(format string, a ...interface{}) (reconcile.Result, error) {
		r.logD.Info("error:", "msg", fmt.Sprintf(format, a...))
		return reconcile.Result{}, fmt.Errorf(format, a...)
	}

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

	obc, err := r.claimFromKey(request.NamespacedName)
	if err != nil && !errors.IsNotFound(err) {
		return handleErr("error getting claim for key: %v", err)
	}

	if !r.shouldProvision(obc) {
		return reconcile.Result{}, nil // don't return errors as it triggers a re-queuing of the request
	}

	bucketName, err := util.GenerateBucketName(obc)
	if err != nil {
		return handleErr("error composing bucket name: %v", err)
	}

	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		return handleErr("unable to get storage class: %v", err)
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
		klog.Error(err) // controller-runtime does not report returned errors. log them before handing them off˚
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (r *objectBucketClaimReconciler) handelReconcile(options *api.BucketOptions) error {

	// ///   ///   ///   ///   ///   ///   ///
	// TODO    CAUTION! UNDER CONSTRUCTION!
	// ///   ///   ///   ///   ///   ///   ///

	r.logD.Info("handleReconcile()")

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
			r.logI.Info("performing cleanup")
			if !pErr.IsBucketExists(err) && ob != nil {
				r.logD.Info("deleting bucket", "name", ob.Spec.Endpoint.BucketName)
				if err := r.provisioner.Delete(ob); err != nil {
					klog.Infof("error deleting bucket: %v", err)
				}
			}
			r.deleteResources(ob, configMap, secret)
		}
	}()

	r.logD.Info("provisioning", "bucket", options.BucketName)
	connection, err = r.provisioner.Provision(options)
	if err != nil {
		return fmt.Errorf("error provisioning bucket: %v", err)
	} else if connection == nil {
		return fmt.Errorf("error provisioning bucket.  got nil connection")
	}

	if ob, err = r.createObjectBucket(options, connection); err != nil {
		return fmt.Errorf("error reconciling: %v", err)
	}

	if secret, err = r.createSecret(options, connection); err != nil {
		return fmt.Errorf("error reconciling: %v", err)
	}

	if configMap, err = r.createConfigMap(options, connection); err != nil {
		return fmt.Errorf("error reconciling: %v", err)
	}

	return nil
}

func (r *objectBucketClaimReconciler) createObjectBucket(options *api.BucketOptions, connection *v1alpha1.Connection) (*v1alpha1.ObjectBucket, error) {
	r.logD.Info("composing ObjectBucket")
	ob, err := util.NewObjectBucket(options.ObjectBucketClaim, connection)
	if err != nil {
		return nil, fmt.Errorf("error composing object bucket: %v", err)
	}
	r.logD.Info("creating ObjectBucket", "name", ob.Name)
	if err = util.CreateUntilDefaultTimeout(ob, r.client, r.retryInterval, r.retryTimeout); err != nil {
		return nil, fmt.Errorf("unable to create ObjectBucket %q: %v", ob.Name, err)
	}
	return ob, nil
}

func (r *objectBucketClaimReconciler) createSecret(options *api.BucketOptions, connection *v1alpha1.Connection) (*corev1.Secret, error) {
	r.logD.Info("composing Secret")
	secret, err := util.NewCredentialsSecret(options.ObjectBucketClaim, connection.Authentication)
	if err != nil {
		return nil, fmt.Errorf("error composing secret: %v", err)
	}
	r.logD.Info("creating Secret", "namespace", secret.Namespace, "name", secret.Name)
	if err = util.CreateUntilDefaultTimeout(secret, r.client, r.retryInterval, r.retryTimeout); err != nil {
		return nil, fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}
	return secret, nil
}

func (r *objectBucketClaimReconciler) createConfigMap(options *api.BucketOptions, connection *v1alpha1.Connection) (*corev1.ConfigMap, error) {
	r.logD.Info("composing ConfigMap")
	configMap, err := util.NewBucketConfigMap(connection.Endpoint, options.ObjectBucketClaim)
	if err != nil {
		return nil, fmt.Errorf("error composing configmap for ObjectBucketClaim %s/%s: %v", options.ObjectBucketClaim.Namespace, options.ObjectBucketClaim.Name, err)
	}
	r.logD.Info("creating Configmap", "namespace", configMap.Namespace, "name", configMap.Name)
	if err = util.CreateUntilDefaultTimeout(configMap, r.client, r.retryInterval, r.retryTimeout); err != nil {
		return nil, fmt.Errorf("unable to create ConfigMap %q for claim %v: %v", configMap.Name, options.ObjectBucketClaim.Name, err)
	}
	return configMap, nil
}

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.
// Down the road, this will perform a broader set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(obc *v1alpha1.ObjectBucketClaim) bool {
	if obc == nil {
		r.logI.Info("nil OBC, assuming delete event")
		return false
	}

	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		klog.Errorf("cannot provision: %v", err)
		return false
	}
	if class.Provisioner != r.provisionerName {
		r.logI.Info("claim provisioner does not match expected provisioner", "claim provisioner", class.Provisioner, "should match", r.provisionerName)
		return false
	}
	return true
}

func (r *objectBucketClaimReconciler) claimFromKey(key client.ObjectKey) (*v1alpha1.ObjectBucketClaim, error) {
	obc := &v1alpha1.ObjectBucketClaim{}
	if err := r.client.Get(r.ctx, key, obc); err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("error getting claim: %v", err)
	}
	return obc, nil
}

func (r *objectBucketClaimReconciler) deleteResources(ob *v1alpha1.ObjectBucket, cm *corev1.ConfigMap, s *corev1.Secret) {
	r.deleteObjectBucket(ob)
	r.deleteSecret(s)
	r.deleteConfigMap(cm)
}

func (r *objectBucketClaimReconciler) deleteBucket(ob *v1alpha1.ObjectBucket) {
	if ob != nil {
		r.logD.Info("deleting bucket", "name", ob.Spec.Endpoint.BucketName)
		if err := r.provisioner.Delete(ob); err != nil {
			klog.Errorf("error deleting object store bucket %v: %v", ob.Spec.Endpoint.BucketName, err)
		}
	}
}

func (r *objectBucketClaimReconciler) deleteConfigMap(cm *corev1.ConfigMap) {
	if cm != nil {
		r.logD.Info("deleting ConfigMap", "name", cm.Name)
		if err := r.client.Delete(context.Background(), cm); err != nil && errors.IsNotFound(err) {
			klog.Errorf("Error deleting ConfigMap %v: %v", cm.Name, err)
		}
	}
}

func (r *objectBucketClaimReconciler) deleteSecret(s *corev1.Secret) {
	if s != nil {
		r.logD.Info("deleting Secret", "name", s.Name)
		if err := r.client.Delete(context.Background(), s); err != nil && errors.IsNotFound(err) {
			klog.Errorf("Error deleting Secret %v: %v", s.Name, err)
		}
	}
}

func (r *objectBucketClaimReconciler) deleteObjectBucket(ob *v1alpha1.ObjectBucket) {
	if ob != nil {
		r.logD.Info("deleting ObjectBucket", "name", ob.Name)
		if err := r.client.Delete(context.Background(), ob); err != nil && !errors.IsNotFound(err) {
			klog.Errorf("Error deleting ObjectBucket %v: %v", ob.Name, err)
		}
	}
}
