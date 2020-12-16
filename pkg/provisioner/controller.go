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
	"context"
	"fmt"
	"os"
	"strconv"
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
	count := 1
	if threadiness, set := os.LookupEnv("LIB_BUCKET_PROVISIONER_THREADS"); set {
		count, _ = strconv.Atoi(threadiness)
	}
	for i := 0; i < count; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
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
	logD.Info("reconciling claim")

	obc, err := claimForKey(key, c.libClientset)
	if err != nil {
		//      The OBC was deleted immediately after creation, before it could be processed by
		//      handleProvisionClaim.  As a finalizer is immediately applied to the OBC before processing,
		//      if it does not have a finalizer, it was not processed, and no artifacts were created.
		//      Therefore, it is safe to assume nothing needs to be done.
		if errors.IsNotFound(err) {
			log.Info("OBC vanished, assuming it was deleted")
			return nil
		}
		return fmt.Errorf("could not sync OBC %s: %v", key, err)
	}

	class, err := storageClassForClaim(c.clientset, obc)
	if err != nil {
		return err
	}
	if !c.supportedProvisioner(class.Provisioner) {
		log.Info("unsupported provisioner", "got", class.Provisioner)
		return nil
	}

	// ***********************
	// Delete or Revoke Bucket
	// ***********************
	if obc.ObjectMeta.DeletionTimestamp != nil {
		log.Info("OBC deleted, proceeding with cleanup")
		return c.handleDeleteClaim(key, obc)
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
		return fmt.Errorf("error updating OBC status: %s", err)
	}

	// By now, we should know that the OBC matches our provisioner, lacks an OB, and thus requires provisioning
	err = c.handleProvisionClaim(key, obc, class)

	// If the handler above errors, the request will be re-queued. In the distant future, we will
	// likely want some ignorable error types in order to skip re-queuing
	return err
}

// handleProvision is an extraction of the core provisioning process in order to defer clean up
// on a provisioning failure
func (c *obcController) handleProvisionClaim(key string, obc *v1alpha1.ObjectBucketClaim, class *storagev1.StorageClass) error {

	log.Info("syncing obc creation")

	var (
		ob        *v1alpha1.ObjectBucket
		secret    *corev1.Secret
		configMap *corev1.ConfigMap
		err       error
	)

	// set finalizer in OBC so that resources cleaned up is controlled when the obc is deleted
	if err = c.setOBCMetaFields(obc); err != nil {
		return err
	}

	// If a storage class contains a non-nil value for the "bucketName" key, it is assumed
	// to be a Grant request to the given bucket (brownfield).  If the value is nil or the
	// key is undefined, it is assumed to be a provisioning request.  This allows administrators
	// to control access to static buckets via RBAC rules on storage classes.
	isDynamicProvisioning := isNewBucketByStorageClass(class)

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

	// In the case where a bucket name is being generated, generate the name and store it in the OBC
	// spec before doing any Provisioning so that any crashes encountered in this code will not
	// result in multiple buckets being generated for the same OBC. bucketName takes precedence over
	// generateBucketName if both are present.
	if obc.Spec.BucketName == "" {
		obc.Spec.BucketName = bucketName
		obc, err = updateClaim(
			c.libClientset,
			obc,
			defaultRetryBaseInterval,
			defaultRetryTimeout)
		if err != nil {
			return fmt.Errorf("error updating OBC %q with bucket name: %v", key, err)
		}
	}

	updateOnSuccess := func() error {
		// In the case where the OB has been created and the operator crashed immediately after
		// (before the OB phase could be updated), we should update the OB phase in addition to
		// updating the OBC reference to the OB and the OBC's phase.
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
		return nil
	}

	// recover from degraded state where provisioning is happening again on a completed obc
	ob, err = getObjectBucketFromClaimKey(key, obc, c.libClientset, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error getting OB for OBC %q: %v", key, err)
	}
	if ob != nil {
		err = updateOnSuccess()
		if err != nil {
			return fmt.Errorf("error marking OB %q as bound to OBC %q: %v", ob.Name, key, err)
		}
		// Do not do any more provisioning; the bucket is already provisioned and merely
		// needed its info re-updated to reference the claim and vice versa
		logD.Info("(re-)bound OB to OBC successfully", "OB", ob.Name)
		return nil
	}

	options := &api.BucketOptions{
		ReclaimPolicy:     class.ReclaimPolicy,
		BucketName:        bucketName,
		ObjectBucketClaim: obc.DeepCopy(),
		Parameters:        class.Parameters,
	}

	// Fill in basic necessary object bucket info
	setBasicOBInfo := func() {
		setObjectBucketName(ob, key)
		ob.Spec.StorageClassName = obc.Spec.StorageClassName
		if ob.Spec.ReclaimPolicy == nil || *ob.Spec.ReclaimPolicy == corev1.PersistentVolumeReclaimPolicy("") {
			// Do not blindly overwrite the reclaim policy. The provisioner might have reason to
			// specify a reclaim policy that is  different from the storage class.
			ob.Spec.ReclaimPolicy = options.ReclaimPolicy
		}
		ob.SetLabels(c.provisionerLabels)
	}

	// Should an error be returned, attempt to clean up the object store and API servers by
	// calling the appropriate provisioner method.  In cases where Provision() or Revoke()
	// return an err, it's likely that the ob == nil, hindering cleanup.
	defer func() {
		if err != nil && ob != nil {
			// If provisioning fails before basic OB info is applied, cleanup below can fail because
			// provisioner methods might not have access to information (like StorageClassName)
			// they need to properly clean up. Therefore, we must set basic OB info here.
			setBasicOBInfo()
			log.Info("cleaning up provisioning artifacts")
			if /*greenfield*/ isDynamicProvisioning && !pErr.IsBucketExists(err) {
				log.Info("deleting provisioned resources")
				if dErr := c.provisioner.Delete(ob); dErr != nil {
					log.Error(dErr, "could not delete provisioned resources")
				}
			} else /*brownfield*/ {
				log.Info("revoking access")
				if dErr := c.provisioner.Revoke(ob); dErr != nil {
					log.Error(err, "could not revoke access")
				}
			}
			_ = c.deleteResources(ob, configMap, secret, nil)
		}
	}()

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
		return fmt.Errorf("provisioner returned empty object bucket")
	}

	// Fill in known information in the object bucket struct as soon as possible
	setBasicOBInfo()
	ob.SetFinalizers([]string{finalizer})
	ob.Spec.ClaimRef, err = claimRefForKey(key, c.libClientset)
	if err != nil {
		return fmt.Errorf("error getting reference to OBC: %v", err)
	}

	// Create auth Secret and bucket info ConfigMap
	// If the operator crashes before the OB is created, provisioning will happen again including a
	// call to Provision()/Grant(), and a Secret and/or ConfigMap might exist from the previous
	// provisioning attempt. Provision() isn't guaranteed to give the same credentials or bucket
	// each time, so if the Secret/ConfigMap already exists, delete the old one and re-create it
	// with the latest info.

	err = deleteExistingSecretAndConfigMapIfExist(obc, c.clientset, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("failed to delete existing Secret and/or ConfigMap for OBC %q: %v", obc.Namespace+"/"+obc.Name, err)
	}

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
	// Note: do not move ob create/update calls before secret.
	//   spec.Authentication is lost after create/update, which breaks secret creation
	ob, err = createObjectBucket(
		ob,
		c.libClientset,
		defaultRetryBaseInterval,
		defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error creating OB %q: %v", ob.Name, err)
	}

	err = updateOnSuccess()
	if err != nil {
		return fmt.Errorf("error marking new OB %q as bound to OBC %q: %v", ob.Name, key, err)
	}

	log.Info("provisioning succeeded")
	return nil
}

// Delete or Revoke access to bucket defined by passed-in key and obc.
func (c *obcController) handleDeleteClaim(key string, obc *v1alpha1.ObjectBucketClaim) error {
	// Call `Delete` for new (greenfield) buckets with reclaimPolicy == "Delete".
	// Call `Revoke` for new buckets with reclaimPolicy != "Delete".
	// Call `Revoke` for existing (brownfield) buckets regardless of reclaimPolicy.

	log.Info("syncing obc deletion")

	ob, cm, secret, errs := c.getExistingResourcesFromKey(key)
	if len(errs) > 0 {
		return fmt.Errorf("error getting resources: %v", errs)
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
	ob, err := updateObjectBucketPhase(c.libClientset, ob, v1alpha1.ObjectBucketClaimStatusPhaseReleased, defaultRetryBaseInterval, defaultRetryTimeout)
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

// trim the errors resulting from objects not being found
func (c *obcController) getExistingResourcesFromKey(key string) (*v1alpha1.ObjectBucket, *corev1.ConfigMap, *corev1.Secret, []error) {
	ob, cm, secret, errs := c.getResourcesFromKey(key)
	for i := len(errs) - 1; i >= 0; i-- {
		if errors.IsNotFound(errs[i]) {
			errs = append(errs[:i], errs[i+1:]...)
		}
	}
	return ob, cm, secret, errs
}

// Gathers resources by names derived from key.
// Returns pointers to those resources if they exist, nil otherwise and an slice of errors who's
// len() == n errors. If no errors occur, len() is 0.
func (c *obcController) getResourcesFromKey(key string) (ob *v1alpha1.ObjectBucket, cm *corev1.ConfigMap, sec *corev1.Secret, errs []error) {

	var err error
	// The cap(errs) must be large enough to encapsulate errors returned by all 3 *ForClaimKey funcs
	errs = make([]error, 0, 3)
	groupErrors := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	ob, err = c.objectBucketForClaimKey(key)
	groupErrors(err)
	cm, err = configMapForClaimKey(key, c.clientset)
	groupErrors(err)
	sec, err = secretForClaimKey(key, c.clientset)
	groupErrors(err)

	return
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
	obc, err = clib.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Get(context.TODO(), obc.Name, metav1.GetOptions{})
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
	ob, err := c.libClientset.ObjectbucketV1alpha1().ObjectBuckets().Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return ob, nil
}
