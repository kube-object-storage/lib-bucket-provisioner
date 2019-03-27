package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/klog"
	"k8s.io/klog/klogr"

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

	logD logr.InfoLogger
	logI logr.InfoLogger
}

var _ reconcile.Reconciler = &objectBucketClaimReconciler{}

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
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
	return &objectBucketClaimReconciler{
		ctx:             context.Background(),
		client:          c,
		scheme:          scheme,
		provisionerName: strings.ToLower(name),
		provisioner:     provisioner,
		retryInterval:   options.RetryInterval,
		retryTimeout:    options.RetryTimeout,
	}
}

// Reconcile implements the Reconciler interface.  This function contains the business logic of the
// OBC controller.  Currently, the process strictly serves as a POC for an OBC controller and is
// extremely fragile.
func (r *objectBucketClaimReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {

	// Generate new loggers each request for descriptive messages
	r.logD = klogr.New().WithName(util.DomainPrefix+"/reconciler").WithValues("req", request.String()).V(util.DebugLogLvl)
	r.logI = klogr.New().WithName(util.DomainPrefix+"/reconciler").WithValues("req", request.String())

	var done = reconcile.Result{Requeue: false}

	obc, err := r.claimForKey(request.NamespacedName)

	// Delete path
	if err != nil {
		// The OBC was deleted
		if errors.IsNotFound(err) || obc.DeletionTimestamp != nil {
			r.logI.Info("looks like the OBC was deleted")
			r.handleDeleteClaim(request.NamespacedName)
			return done, nil
		}
		return done, fmt.Errorf("error getting claim for request key %q", request)
	}

	// Provision path
	if !r.shouldProvision(obc) {
		r.logI.Info("skipping provision")
		return done, nil
	}
	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		klog.Error(err)
		return done, err
	}
	if !r.supportedProvisioner(class.Provisioner) {
		r.logI.Info("unsupported provisioner", "got", class.Provisioner)
		return done, nil
	}

	// By now, we should know that the OBC matches our provisioner, lacks an OB, and thus requires provisioning
	err = r.handleProvisionClaim(request.NamespacedName, obc)
	if err != nil {
		// controller-manager does not report the errors, log them before returning
		klog.Error(err)
	}
	// If handleReconcile() errors, the request will be re-queued.  In the distant future, we will likely want some
	// ignorable error types in order to skip re-queuing
	return done, err
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (r *objectBucketClaimReconciler) handleProvisionClaim(key client.ObjectKey, obc *v1alpha1.ObjectBucketClaim) error {

	var (
		ob         *v1alpha1.ObjectBucket
		connection *v1alpha1.Connection
		secret     *corev1.Secret
		configMap  *corev1.ConfigMap
		err        error
	)

	// If any process of provisioning occurs, clean up all artifacts of the provision process
	// so we can start fresh in the next iteration`
	defer func() {
		if err != nil {
			r.logI.Info("performing cleanup")
			if !pErr.IsBucketExists(err) && ob != nil {
				r.logD.Info("deleting bucket", "bucket name", ob.Spec.Endpoint.BucketName)
				if err := r.provisioner.Delete(ob); err != nil {
					klog.Errorf("error deleting bucket: %v", err)
				}
			}
			r.deleteResources(ob, configMap, secret)
		}
	}()

	bucketName, err := util.ComposeBucketName(obc)
	if err != nil {
		return fmt.Errorf("error composing bucket name: %v", err)
	}

	class, err := util.StorageClassForClaim(obc, r.client, r.ctx)
	if err != nil {
		return err
	}

	options := &api.BucketOptions{
		ReclaimPolicy:     class.ReclaimPolicy,
		ObjectBucketName:  fmt.Sprintf("obc-%s-%s", obc.Namespace, obc.Name),
		BucketName:        bucketName,
		ObjectBucketClaim: obc,
		Parameters:        class.Parameters,
	}

	obc, err = r.claimForKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("OBC was lost before we could provision: %v", err)
		}
		return err
	}

	if !r.shouldProvision(obc) {
		return nil
	}

	r.logD.Info("provisioning", "bucket", options.BucketName)
	connection, err = r.provisioner.Provision(options)
	if err != nil {
		return fmt.Errorf("error provisioning bucket: %v", err)
	}
	if connection == nil {
		return fmt.Errorf("error provisioning bucket.  got nil connection")
	}
	if err := util.ValidEndpoint(connection.Endpoint); err != nil {
		return err
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

func (r *objectBucketClaimReconciler) handleDeleteClaim(key client.ObjectKey) error {
	ob, err := r.objectBucketForClaimKey(key)
	if err != nil {
		return err
	}
	return r.provisioner.Delete(ob)
}

func (r *objectBucketClaimReconciler) createObjectBucket(options *api.BucketOptions, connection *v1alpha1.Connection) (*v1alpha1.ObjectBucket, error) {
	r.logD.Info("composing ObjectBucket")
	ob, err := util.NewObjectBucket(options.ObjectBucketClaim, connection, r.client, r.ctx, r.scheme)
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
	err = util.CreateUntilDefaultTimeout(configMap, r.client, r.retryInterval, r.retryTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to create ConfigMap %q for claim %v: %v", configMap.Name, options.ObjectBucketClaim.Name, err)
	}
	return configMap, nil
}

func (r *objectBucketClaimReconciler) createUntilDefaultTimeout(obj runtime.Object) error {
	return wait.PollImmediate(r.retryInterval, r.retryTimeout, func() (done bool, err error) {
		err = r.client.Create(r.ctx, obj)
		if err != nil && !errors.IsAlreadyExists(err) {
			return false, err
		}
		return true, nil
	})
}

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.
// Down the road, this will perform a broader set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(obc *v1alpha1.ObjectBucketClaim) bool {

	if obc.Spec.ObjectBucketName != "" {
		r.logI.Info("provisioning already completed", "ObjectBucket", obc.Spec.ObjectBucketName)
		return false
	}
	if obc.Spec.StorageClassName == "" {
		r.logI.Info("OBC did not provide a storage class, cannot provision")
		return false
	}
	return true
}

func (r *objectBucketClaimReconciler) supportedProvisioner(provisioner string) bool {
	return provisioner == r.provisionerName
}

func (r *objectBucketClaimReconciler) claimForKey(key client.ObjectKey) (*v1alpha1.ObjectBucketClaim, error) {
	obc := &v1alpha1.ObjectBucketClaim{}
	if err := r.client.Get(r.ctx, key, obc); err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("error getting claim: %v", err)
	}
	return obc.DeepCopy(), nil
}

func (r *objectBucketClaimReconciler) objectBucketForClaimKey(key client.ObjectKey) (*v1alpha1.ObjectBucket, error) {
	clusterOBs := &v1alpha1.ObjectBucketList{}
	err := r.client.List(r.ctx, &client.ListOptions{}, clusterOBs)
	if err != nil {
		return nil, fmt.Errorf("error listing object buckets: %v", err)
	}

	var ob *v1alpha1.ObjectBucket
	for _, item := range clusterOBs.Items {
		if item.Name == fmt.Sprintf(util.ObjectBucketFormat, key.Namespace, key.Name) {
			item.DeepCopyInto(ob)
			break
		}
	}
	return ob, nil
}

func (r *objectBucketClaimReconciler) configMapForClaimKey(key client.ObjectKey) (*corev1.ConfigMap, error) {
	var cm *corev1.ConfigMap
	err := r.client.Get(r.ctx, key, cm)
	return cm, err
}

func (r *objectBucketClaimReconciler) updateObjectBucketClaimPhase(obc *v1alpha1.ObjectBucketClaim, phase v1alpha1.ObjectBucketClaimStatusPhase) (*v1alpha1.ObjectBucketClaim, error) {
	obc.Status.Phase = phase
	err := r.client.Update(r.ctx, obc)
	if err != nil {
		return nil, fmt.Errorf("error updating phase: %v", err)
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

func (r *objectBucketClaimReconciler) cleanUp() {}
