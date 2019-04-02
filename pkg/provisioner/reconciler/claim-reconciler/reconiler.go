package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/klog"
	"k8s.io/klog/klogr"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	pErr "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api/errors"
	internal "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/reconciler-internal"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/util"
)

type objectBucketClaimReconciler struct {
	*internal.InternalClient

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

func NewObjectBucketClaimReconciler(client *internal.InternalClient, name string, provisioner api.Provisioner, options Options) *objectBucketClaimReconciler {
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
		InternalClient:  client,
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

	/**************************
	 Delete Bucket
	***************************/
	if err != nil {
		// The OBC was deleted
		if errors.IsNotFound(err) {
			r.logI.Info("looks like the OBC was deleted")
			err := r.handleDeleteClaim(request.NamespacedName)
			if err != nil {
				klog.Errorf("error deleting ObjectBucket: %v", err)
			}
			return done, err
		}
		return done, fmt.Errorf("error getting claim for request key %q", request)
	}

	/**************************
	 Provision Bucket
	***************************/
	if !r.shouldProvision(obc) {
		r.logI.Info("skipping provision")
		return done, nil
	}
	class, err := util.StorageClassForClaim(obc, r.InternalClient)
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
		ob        *v1alpha1.ObjectBucket
		secret    *corev1.Secret
		configMap *corev1.ConfigMap
		err       error
	)

	obc, err = r.claimForKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("OBC was lost before we could provision: %v", err)
		}
		return err
	}

	// Following getting the claim, if any provisioning task fails, clean up provisiong artifacts.
	// It is assumed that if the claim get fails, no resources were generated to begin with.
	defer func() {
		if err != nil {
			klog.Errorf("errored during reconciliation: %v", err)
			if !pErr.IsBucketExists(err) && ob != nil {
				r.logI.Info("cleaning up cluster")
				r.logI.Info("deleting bucket", "name", ob.Spec.Endpoint.BucketName)
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

	class, err := util.StorageClassForClaim(obc, r.InternalClient)
	if err != nil {
		return err
	}

	if !r.shouldProvision(obc) {
		return nil
	}

	options := &api.BucketOptions{
		ReclaimPolicy:     class.ReclaimPolicy,
		BucketName:        bucketName,
		ObjectBucketClaim: obc.DeepCopy(),
		Parameters:        class.Parameters,
	}

	r.logD.Info("provisioning", "bucket", options.BucketName)
	ob, err = r.provisioner.Provision(options)

	if err != nil {
		return fmt.Errorf("error provisioning bucket: %v", err)
	} else if ob == (&v1alpha1.ObjectBucket{}) {
		return fmt.Errorf("provisioner returned nil/empty object bucket")
	}

	util.SetObjectBucketName(ob, key)
	ob.Spec.StorageClassName = obc.Spec.StorageClassName

	if ob, err = r.createObjectBucket(ob); err != nil {
		return err
	}

	if secret, err = r.createSecret(obc, ob.Spec.Authentication); err != nil {
		return err
	}

	if configMap, err = r.createConfigMap(obc, ob.Spec.Endpoint); err != nil {
		return err
	}

	obc.Spec.ObjectBucketName = ob.Name
	obc.Spec.BucketName = bucketName
	if err = util.UpdateClaim(obc, r.InternalClient); err != nil {
		return err
	}

	return nil
}

func (r *objectBucketClaimReconciler) handleDeleteClaim(key client.ObjectKey) error {

	// TODO each delete should retry a few times to mitigate intermittent errors

	cm := &corev1.ConfigMap{}
	if err := r.Client.Get(r.Ctx, key, cm); err == nil {
		err = r.deleteConfigMap(cm)
		if err != nil {
			return err
		}
	} else if errors.IsNotFound(err) {
		klog.Warningf("ConfigMap for key %v not found, assuming it was deleted in a previous iteration", key)
	} else {
		klog.Errorf("error getting ConfigMap for deletion: %v", err)
	}

	secret := &corev1.Secret{}
	if err := r.Client.Get(r.Ctx, key, secret); err == nil {
		err = r.deleteSecret(secret)
		if err != nil {
			return err
		}
	} else if errors.IsNotFound(err) {
		klog.Warningf("Secret for key %v not found, assuming it was deleted in a previous iteration", key)
	} else {
		klog.Errorf("error getting Secret for deletion: %v", err)
	}

	ob, err := r.objectBucketForClaimKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Errorf("objectBucket was deleted prior to this iteration, ending reconciling")
			return nil
		} else {
			return fmt.Errorf("error getting objectBucket for key: %v", err)
		}
	} else if ob == nil {
		klog.Warning("go a nil bucket for claim key, assuming deletion complete")
		return nil
	}

	if err = r.provisioner.Delete(ob); err != nil {
		// Do not proceed to deleting the ObjectBucket if the deprovisioning fails for bookkeeping purposes
		return fmt.Errorf("error deprovisioning bucket %v", err)
	}

	if err = r.deleteObjectBucket(ob); err != nil {
		if errors.IsNotFound(err) {
			klog.Warning("ObjectBucket %v vanished during deprovisioning, skipping", ob.Name)
		} else {
			return fmt.Errorf("error deleting objectBucket %v", ob.Name)
		}
	}
	return nil
}

func (r *objectBucketClaimReconciler) createObjectBucket(ob *v1alpha1.ObjectBucket) (*v1alpha1.ObjectBucket, error) {
	r.logD.Info("creating ObjectBucket", "name", ob.Name)
	if err := util.CreateUntilDefaultTimeout(ob, r.Client, r.retryInterval, r.retryTimeout); err != nil {
		return nil, err
	}
	return ob, nil
}

func (r *objectBucketClaimReconciler) createSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication) (*corev1.Secret, error) {
	r.logD.Info("composing Secret")
	secret, err := util.NewCredentialsSecret(obc, auth)
	if err != nil {
		return nil, fmt.Errorf("error composing secret: %v", err)
	}

	r.logD.Info("creating Secret", "namespace", secret.Namespace, "name", secret.Name)
	if err = util.CreateUntilDefaultTimeout(secret, r.Client, r.retryInterval, r.retryTimeout); err != nil {
		return nil, fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}
	return secret, nil
}

func (r *objectBucketClaimReconciler) createConfigMap(obc *v1alpha1.ObjectBucketClaim, ep *v1alpha1.Endpoint) (*corev1.ConfigMap, error) {
	r.logD.Info("composing ConfigMap")
	configMap, err := util.NewBucketConfigMap(ep, obc)
	if err != nil {
		return nil, fmt.Errorf("error composing ConfigMap: %v", err)
	}

	r.logD.Info("creating Configmap", "namespace", configMap.Namespace, "name", configMap.Name)
	err = util.CreateUntilDefaultTimeout(configMap, r.Client, r.retryInterval, r.retryTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to create ConfigMap %q for claim %v: %v", configMap.Name, configMap.Name, err)
	}
	return configMap, nil
}

func (r *objectBucketClaimReconciler) createUntilDefaultTimeout(obj runtime.Object) error {
	return wait.PollImmediate(r.retryInterval, r.retryTimeout, func() (done bool, err error) {
		err = r.Client.Create(r.Ctx, obj)
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
	if err := r.Client.Get(r.Ctx, key, obc); err != nil {
		if errors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("error getting claim: %v", err)
	}
	return obc.DeepCopy(), nil
}

func (r *objectBucketClaimReconciler) objectBucketForClaimKey(key client.ObjectKey) (*v1alpha1.ObjectBucket, error) {
	ob := &v1alpha1.ObjectBucket{}
	obKey := client.ObjectKey{
		Name: fmt.Sprintf(util.ObjectBucketNameFormat, key.Namespace, key.Name),
	}
	err := r.Client.Get(r.Ctx, obKey, ob)
	if err != nil {
		return nil, fmt.Errorf("error listing object buckets: %v", err)
	}
	return ob, nil
}

func (r *objectBucketClaimReconciler) configMapForClaimKey(key client.ObjectKey) (*corev1.ConfigMap, error) {
	var cm *corev1.ConfigMap
	err := r.Client.Get(r.Ctx, key, cm)
	return cm, err
}

func (r *objectBucketClaimReconciler) updateObjectBucketClaimPhase(obc *v1alpha1.ObjectBucketClaim, phase v1alpha1.ObjectBucketClaimStatusPhase) (*v1alpha1.ObjectBucketClaim, error) {
	obc.Status.Phase = phase
	err := r.Client.Update(r.Ctx, obc)
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

func (r *objectBucketClaimReconciler) deprovisionBucket(ob *v1alpha1.ObjectBucket) {
	if ob != nil {
		r.logD.Info("deprovisioning bucket", "name", ob.Spec.Endpoint.BucketName)

		if err := r.provisioner.Delete(ob); err != nil {
			klog.Errorf("error deleting object store bucket %v: %v", ob.Spec.Endpoint.BucketName, err)
		}
	}
}

func (r *objectBucketClaimReconciler) deleteConfigMap(cm *corev1.ConfigMap) error {
	if cm == nil {
		klog.Errorf("got nil ConfigMap, skipping")
		return nil
	}
	if util.HasFinalizer(cm) {
		r.logD.Info("removing finalizer from ConfigMap", "name", cm.Name)

		err := util.RemoveFinalizer(cm, r.InternalClient)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warning("ConfigMap %v vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on ConfigMap %s/%s: %v", cm.Namespace, cm.Name, err)
			}
		}
	}

	err := r.Client.Delete(context.Background(), cm)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Warning("ConfigMap %v vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting ConfigMap %s/%s: %v", cm.Namespace, cm.Name, err)
		}
	}
	return nil
}

func (r *objectBucketClaimReconciler) deleteSecret(sec *corev1.Secret) error {
	if sec == nil {
		klog.Errorf("got nil secret, skipping")
		return nil
	}
	if util.HasFinalizer(sec) {
		r.logD.Info("removing finalizer from Secret", "namespace", sec.Namespace, "name", sec.Name)

		err := util.RemoveFinalizer(sec, r.InternalClient)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warning("Secret %v vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on Secret %s/%s: %v", sec.Namespace, sec.Name, err)
			}
		}
	}

	err := r.Client.Delete(context.Background(), sec)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Warning("Secret %v vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting Secret %s/%s: %v", sec.Namespace, sec.Name, err)
		}
	}
	return nil
}

func (r *objectBucketClaimReconciler) deleteObjectBucket(ob *v1alpha1.ObjectBucket) error {

	if ob == nil {
		klog.Errorf("got nil objectBucket, skipping")
		return nil
	}
	if util.HasFinalizer(ob) {

		r.logD.Info("deleting ObjectBucket", "name", ob.Name)

		err := util.RemoveFinalizer(ob, r.InternalClient)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Warning("ObjectBucket %v vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on ObjectBucket %s: %v", ob.Name, err)
			}
		}
	}

	err := r.Client.Delete(context.Background(), ob)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Warning("ObjectBucket %v vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting ObjectBucket %s/%s: %v", ob.Namespace, ob.Name, err)
		}
	}
	return nil
}
