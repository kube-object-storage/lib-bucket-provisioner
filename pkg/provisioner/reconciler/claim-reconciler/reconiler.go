package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	pErr "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api/errors"
	internal "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/reconciler/reconciler-internal"
)

type objectBucketClaimReconciler struct {
	*internal.InternalClient

	provisionerName string
	provisioner     api.Provisioner

	retryInterval time.Duration
	retryTimeout  time.Duration
}

var _ reconcile.Reconciler = &objectBucketClaimReconciler{}

type Options struct {
	RetryInterval time.Duration
	RetryTimeout  time.Duration
}

func NewObjectBucketClaimReconciler(client client.Client, scheme *runtime.Scheme, name string, provisioner api.Provisioner, options Options) *objectBucketClaimReconciler {

	log.Info("constructing new reconciler", "provisioner", name)

	if options.RetryInterval < internal.DefaultRetryBaseInterval {
		options.RetryInterval = internal.DefaultRetryBaseInterval
	}
	logD.Info("retry loop setting", "RetryBaseInterval", options.RetryInterval)
	if options.RetryTimeout < internal.DefaultRetryTimeout {
		options.RetryTimeout = internal.DefaultRetryTimeout
	}
	logD.Info("retry loop setting", "RetryTimeout", options.RetryTimeout)

	return &objectBucketClaimReconciler{
		InternalClient: &internal.InternalClient{
			Ctx:    context.Background(),
			Client: client,
			Scheme: scheme,
		},
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

	setLoggersWithRequest(request)

	logD.Info("new Reconcile iteration")

	var done = reconcile.Result{Requeue: false}

	obc, err := r.claimForKey(request.NamespacedName)

	/**************************
	 Delete Bucket
	***************************/
	if err != nil {
		// The OBC was deleted
		log.Info("error getting claim")
		if errors.IsNotFound(err) {
			log.Info("looks like the OBC was deleted, proceeding with cleanup")
			err := r.handleDeleteClaim(request.NamespacedName)
			if err != nil {
				log.Error(err, "error deleting ObjectBucket: %v")
			}
			return done, err
		}
		return done, fmt.Errorf("error getting claim for request key %q", request)
	}

	/**************************
	 Provision Bucket
	***************************/
	if !r.shouldProvision(obc) {
		log.Info("skipping provision")
		return done, nil
	}
	class, err := internal.StorageClassForClaim(obc, r.InternalClient)
	if err != nil {
		return done, err
	}
	if !r.supportedProvisioner(class.Provisioner) {
		log.Info("unsupported provisioner", "got", class.Provisioner)
		return done, nil
	}

	// By now, we should know that the OBC matches our provisioner, lacks an OB, and thus requires provisioning
	err = r.handleProvisionClaim(request.NamespacedName, obc)

	// If handleReconcile() errors, the request will be re-queued.  In the distant future, we will likely want some ignorable error types in order to skip re-queuing
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

	// Following getting the claim, if any provisioning task fails, clean up provisioned artifacts.
	// It is assumed that if the get claim fails, no resources were generated to begin with.
	defer func() {
		if err != nil {
			log.Error(err, "cleaning up reconcile artifacts")
			if !pErr.IsBucketExists(err) && ob != nil {
				log.Info("deleting bucket", "name", ob.Spec.Endpoint.BucketName)
				if err := r.provisioner.Delete(ob); err != nil {
					log.Error(err, "error deleting bucket")
				}
			}
			r.deleteResources(ob, configMap, secret)
		}
	}()

	bucketName, err := internal.ComposeBucketName(obc)
	if err != nil {
		return fmt.Errorf("error composing bucket name: %v", err)
	}

	class, err := internal.StorageClassForClaim(obc, r.InternalClient)
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

	logD.Info("provisioning", "bucket", options.BucketName)
	ob, err = r.provisioner.Provision(options)

	if err != nil {
		return fmt.Errorf("error provisioning bucket: %v", err)
	} else if ob == (&v1alpha1.ObjectBucket{}) {
		return fmt.Errorf("provisioner returned nil/empty object bucket")
	}

	internal.SetObjectBucketName(ob, key)
	ob.Spec.StorageClassName = obc.Spec.StorageClassName

	if ob, err = internal.CreateObjectBucket(ob, r.Client, r.retryInterval, r.retryTimeout); err != nil {
		return err
	}

	if secret, err = internal.CreateSecret(obc, ob.Spec.Authentication, r.Client, r.retryInterval, r.retryTimeout); err != nil {
		return err
	}

	if configMap, err = internal.CreateConfigMap(obc, ob.Spec.Endpoint, r.Client, r.retryInterval, r.retryTimeout); err != nil {
		return err
	}

	obc.Spec.ObjectBucketName = ob.Name
	obc.Spec.BucketName = bucketName
	if err = internal.UpdateClaim(obc, r.InternalClient); err != nil {
		return err
	}
	log.Info("provisioning succeeded")
	return nil
}

func (r *objectBucketClaimReconciler) handleDeleteClaim(key client.ObjectKey) error {

	// TODO each delete should retry a few times to mitigate intermittent errors

	cm := &corev1.ConfigMap{}
	if err := r.Client.Get(r.Ctx, key, cm); err == nil {
		err = internal.DeleteConfigMap(cm, r.InternalClient)
		if err != nil {
			return err
		}
	} else if errors.IsNotFound(err) {
		log.Error(err, "configMap not found, assuming it was already deleted")
	} else {
		log.Error(err, "error getting configMap for deletion")
	}

	secret := &corev1.Secret{}
	if err := r.Client.Get(r.Ctx, key, secret); err == nil {
		err = internal.DeleteSecret(secret, r.InternalClient)
		if err != nil {
			return err
		}
	} else if errors.IsNotFound(err) {
		log.Error(err, "secret not found, assuming it was already deleted ")
	} else {
		log.Error(err, "error getting secret for deletion")
	}

	ob, err := r.objectBucketForClaimKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "objectBucket not found, assuming it was already deleted")
			return nil
		} else {
			return fmt.Errorf("error getting objectBucket for key: %v", err)
		}
	} else if ob == nil {
		log.Error(nil, "got nil objectBucket, assuming deletion complete")
		return nil
	}

	if err = r.provisioner.Delete(ob); err != nil {
		// Do not proceed to deleting the ObjectBucket if the deprovisioning fails for bookkeeping purposes
		return fmt.Errorf("error deprovisioning bucket %v", err)
	}

	if err = internal.DeleteObjectBucket(ob, r.InternalClient); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "ObjectBucket vanished during deprovisioning, assuming deletion complete")
		} else {
			return fmt.Errorf("error deleting objectBucket %v", ob.Name)
		}
	}
	return nil
}

// shouldProvision is a simplistic check on whether this obc is a concern for this provisioner.
// Down the road, this will perform a broader set of checks.
func (r *objectBucketClaimReconciler) shouldProvision(obc *v1alpha1.ObjectBucketClaim) bool {
	logD.Info("validating claim for provisioning")
	if obc.Spec.ObjectBucketName != "" {
		log.Info("provisioning already completed", "ObjectBucket", obc.Spec.ObjectBucketName)
		return false
	}
	if obc.Spec.StorageClassName == "" {
		log.Info("OBC did not provide a storage class, cannot provision")
		return false
	}
	return true
}

func (r *objectBucketClaimReconciler) supportedProvisioner(provisioner string) bool {
	return provisioner == r.provisionerName
}

func (r *objectBucketClaimReconciler) claimForKey(key client.ObjectKey) (*v1alpha1.ObjectBucketClaim, error) {
	logD.Info("getting claim for key")
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
	logD.Info("getting objectBucket for key", "key", key)
	ob := &v1alpha1.ObjectBucket{}
	obKey := client.ObjectKey{
		Name: fmt.Sprintf(internal.ObjectBucketNameFormat, key.Namespace, key.Name),
	}
	err := r.Client.Get(r.Ctx, obKey, ob)
	if err != nil {
		return nil, fmt.Errorf("error listing object buckets: %v", err)
	}
	return ob, nil
}

func (r *objectBucketClaimReconciler) configMapForClaimKey(key client.ObjectKey) (*corev1.ConfigMap, error) {
	logD.Info("getting configMap for key", "key", key)
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
	if err := internal.DeleteObjectBucket(ob, r.InternalClient); err != nil {
		log.Error(err, "error deleting objectBucket")
	}
	if err := internal.DeleteSecret(s, r.InternalClient); err != nil {
		log.Error(err, "error deleting secret")
	}
	if err := internal.DeleteConfigMap(cm, r.InternalClient); err != nil {
		log.Error(err, "error deleting configMap")
	}
}
