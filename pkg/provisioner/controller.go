package provisioner

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/client/clientset/versioned"
	informers "github.com/yard-turkey/lib-bucket-provisioner/pkg/client/informers/externalversions/objectbucket.io/v1alpha1"
	listers "github.com/yard-turkey/lib-bucket-provisioner/pkg/client/listers/objectbucket.io/v1alpha1"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	pErr "github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api/errors"
)

type controller interface {
	Start(<-chan struct{}) error
}

// Provisioner is a CRD Controller responsible for executing the Reconcile() function in response to OB and OBC events.
type Controller struct {
	clientset    kubernetes.Interface
	libClientset versioned.Interface
	obcLister    listers.ObjectBucketClaimLister
	obLister     listers.ObjectBucketLister
	obcInformer  informers.ObjectBucketClaimInformer
	obcHasSynced cache.InformerSynced
	obHasSynced  cache.InformerSynced
	queue        workqueue.RateLimitingInterface

	provisioner     api.Provisioner
	provisionerName string
}

var _ controller = &Controller{}

func NewController(provisionerName string, provisioner api.Provisioner, clientset kubernetes.Interface, crdClientSet versioned.Interface, obcInformer informers.ObjectBucketClaimInformer, obInformer informers.ObjectBucketInformer) *Controller {
	ctrl := &Controller{
		clientset:       clientset,
		libClientset:    crdClientSet,
		obcLister:       obcInformer.Lister(),
		obLister:        obInformer.Lister(),
		obcInformer:     obcInformer,
		obcHasSynced:    obcInformer.Informer().HasSynced,
		obHasSynced:     obInformer.Informer().HasSynced,
		queue:           workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		provisionerName: provisionerName,
		provisioner:     provisioner,
	}

	obcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ctrl.enqueueOBC,
		UpdateFunc: func(oldObj, newObj interface{}) {
			// noop
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				utilruntime.HandleError(err)
			}
			ctrl.queue.AddRateLimited(key)
		},
	})
	return ctrl
}

func (c *Controller) enqueueOBC(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.queue.AddRateLimited(key)
}

func (c *Controller) Start(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, c.obcHasSynced, c.obHasSynced) {
		return fmt.Errorf("failed to waith for caches to sync ")
	}
	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
	return nil
}

func (c *Controller) runWorker() {
	for c.processNextItemInQueue() {
	}
}

func (c *Controller) processNextItemInQueue() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.queue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.queue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.queue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.queue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
	}
	return true
}

// Reconcile implements the Reconciler interface.  This function contains the business logic of the
// OBC Controller.  Currently, the process strictly serves as a POC for an OBC Controller and is
// extremely fragile.
func (c *Controller) syncHandler(key string) error {

	setLoggersWithRequest(key)

	logD.Info("new Reconcile iteration")

	obc, err := claimForKey(key, c.libClientset)

	/**************************
	 Delete or Revoke Bucket
	***************************/
	if err != nil {
		// the OBC was deleted or some other error
		log.Info("error getting claim")
		if errors.IsNotFound(err) {
			log.Info("looks like the OBC was deleted, proceeding with cleanup")
			err = c.handleDeleteClaim(key)
			if err != nil {
				log.Error(err, "error cleaning up ObjectBucket: %v")
			}
			return err
		}
		return fmt.Errorf("error getting claim for request key %q", key)
	}

	/*******************************************************
	 Provision New Bucket or Grant Access to Existing Bucket
	********************************************************/
	if !shouldProvision(obc) {
		log.Info("skipping provision")
		return nil
	}
	class, err := storageClassForClaim(c.clientset, obc)
	if err != nil {
		return err
	}
	if !c.supportedProvisioner(class.Provisioner) {
		log.Info("unsupported provisioner", "got", class.Provisioner)
		return nil
	}

	obc, err = updateObjectBucketClaimPhase(c.libClientset, obc, v1alpha1.ObjectBucketClaimStatusPhasePending, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return err
	}

	// By now, we should know that the OBC matches our provisioner, lacks an OB, and thus requires provisioning
	err = c.handleProvisionClaim(key, obc, class)

	// If handleReconcile() errors, the request will be re-queued.  In the distant future, we will likely want some ignorable error types in order to skip re-queuing
	return err
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (c *Controller) handleProvisionClaim(key string, obc *v1alpha1.ObjectBucketClaim, class *storagev1.StorageClass) error {

	var (
		ob        *v1alpha1.ObjectBucket
		secret    *corev1.Secret
		configMap *corev1.ConfigMap
		err       error
	)

	// If the storage class contains the name of the bucket then we create access
	// to an existing bucket. If the bucket name does not appear in the storage
	// class then we dynamically provision a new bucket.
	isDynamicProvisioning := isNewBucketByClass(class)

	// Following getting the claim, if any provisioning task fails, clean up provisioned artifacts.
	// It is assumed that if the get claim fails, no resources were generated to begin with.
	defer func() {
		if err != nil {
			log.Error(err, "cleaning up reconcile artifacts")
			if !pErr.IsBucketExists(err) && ob != nil && isDynamicProvisioning {
				log.Info("deleting bucket", "name", ob.Spec.Endpoint.BucketName)
				if err = c.provisioner.Delete(ob); err != nil {
					log.Error(err, "error deleting bucket")
				}
			}
			c.deleteResources(ob, configMap, secret)
		}
	}()

	bucketName := class.Parameters[v1alpha1.StorageClassBucket]
	if isDynamicProvisioning {
		bucketName, err = composeBucketName(obc)
		if err != nil {
			return fmt.Errorf("error composing bucket name: %v", err)
		}
	}
	if len(bucketName) == 0 {
		return fmt.Errorf("bucket name missing")
	}

	if !shouldProvision(obc) {
		return nil
	}

	options := &api.BucketOptions{
		ReclaimPolicy:     class.ReclaimPolicy,
		BucketName:        bucketName,
		ObjectBucketClaim: obc.DeepCopy(),
		Parameters:        class.Parameters,
	}

	verb := "provisioning"
	if !isDynamicProvisioning {
		verb = "granting access to"
	}
	logD.Info(verb, "bucket", options.BucketName)

	// Re-Get the claim in order to shorten the race condition where the claim was deleted after Reconcile() started
	obc, err = claimForKey(key, c.libClientset)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("OBC was lost before we could provision: %v", err)
		}
		return err
	}

	if isDynamicProvisioning {
		ob, err = c.provisioner.Provision(options)
	} else {
		ob, err = c.provisioner.Grant(options)
	}
	if err != nil {
		return fmt.Errorf("error %s bucket: %v", verb, err)
	} else if ob == (&v1alpha1.ObjectBucket{}) {
		return fmt.Errorf("provisioner returned nil/empty object bucket")
	}

	setObjectBucketName(ob, key)
	ob.Spec.StorageClassName = obc.Spec.StorageClassName
	ob.Spec.ClaimRef, err = claimRefForKey(key, c.libClientset)
	ob.Spec.ReclaimPolicy = options.ReclaimPolicy
	ob.SetFinalizers([]string{finalizer})

	obc, err = updateObjectBucketClaimPhase(c.libClientset, obc, v1alpha1.ObjectBucketClaimStatusPhaseBound, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return err
	}

	obc.Spec.ObjectBucketName = ob.Name
	obc.Spec.BucketName = bucketName

	if secret, err = createSecret(obc, ob.Spec.Authentication, c.clientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return err
	}

	if configMap, err = createConfigMap(obc, ob.Spec.Endpoint, c.clientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return err
	}

	// NOTE: do not move ob create/update calls before secret or vice versa.  spec.Authentication is lost after create/update, which
	// break secret creation
	if ob, err = createObjectBucket(ob, c.libClientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return err
	}

	ob, err = updateObjectBucketPhase(c.libClientset, ob, v1alpha1.ObjectBucketStatusPhaseBound, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return err
	}

	// Only update the claim if the secret and configMap succeed
	if _, err = updateClaim(c.libClientset, obc, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return err
	}
	log.Info("provisioning succeeded")
	return nil
}

func (c *Controller) handleDeleteClaim(key string) error {

	// TODO each delete should retry a few times to mitigate intermittent errors

	cm, err := configMapForClaimKey(key, c.clientset)
	if err == nil {
		err = deleteConfigMap(cm, c.clientset)
		if err != nil {
			return err
		}
	} else {
		log.Error(err, "could not get configMap")
	}

	secret, err := secretForClaimKey(key, c.clientset)
	if err == nil {
		err = deleteSecret(secret, c.clientset)
		if err != nil {
			return err
		}
	} else {
		log.Error(err, "could not get secret")
	}

	ob, err := c.objectBucketForClaimKey(key)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "objectBucket not found, assuming it was already deleted")
			return nil
		}
		return fmt.Errorf("error getting objectBucket for key: %v", err)
	} else if ob == nil {
		log.Error(nil, "got nil objectBucket, assuming deletion complete")
		return nil
	}

	// Call the provisioner's `Revoke` method for old (brownfield) buckets regardless of reclaimPolicy.
	// Also call `Revoke` for new buckets with a reclaimPolicy other than "Delete".
	if ob.Spec.ReclaimPolicy == nil {
		log.Error(nil, "got null reclaimPolicy", "ob", ob.Name)
		return nil
	}

	ob, err = updateObjectBucketPhase(c.libClientset, ob, v1alpha1.ObjectBucketClaimStatusPhaseReleased, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return err
	}

	// decide whether Delete or Revoke is called
	if isNewBucketByOB(c.clientset, ob) && *ob.Spec.ReclaimPolicy == corev1.PersistentVolumeReclaimDelete {
		if err = c.provisioner.Delete(ob); err != nil {
			// Do not proceed to deleting the ObjectBucket if the deprovisioning fails for bookkeeping purposes
			return fmt.Errorf("provisioner error deleting bucket %v", err)
		}
	} else {
		if err = c.provisioner.Revoke(ob); err != nil {
			return fmt.Errorf("provisioner error revoking access to bucket %v", err)
		}
	}

	if err = deleteObjectBucket(ob, c.libClientset); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "ObjectBucket vanished during deprovisioning, assuming deletion complete")
		} else {
			return fmt.Errorf("error deleting objectBucket %v", ob.Name)
		}
	}
	return nil
}

func (c *Controller) supportedProvisioner(provisioner string) bool {
	return provisioner == c.provisionerName
}

func (c *Controller) objectBucketForClaimKey(key string) (*v1alpha1.ObjectBucket, error) {
	logD.Info("getting objectBucket for key", "key", key)
	name, err := obNameFromClaimKey(key)
	if err != nil {
		return nil, err
	}
	ob, err := c.libClientset.ObjectbucketV1alpha1().ObjectBuckets().Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing object buckets: %v", err)
	}
	return ob, nil
}

func (c *Controller) deleteResources(ob *v1alpha1.ObjectBucket, cm *corev1.ConfigMap, s *corev1.Secret) {
	if err := deleteObjectBucket(ob, c.libClientset); err != nil {
		log.Error(err, "error deleting objectBucket")
	}
	if err := deleteSecret(s, c.clientset); err != nil {
		log.Error(err, "error deleting secret")
	}
	if err := deleteConfigMap(cm, c.clientset); err != nil {
		log.Error(err, "error deleting configMap")
	}
}
