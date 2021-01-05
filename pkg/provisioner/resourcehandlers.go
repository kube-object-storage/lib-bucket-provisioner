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
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned"
	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/provisioner/api"
)

const (
	// defaultRetryBaseInterval controls how long to wait for a single create API object call
	defaultRetryBaseInterval = time.Second * 3
	// defaultRetryTimeout defines how long in total to try to create an API object before ending the reconciliation
	// attempt
	defaultRetryTimeout = time.Second * 30

	bucketName      = "BUCKET_NAME"
	bucketHost      = "BUCKET_HOST"
	bucketPort      = "BUCKET_PORT"
	bucketRegion    = "BUCKET_REGION"
	bucketSubRegion = "BUCKET_SUBREGION"
	// finalizer is applied to all resources generated by the provisioner and to the obc
	finalizer = api.Domain + "/finalizer"
	// label applied to all resources generated by the provisioner and to the obc
	provisionerLabelKey    = "bucket-provisioner"
	objectBucketNameFormat = "obc-%s-%s"
)

// newBucketConfigMap returns a config map from a given endpoint and ObjectBucketClaim.
// A finalizer is added to reduce chances of the CM being accidentally deleted. An OwnerReference
// is added so that the CM is automatically garbage collected when the parent OBC is deleted.
func newBucketConfigMap(obc *v1alpha1.ObjectBucketClaim, ep *v1alpha1.Endpoint, labels map[string]string) (*corev1.ConfigMap, error) {
	if ep == nil {
		return nil, fmt.Errorf("cannot construct configMap, got nil Endpoint")
	}
	if obc == nil {
		return nil, fmt.Errorf("cannot construct configMap, got nil OBC")
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       composeConfigMapName(obc),
			Namespace:  obc.Namespace,
			Finalizers: []string{finalizer},
			Labels:     labels,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerReference(obc),
			},
		},
		Data: map[string]string{
			bucketName:      ep.BucketName,
			bucketHost:      ep.BucketHost,
			bucketPort:      strconv.Itoa(ep.BucketPort),
			bucketRegion:    ep.Region,
			bucketSubRegion: ep.SubRegion,
		},
	}, nil
}

// newCredentialsSecret returns a secret with data appropriate to the supported authenticaion
// method. Even if the values for the Authentication keys are empty, we generate the secret.
// A finalizer is added to reduce chances of the secret being accidentally deleted.
// An OwnerReference is added so that the secret is automatically garbage collected when the
// parent OBC is deleted.
func newCredentialsSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication, labels map[string]string) (*corev1.Secret, error) {
	if obc == nil {
		return nil, fmt.Errorf("ObjectBucketClaim required to generate secret")
	}
	if auth == nil {
		return nil, fmt.Errorf("got nil authentication, nothing to do")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       composeSecretName(obc),
			Namespace:  obc.Namespace,
			Finalizers: []string{finalizer},
			Labels:     labels,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerReference(obc),
			},
		},
	}

	secret.StringData = auth.ToMap()
	return secret, nil
}

// createObjectBucket creates an OB based on the passed-in ob spec.
// Note: a finalizer has been added to reduce chances of the ob being accidentally deleted.
func createObjectBucket(ob *v1alpha1.ObjectBucket, c versioned.Interface, retryInterval, retryTimeout time.Duration) (result *v1alpha1.ObjectBucket, err error) {
	logD.Info("creating ObjectBucket", "name", ob.Name)

	result, err = c.ObjectbucketV1alpha1().ObjectBuckets().Create(context.TODO(), ob, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// return input ob here since result is nil on error returns
			return ob, nil
		}
		return ob, fmt.Errorf("failed to create OB %s: %v", ob.Name, err)
	}
	return result, err
}

func updateObjectBucket(ob *v1alpha1.ObjectBucket, c versioned.Interface) (result *v1alpha1.ObjectBucket, err error) {
	logD.Info("updating ObjectBucket", "name", ob.Name)
	result, err = c.ObjectbucketV1alpha1().ObjectBuckets().Update(context.TODO(), ob, metav1.UpdateOptions{})
	if err != nil {
		// return input ob here since result is nil on error returns
		return ob, fmt.Errorf("failed to update OB %s: %v", ob.Name, err)
	}
	return result, err
}

func getObjectBucketFromClaimKey(key string, obc *v1alpha1.ObjectBucketClaim, c versioned.Interface, retryInterval, retryTimeout time.Duration) (ob *v1alpha1.ObjectBucket, err error) {
	obName, err := objectBucketNameFromClaimKey(key)
	if err != nil {
		return nil, err
	}

	logD.Info("seeing if OB for OBC exists", "checking for OB name", obName)
	ob, err = c.ObjectbucketV1alpha1().ObjectBuckets().Get(context.TODO(), obName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Not found is a valid exit here which returns no error and a nil result
			return nil, nil
		}
		return nil, err
	}

	if bucketIsOwnedByClaim(obc, ob) {
		logD.Info("found OB belonging to this OBC", "OB", ob.Name)
		return ob, err
	}

	emptyRef := corev1.ObjectReference{}
	if ob.Spec.ClaimRef == nil || *ob.Spec.ClaimRef == emptyRef {
		// If the CRD validation for the OB improperly specifies the object reference field, it will
		// be missing. Do our best to recover from this case gracefully.
		logD.Info("found OB matching OBC, but the OB ClaimRef is empty. binding OB to OBC", "OB", ob.Name)
		ob.Spec.ClaimRef, err = claimRefForKey(key, c)
		if err != nil {
			return nil, fmt.Errorf("error generating OB %q's reference to OBC: %v", ob.Name, err)
		}
		ob, err = updateObjectBucket(ob, c)
		if err != nil {
			return nil, fmt.Errorf("error updating OB %q with assumed claim ref: %v", ob.Name, err)
		}
		return ob, err
	}

	return nil, fmt.Errorf("found OB %q not owned by OBC %q, likely an artifact from a previous OBC that did not get cleaned up, user must delete the OB in order to allow OBC provisioning to continue", ob.Name, key)
}

func createSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication, labels map[string]string, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) (*corev1.Secret, error) {
	secret, err := newCredentialsSecret(obc, auth, labels)
	if err != nil {
		return nil, err
	}
	logD.Info("creating Secret", "name", secret.Namespace+"/"+secret.Name)
	err = wait.PollImmediate(retryInterval, retryTimeout, func() (done bool, err error) {
		secret, err = c.CoreV1().Secrets(obc.Namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// The object already exists don't spam the logs, instead let the request be requeued
				return true, err
			}
			// The error could be intermittent, log and try again
			log.Error(err, "probably not fatal, retrying")
			return false, nil
		}
		return true, nil
	})
	return secret, err
}

func createConfigMap(obc *v1alpha1.ObjectBucketClaim, ep *v1alpha1.Endpoint, labels map[string]string, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) (*corev1.ConfigMap, error) {
	configMap, err := newBucketConfigMap(obc, ep, labels)
	if err != nil {
		return nil, err
	}

	logD.Info("creating ConfigMap", "name", configMap.Namespace+"/"+configMap.Name)
	err = wait.PollImmediate(retryInterval, retryTimeout, func() (done bool, err error) {
		configMap, err = c.CoreV1().ConfigMaps(obc.Namespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// The object already exists don't spam the logs, instead let the request be requeued
				return true, err
			}
			// The error could be intermittent, log and try again
			log.Error(err, "probably not fatal, retrying")
			return false, nil
		}
		return true, nil
	})
	return configMap, err
}

func deleteExistingSecretAndConfigMapIfExist(obc *v1alpha1.ObjectBucketClaim, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) error {

	secret, err := getExistingSecret(obc, c, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error checking for existing OBC credentials Secret: %v", err)
	}
	if secret != nil {
		if !objectIsOwnedByClaim(obc, secret.OwnerReferences) {
			return fmt.Errorf("found OBC credentials Secret %q not owned by OBC, assuming this is a user-created resource, user must delete the Secret in order to allow OBC provisioning to continue", secret.Name)
		}
		logD.Info("found OBC credentials Secret owned by OBC, deleting and re-creating")
		err = deleteSecretAndWait(secret, c, defaultRetryBaseInterval, defaultRetryTimeout)
		if err != nil {
			return fmt.Errorf("failed to delete existing OBC credentials Secret in order to re-create: %v", err)
		}
	}

	configMap, err := getExistingConfigMap(obc, c, defaultRetryBaseInterval, defaultRetryTimeout)
	if err != nil {
		return fmt.Errorf("error checking for existing OBC bucket info ConfigMap: %v", err)
	}
	if configMap != nil {
		if !objectIsOwnedByClaim(obc, configMap.OwnerReferences) {
			return fmt.Errorf("found OBC bucket info ConfigMap %q not owned by OBC, assuming this is a user-created resource, user must delete the ConfigMap in order to allow OBC provisioning to continue", configMap.Name)
		}
		logD.Info("found OBC bucket info ConfigMap owned by OBC, deleting and re-creating")
		err = deleteConfigMapAndWait(configMap, c, defaultRetryBaseInterval, defaultRetryTimeout)
		if err != nil {
			return fmt.Errorf("failed to delete existing OBC bucket info ConfigMap in order to re-create: %v", err)
		}
	}

	return nil
}

func getExistingSecret(obc *v1alpha1.ObjectBucketClaim, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) (secret *corev1.Secret, err error) {
	secret, err = c.CoreV1().Secrets(obc.Namespace).Get(context.TODO(), composeSecretName(obc), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Not found is valid here which returns no error and a nil secret
			return nil, nil
		}
		return nil, err
	}
	return secret, err
}

func getExistingConfigMap(obc *v1alpha1.ObjectBucketClaim, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) (configMap *corev1.ConfigMap, err error) {
	configMap, err = c.CoreV1().ConfigMaps(obc.Namespace).Get(context.TODO(), composeConfigMapName(obc), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Not found is valid here which returns no error and a nil configMap
			return nil, nil
		}
		return nil, err
	}
	return configMap, err
}

func deleteSecretAndWait(sec *corev1.Secret, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) error {
	logD.Info("deleting secret", "secret name", sec.Name)

	err := releaseSecret(sec, c)
	if err != nil {
		return fmt.Errorf("failed to release secret for deletion: %v", err)
	}
	err = c.CoreV1().Secrets(sec.Namespace).Delete(context.TODO(), sec.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	err = wait.PollImmediate(retryInterval, retryTimeout, func() (done bool, err error) {
		_, err = c.CoreV1().Secrets(sec.Namespace).Get(context.TODO(), sec.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Is Deleted
				err = nil
				return true, nil
			}
			// for other errors, give the opportunity to retry until timeout
			return false, nil
		}
		return false, nil // still exists
	})
	return err
}

func deleteConfigMapAndWait(cm *corev1.ConfigMap, c kubernetes.Interface, retryInterval, retryTimeout time.Duration) error {
	logD.Info("deleting configmap", "configmap name", cm.Name)

	err := releaseConfigMap(cm, c)
	if err != nil {
		return fmt.Errorf("failed to release ConfigMap for deletion: %v", err)
	}
	err = c.CoreV1().ConfigMaps(cm.Namespace).Delete(context.TODO(), cm.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}
	err = wait.PollImmediate(retryInterval, retryTimeout, func() (done bool, err error) {
		_, err = c.CoreV1().ConfigMaps(cm.Namespace).Get(context.TODO(), cm.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				// Is Deleted
				err = nil
				return true, nil
			}
			// for other errors, give the opportunity to retry until timeout
			return false, nil
		}
		return false, nil // still exists
	})
	return err
}

// Only the finalizer needs to be removed. The CM will be garbage collected since its
// ownerReference refers to the parent OBC.
func releaseConfigMap(cm *corev1.ConfigMap, c kubernetes.Interface) (err error) {
	if cm == nil {
		logD.Info("got nil configmap, skipping")
		return nil
	}
	cm, err = c.CoreV1().ConfigMaps(cm.Namespace).Get(context.TODO(), cm.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	logD.Info("removing configmap finalizer")
	removeFinalizer(cm)
	cm, err = c.CoreV1().ConfigMaps(cm.Namespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// Only the finalizer needs to be removed. The Secret will be garbage collected since its
// ownerReference refers to the parent OBC.
func releaseSecret(sec *corev1.Secret, c kubernetes.Interface) (err error) {
	if sec == nil {
		logD.Info("got nil secret, skipping")
		return nil
	}
	sec, err = c.CoreV1().Secrets(sec.Namespace).Get(context.TODO(), sec.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	logD.Info("removing secret finalizer")
	removeFinalizer(sec)
	sec, err = c.CoreV1().Secrets(sec.Namespace).Update(context.TODO(), sec, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// Remove the finalizer allowing the OBC to finally be deleted.
func releaseOBC(obc *v1alpha1.ObjectBucketClaim, c versioned.Interface) (err error) {
	if obc == nil {
		logD.Info("got nil obc, skipping")
		return nil
	}
	obcNsName := obc.Namespace + "/" + obc.Name
	obc, err = c.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Get(context.TODO(), obc.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to Get obc %q in order to remove finalizer: %v", obcNsName, err)
	}
	logD.Info("removing obc finalizer")
	removeFinalizer(obc)

	obc, err = c.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Update(context.TODO(), obc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("unable to Update obc %q to reflect removed finalizer: %v", obcNsName, err)
	}

	return nil
}

// The OB does not have an ownerReference and must be explicitly deleted after its
// finalizer is removed.
// Uses Update() because Patch Strategies are not supported for CRDs
// https://github.com/kubernetes/kubernetes/issues/50037
func deleteObjectBucket(ob *v1alpha1.ObjectBucket, c versioned.Interface) error {
	// skip if ob is nil or otherwise wasn't instantiated.
	// note: the ob is returned by Provision and Grant, partially filled
	if ob == nil || ob.ObjectMeta.UID == "" {
		return nil
	}

	logD.Info("removing ObjectBucket finalizer", "name", ob.Name)
	removeFinalizer(ob)
	_, err := c.ObjectbucketV1alpha1().ObjectBuckets().Update(context.TODO(), ob, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	logD.Info("deleting ObjectBucket", "name", ob.Name)
	err = c.ObjectbucketV1alpha1().ObjectBuckets().Delete(context.TODO(), ob.Name, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "ObjectBucket vanished before we could delete it, skipping", "name", ob.Name)
			return nil
		}
		return fmt.Errorf("error deleting ObjectBucket %q: %v", ob.Name, err)
	}
	logD.Info("ObjectBucket deleted", "name", ob.Name)
	return nil
}

func updateClaim(c versioned.Interface, obc *v1alpha1.ObjectBucketClaim) (result *v1alpha1.ObjectBucketClaim, err error) {
	logD.Info("updating", "obc", obc.Namespace+"/"+obc.Name)
	result, err = c.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).Update(context.TODO(), obc, metav1.UpdateOptions{})
	if err != nil {
		// return input obc here since result is nil on error returns
		return obc, fmt.Errorf("failed to update OBC %s/%s: %v", obc.Namespace, obc.Name, err)
	}
	return result, err
}

func updateObjectBucketClaimPhase(c versioned.Interface, obc *v1alpha1.ObjectBucketClaim, phase v1alpha1.ObjectBucketClaimStatusPhase) (result *v1alpha1.ObjectBucketClaim, err error) {
	logD.Info("updating status:", "obc", obc.Namespace+"/"+obc.Name, "old status",
		obc.Status.Phase, "new status", phase)
	// Do not make changes directly to the obc used as input. If the update fails, we should return
	// the obc given as input as it was given so code that comes after can't assume obc is at the
	// new phase.
	updateOBC := obc.DeepCopy()
	updateOBC.Status.Phase = phase

	result, err = c.ObjectbucketV1alpha1().ObjectBucketClaims(obc.Namespace).UpdateStatus(context.TODO(), updateOBC, metav1.UpdateOptions{})
	if err != nil {
		// return input obc here since result is nil on error returns
		return obc, fmt.Errorf("failed to update OBC %s/%s phase to %q: %v", obc.Namespace, obc.Name, phase, err)
	}
	return result, err
}

func updateObjectBucketPhase(c versioned.Interface, ob *v1alpha1.ObjectBucket, phase v1alpha1.ObjectBucketStatusPhase) (result *v1alpha1.ObjectBucket, err error) {
	logD.Info("updating status:", "ob", ob.Name, "old status", ob.Status.Phase, "new status", phase)
	// Do not make changes directly to the ob used as input. If the update fails, we should return
	// the ob given as input as it was given so code that comes after can't assume ob is at the new
	// phase.
	updateOB := ob.DeepCopy()
	updateOB.Status.Phase = phase

	result, err = c.ObjectbucketV1alpha1().ObjectBuckets().UpdateStatus(context.TODO(), updateOB, metav1.UpdateOptions{})
	if err != nil {
		// return input ob here since result is nil on error returns
		return ob, fmt.Errorf("failed to update OB %s phase to %q: %v", ob.Name, phase, err)
	}
	return result, err
}
