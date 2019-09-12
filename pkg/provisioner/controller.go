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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	SetLabels(map[string]string)
}

// Provisioner is a CRD Controller responsible for executing the Reconcile() function
// in response to OBC events.
type obcController struct {
	clientset    kubernetes.Interface
	libClientset versioned.Interface
	obcLister    listers.ObjectBucketClaimLister
	obLister     listers.ObjectBucketLister
	obcInformer  informers.ObjectBucketClaimInformer
	obcHasSynced cache.InformerSynced
	obHasSynced  cache.InformerSynced
	queue        workqueue.RateLimitingInterface
	// static label containing provisioner name and provisioner-specific labels which are all added
	// to the OB, OBC, configmap and secret
	provisionerLabels map[string]string
	provisioner       api.Provisioner
	provisionerName   string
}

var _ controller = &obcController{}

func NewController(provisionerName string, provisioner api.Provisioner, clientset kubernetes.Interface, crdClientSet versioned.Interface, obcInformer informers.ObjectBucketClaimInformer, obInformer informers.ObjectBucketInformer) *obcController {
	ctrl := &obcController{
		clientset:    clientset,
		libClientset: crdClientSet,
		obcLister:    obcInformer.Lister(),
		obLister:     obInformer.Lister(),
		obcInformer:  obcInformer,
		obcHasSynced: obcInformer.Informer().HasSynced,
		obHasSynced:  obInformer.Informer().HasSynced,
		queue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		provisionerLabels: map[string]string{
			provisionerLabelKey: labelValue(provisionerName),
		},
		provisionerName: provisionerName,
		provisioner:     provisioner,
	}

	obcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ctrl.enqueueOBC,
		UpdateFunc: func(old, new interface{}) {
			oldObc := old.(*v1alpha1.ObjectBucketClaim)
			newObc := new.(*v1alpha1.ObjectBucketClaim)
			if newObc.ResourceVersion == oldObc.ResourceVersion {
				// periodic re-sync can be ignored
				return
			}
			// if old and new both have deletionTimestamps we can also ignore the
			// update since these events are occurring on an obc marked for deletion,
			// eg. extra finalizers being added and deleted.
			if newObc.ObjectMeta.DeletionTimestamp != nil && oldObc.ObjectMeta.DeletionTimestamp != nil {
				return
			}
			// handle this update
			ctrl.enqueueOBC(new)
		},
		DeleteFunc: func(obj interface{}) {
			// Since a finalizer is added to the obc and thus the obc will remain
			// visible, we do not need to handle delete events here. Instead, obc
			// deletes are indicated by the deletionTimestamp being non-nil.
			return
		},
	})
	return ctrl
}

func (c *obcController) Start(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, c.obcHasSynced, c.obHasSynced) {
		return fmt.Errorf("failed to waith for caches to sync ")
	}
	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
	return nil
}

// add provisioner-specific labels to the existing static label in the obcController struct.
func (c *obcController) SetLabels(labels map[string]string) {
	for k, v := range labels {
		c.provisionerLabels[k] = v
	}
}

func (c *obcController) enqueueOBC(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.queue.AddRateLimited(key)
}

func (c *obcController) runWorker() {
	for c.processNextItemInQueue() {
	}
}

func (c *obcController) processNextItemInQueue() bool {
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
		// more up to date than when the item was initially put onto the
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

// Reconcile implements the Reconciler interface. This function contains the business logic
// of the OBC obcController.
// Note: the obc obtained from the key is not expected to be nil. In other words, this func is
//   not called when informers detect an object is missing and trigger a formal delete event.
//   Instead, delete is indicated by the deletionTimestamp being non-nil on an update event.
func (c *obcController) syncHandler(key string) error {

	setLoggersWithRequest(key)
	logD.Info("new Reconcile iteration")

	obc, err := claimForKey(key, c.libClientset)
	if err != nil {
		return fmt.Errorf("request key %q: %v", key, err)
	}
	if obc == nil {
		return fmt.Errorf("unexpected nil obc for request key %q", key)
	}

	deleteEvent := obc.ObjectMeta.DeletionTimestamp != nil

	if deleteEvent {
		// ***********************
		// Delete or Revoke Bucket
		// ***********************
		log.Info("OBC deleted, proceeding with cleanup")
		err = c.handleDeleteClaim(key, obc)
		if err != nil {
			log.Error(err, "error cleaning up OBC", "name", key)
		}
		return err
	}

	// *******************************************************
	// Provision New Bucket or Grant Access to Existing Bucket
	// *******************************************************
	if !shouldProvision(obc) {
		log.Info("skipping provision")
		return nil
	}

	// update the OBC's status to pending before any provisioning related errors can occur
	obc, err = updateObjectBucketClaimPhase(
		c.libClientset,
		obc,
		v1alpha1.ObjectBucketClaimStatusPhasePending,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error updating OBC status:", err)
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
func (c *obcController) handleProvisionClaim(key string, obc *v1alpha1.ObjectBucketClaim, class *storagev1.StorageClass) (err error) {

	var (
		ob        *v1alpha1.ObjectBucket
		secret    *corev1.Secret
		configMap *corev1.ConfigMap
	)

	// set finalizer in OBC so that resources cleaned up is controlled when the obc is deleted
	if err = c.setOBCMetaFields(obc); err != nil {
		return err
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
			_ = c.deleteResources(ob, configMap, secret, nil)
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
	secret, err = createSecret(
		obc,
		ob.Spec.Authentication,
		c.provisionerLabels,
		c.clientset,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error creating secret for OBC: %v", err)
	}
	configMap, err = createConfigMap(
		obc,
		ob.Spec.Endpoint,
		c.provisionerLabels,
		c.clientset,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error creating configmap for OBC: %v", err)
	}

	// Create OB
	// Note: do not move ob create/update calls before secret or vice versa.
	//   spec.Authentication is lost after create/update, which break secret creation
	setObjectBucketName(ob, key)
	ob.Spec.StorageClassName = obc.Spec.StorageClassName
	ob.Spec.ClaimRef, err = claimRefForKey(key, c.libClientset)
	ob.Spec.ReclaimPolicy = options.ReclaimPolicy
	ob.SetFinalizers([]string{finalizer})
	ob.SetLabels(c.provisionerLabels)

	ob, err = createObjectBucket(
		ob,
		c.libClientset,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error creating OB %q: %v", ob.Name, err)
	}
	ob, err = updateObjectBucketPhase(
		c.libClientset,
		ob,
		v1alpha1.ObjectBucketStatusPhaseBound,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error updating OB %q's status to %q: %v", ob.Name, v1alpha1.ObjectBucketStatusPhaseBound, err)
	}

	// update OBC
	obc.Spec.ObjectBucketName = ob.Name
	obc.Spec.BucketName = bucketName
	obc, err = updateClaim(
		c.libClientset,
		obc,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error updating OBC: %v", err)
	}
	obc, err = updateObjectBucketClaimPhase(
		c.libClientset,
		obc,
		v1alpha1.ObjectBucketClaimStatusPhaseBound,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error updating OBC %q's status to: %v", v1alpha1.ObjectBucketClaimStatusPhaseBound, err)
	}

	log.Info("provisioning succeeded")
	return nil
}

// Delete or Revoke access to bucket defined by passed-in key and obc.
// TODO each delete should retry a few times to mitigate intermittent errors
func (c *obcController) handleDeleteClaim(key string, obc *v1alpha1.ObjectBucketClaim) error {
	// Call `Delete` for new (greenfield) buckets with reclaimPolicy == "Delete".
	// Call `Revoke` for new buckets with reclaimPolicy != "Delete".
	// Call `Revoke` for existing (brownfield) buckets regardless of reclaimPolicy.
	ob, cm, secret, err := c.getResourcesFromKey(key)
	if err != nil {
		return err
	}

	// Delete/Revoke cannot be called if the ob is nil; however, if the secret
	// and/or cm != nil we can delete them
	if ob == nil {
		log.Error(nil, "nil ObjectBucket, assuming it has been deleted")
		return c.deleteResources(nil, cm, secret, obc)
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

	return c.deleteResources(ob, cm, secret, obc)
}

func (c *obcController) supportedProvisioner(provisioner string) bool {
	return provisioner == c.provisionerName
}

// Returns the ob, configmap, and secret based on the passed-in key. Only returns non-nil
// error if unable to get all resources. Some resources may be nil.
func (c *obcController) getResourcesFromKey(key string) (*v1alpha1.ObjectBucket, *corev1.ConfigMap, *corev1.Secret, error) {

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
// the OBC. However, a finalizer is added to the OBC so that we can cleanup up the other resources
// created by a Provision or Grant call. Since the secret and configmap's ownerReference is the OBC
// they will be garbage collected once their finalizers are removed. The OB must be explicitly
// deleted since it is a global resource and cannot have a namespaced ownerReference. The last step
// is to remove the finalizer on the OBC so it too will be garbage collected.
// Returns err if we can't delete one or more of the resources, the final returned error being
// somewhat arbitrary.
func (c *obcController) deleteResources(ob *v1alpha1.ObjectBucket, cm *corev1.ConfigMap, s *corev1.Secret, obc *v1alpha1.ObjectBucketClaim) (err error) {

	if delErr := deleteObjectBucket(ob, c.libClientset); delErr != nil {
		log.Error(delErr, "error deleting objectBucket", ob.Name)
		err = delErr
	}
	if delErr := releaseSecret(s, c.clientset); delErr != nil {
		log.Error(delErr, "error releasing secret")
		err = delErr
	}
	if delErr := releaseConfigMap(cm, c.clientset); delErr != nil {
		log.Error(delErr, "error releasing configMap")
		err = delErr
	}
	if delErr := releaseOBC(obc, c.libClientset); delErr != nil {
		log.Error(delErr, "error releasing obc")
		err = delErr
	}
	return err
}

// Add finalizer and labels to the OBC.
func (c *obcController) setOBCMetaFields(obc *v1alpha1.ObjectBucketClaim) (err error) {
	clib := c.libClientset

	logD.Info("getting OBC to set metadata fields")
	obc, err = clib.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Get(obc.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error getting obc: %v", err)
	}

	obc.SetFinalizers([]string{finalizer})
	obc.SetLabels(c.provisionerLabels)

	logD.Info("updating OBC metadata")
	obc, err = updateClaim(clib, obc, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error configuring obc metadata: %v", err)
	}

	return nil
}

func (c *obcController) objectBucketForClaimKey(key string) (*v1alpha1.ObjectBucket, error) {
	logD.Info("getting objectBucket for key", "key", key)
	name, err := objectBucketNameFromClaimKey(key)
	if err != nil {
		return nil, err
	}
	ob, err := c.libClientset.ObjectbucketV1alpha1().ObjectBuckets().Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting object bucket %q: %v", name, err)
	}
	return ob, nil
}
