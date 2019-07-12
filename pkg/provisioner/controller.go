/*
Copyright 2019 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioner

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned"
	informers "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/informers/externalversions/objectbucket.io/v1alpha1"
	listers "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/listers/objectbucket.io/v1alpha1"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
	pErr "github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api/errors"
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
		if errors.IsNotFound(err) {
			log.Info("looks like the OBC was deleted, proceeding with cleanup")
			err = c.handleDeleteClaim(key)
			if err != nil {
				log.Error(err, "error cleaning up OBC", "name", key)
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

	// By now, we should know that the OBC matches our provisioner, lacks an OB, and thus requires provisioning
	err = c.handleProvisionClaim(key, obc, class)

	// If handleReconcile() errors, the request will be re-queued.  In the distant future, we will likely want some ignorable error types in order to skip re-queuing
	return err
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (c *Controller) handleProvisionClaim(key string, obc *v1alpha1.ObjectBucketClaim, class *storagev1.StorageClass) (err error) {

	var (
		ob        *v1alpha1.ObjectBucket
		secret    *corev1.Secret
		configMap *corev1.ConfigMap
	)
	obcNsName := obc.Namespace + "/" + obc.Name

	// first step is to update the OBC's status to pending
	if obc, err = updateObjectBucketClaimPhase(c.libClientset, obc, v1alpha1.ObjectBucketClaimStatusPhasePending, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error updating OBC %q's status to %q: %v", obcNsName, v1alpha1.ObjectBucketClaimStatusPhasePending, err)
	}

	// If the storage class contains the name of the bucket then we create access
	// to an existing bucket. If the bucket name does not appear in the storage
	// class then we dynamically provision a new bucket.
	isDynamicProvisioning := isNewBucketByStorageClass(class)

	// Following getting the claim, if any provisioning task fails, clean up provisioned artifacts.
	// It is assumed that if the get claim fails, no resources were generated to begin with.
	defer func() {
		if err != nil {
			log.Error(err, "cleaning up reconcile artifacts")
			if !pErr.IsBucketExists(err) && ob != nil && isDynamicProvisioning {
				log.Info("deleting storage artifacts")
				if err = c.provisioner.Delete(ob); err != nil {
					log.Error(err, "error deleting storage artifacts")
				}
			}
			_ = c.deleteResources(ob, configMap, secret)
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

	// Re-Get the claim in order to shorten the race condition where the claim was deleted after Reconcile() started
	obc, err = claimForKey(key, c.libClientset)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("OBC was lost before we could provision: %v", err)
		}
		return err
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

	// create Secret and ConfigMap
	if secret, err = createSecret(obc, ob.Spec.Authentication, c.clientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error creating secret for OBC %q: %v", obcNsName, err)
	}
	if configMap, err = createConfigMap(obc, ob.Spec.Endpoint, c.clientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error creating configmap for OBC %q: %v", obcNsName, err)
	}

	// Create OB
	// Note: do not move ob create/update calls before secret or vice versa.
	//   spec.Authentication is lost after create/update, which break secret creation
	setObjectBucketName(ob, key)
	ob.Spec.StorageClassName = obc.Spec.StorageClassName
	ob.Spec.ClaimRef, err = claimRefForKey(key, c.libClientset)
	ob.Spec.ReclaimPolicy = options.ReclaimPolicy
	ob.SetFinalizers([]string{finalizer})

	if ob, err = createObjectBucket(ob, c.libClientset, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error creating OB %q: %v", ob.Name, err)
	}
	if ob, err = updateObjectBucketPhase(c.libClientset, ob, v1alpha1.ObjectBucketStatusPhaseBound, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error updating OB %q's status to %q:", ob.Name, v1alpha1.ObjectBucketStatusPhaseBound, err)
	}

	// update OBC
	obc.Spec.ObjectBucketName = ob.Name
	obc.Spec.BucketName = bucketName

	if obc, err = updateClaim(c.libClientset, obc, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error updating OBC %q: %v", obcNsName, err)
	}
	if obc, err = updateObjectBucketClaimPhase(c.libClientset, obc, v1alpha1.ObjectBucketClaimStatusPhaseBound, defaultRetryBaseInterval, defaultRetryTimeout); err != nil {
		return fmt.Errorf("error updating OBC %q's status to %q: %v", obcNsName, v1alpha1.ObjectBucketClaimStatusPhaseBound, err)
	}

	log.Info("provisioning succeeded")
	return nil
}

// Delete or Revoke access to bucket defined by passed-in key.
// TODO each delete should retry a few times to mitigate intermittent errors
func (c *Controller) handleDeleteClaim(key string) error {
	// Call `Delete` for new (greenfield) buckets with reclaimPolicy == "Delete".
	// Call `Revoke` for new buckets with reclaimPolicy != "Delete".
	// Call `Revoke` for existing (brownfield) buckets regardless of reclaimPolicy.

	ob, cm, secret, err := c.getResourcesFromKey(key)
	if err != nil {
		return err
	}

	// ob or cm or secret (or 2 of the 3) can be nil, but Delete/Revoke cannot be called
	// if the ob is nil. However if the secret and/or cm != nil we can delete them.
	if ob == nil {
		log.Error(nil, "nil ObjectBucket, assuming it has been deleted")
		return c.deleteResources(nil, cm, secret)
	}
	if ob.Spec.ReclaimPolicy == nil {
		log.Error(nil, "missing reclaimPolicy", "ob", ob.Name)
		return nil
	}

	// call Delete or Revoke and then delete generated k8s resources
	// Note: if Delete or Revoke return err then we do not try to delete resources
	ob, err = updateObjectBucketPhase(c.libClientset, ob, v1alpha1.ObjectBucketClaimStatusPhaseReleased, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return err
	}

	// decide whether Delete or Revoke is called
	if isNewBucketByObjectBucket(c.clientset, ob) && *ob.Spec.ReclaimPolicy == corev1.PersistentVolumeReclaimDelete {
		if err = c.provisioner.Delete(ob); err != nil {
			// Do not proceed to deleting the ObjectBucket if the deprovisioning fails for bookkeeping purposes
			return fmt.Errorf("provisioner error deleting bucket %v", err)
		}
	} else {
		if err = c.provisioner.Revoke(ob); err != nil {
			return fmt.Errorf("provisioner error revoking access to bucket %v", err)
		}
	}

	return c.deleteResources(ob, cm, secret)
}

func (c *Controller) supportedProvisioner(provisioner string) bool {
	return provisioner == c.provisionerName
}

// Returns the ob, configmap, and secret based on the passed-in key. Only returns non-nil
// error if unable to get all resources. Some resources may be nil.
func (c *Controller) getResourcesFromKey(key string) (*v1alpha1.ObjectBucket, *corev1.ConfigMap, *corev1.Secret, error) {

	ob, obErr := c.objectBucketForClaimKey(key)
	if errors.IsNotFound(obErr) {
		log.Error(obErr, "objectBucket not found")
		obErr = nil
	}
	cm, cmErr := configMapForClaimKey(key, c.clientset)
	if errors.IsNotFound(cmErr) {
		log.Error(cmErr, "configMap not found")
		cmErr = nil
	}
	s, sErr := secretForClaimKey(key, c.clientset)
	if errors.IsNotFound(sErr) {
		log.Error(sErr, "secret not found")
		sErr = nil
	}

	var err error
	// return err if all resources were not retrieved, else no error
	if obErr != nil && cmErr != nil && sErr != nil {
		err = fmt.Errorf("could not get all needed resources: %v : %v : %v", obErr, cmErr, sErr)
	}

	return ob, cm, s, err
}

// Deleting the resources generated by a Provision or Grant call is triggered by the delete of
// the OBC. Since the secret and configmap's ownerReference is the OBC they will be garbage
// collected by k8s once their finalizers are removed. However, the OB must be explicitly
// deleted since it is a global resource and cannot have a namespaced ownerReference.
// Returns err if we can't delete one or more of the resources. The final error is somewhat arbitrary.
func (c *Controller) deleteResources(ob *v1alpha1.ObjectBucket, cm *corev1.ConfigMap, s *corev1.Secret) (err error) {
	if ob != nil {
		if delErr := deleteObjectBucket(ob, c.libClientset); delErr != nil {
			log.Error(delErr, "error deleting objectBucket", ob.Namespace+"/"+ob.Name)
			err = delErr
		}
	}
	if s != nil {
		if delErr := releaseSecret(s, c.clientset); delErr != nil {
			log.Error(delErr, "error releasing secret", s.Namespace+"/"+s.Name)
			err = delErr
		}
	}
	if cm != nil {
		if delErr := releaseConfigMap(cm, c.clientset); delErr != nil {
			log.Error(delErr, "error releasing configMap", cm.Namespace+"/"+cm.Name)
			err = delErr
		}
	}

	return err
}
