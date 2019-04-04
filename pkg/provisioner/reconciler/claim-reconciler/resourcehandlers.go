package reconciler

import (
	"context"
	"fmt"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/provisioner/api"
	"path"
	"strconv"
	"time"

	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

const (
	DefaultRetryBaseInterval = time.Second * 3
	DefaultRetryTimeout      = time.Second * 30

	BucketName      = "BUCKET_NAME"
	BucketHost      = "BUCKET_HOST"
	BucketPort      = "BUCKET_PORT"
	BucketRegion    = "BUCKET_REGION"
	BucketSubRegion = "BUCKET_SUBREGION"
	BucketURL       = "BUCKET_URL"
	BucketSSL       = "BUCKET_SSL"

	Finalizer = api.Domain + "/finalizer"

	ObjectBucketNameFormat = "obc-%s-%s"
)

// newBucketConfigMap constructs a config map from a given endpoint and ObjectBucketClaim
// As a quality of life addition, it constructs a full URL for the bucket path.
// Success is constrained by a defined Bucket name and Bucket host.
func newBucketConfigMap(ep *v1alpha1.Endpoint, obc *v1alpha1.ObjectBucketClaim) (*corev1.ConfigMap, error) {
	logD.Info("defining new configMap", "for claim", obc.Namespace+"/"+obc.Name)
	if ep == nil {
		return nil, fmt.Errorf("cannot construct configMap, got nil Endpoint")
	}
	if obc == nil {
		return nil, fmt.Errorf("cannot construct configMap, got nil OBC")
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       obc.Name,
			Namespace:  obc.Namespace,
			Finalizers: []string{Finalizer},
		},
		Data: map[string]string{
			BucketName:      obc.Spec.BucketName,
			BucketHost:      ep.BucketHost,
			BucketPort:      strconv.Itoa(ep.BucketPort),
			BucketSSL:       strconv.FormatBool(ep.SSL),
			BucketRegion:    ep.Region,
			BucketSubRegion: ep.SubRegion,
			BucketURL:       fmt.Sprintf("%s:%d/%s", ep.BucketHost, ep.BucketPort, path.Join(ep.Region, ep.SubRegion, ep.BucketName)),
		},
	}, nil
}

// NewCredentailsSecret returns a secret with data appropriate to the supported authenticaion method.
// Even if the values for the Authentication keys are empty, we generate the secret.
func newCredentialsSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication) (*corev1.Secret, error) {

	if obc == nil {
		return nil, fmt.Errorf("ObjectBucketClaim required to generate secret")
	}
	if auth == nil {
		return nil, fmt.Errorf("got nil authentication, nothing to do")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       obc.Name,
			Namespace:  obc.Namespace,
			Finalizers: []string{Finalizer},
		},
	}

	secret.StringData = auth.ToMap()
	return secret, nil
}

func createObjectBucket(ob *v1alpha1.ObjectBucket, c client.Client, retryInterval, retryTimeout time.Duration) (*v1alpha1.ObjectBucket, error) {
	logD.Info("creating ObjectBucket", "name", ob.Name)
	if err := createUntilDefaultTimeout(ob, c, retryInterval, retryTimeout); err != nil {
		return nil, err
	}
	return ob, nil
}

func createSecret(obc *v1alpha1.ObjectBucketClaim, auth *v1alpha1.Authentication, c client.Client, retryInterval, retryTimeout time.Duration) (*v1.Secret, error) {
	secret, err := newCredentialsSecret(obc, auth)
	if err != nil {
		return nil, err
	}

	logD.Info("creating Secret", "namespace", secret.Namespace, "name", secret.Name)
	if err = createUntilDefaultTimeout(secret, c, retryInterval, retryTimeout); err != nil {
		return nil, fmt.Errorf("unable to create Secret %q: %v", secret.Name, err)
	}
	return secret, nil
}

func createConfigMap(obc *v1alpha1.ObjectBucketClaim, ep *v1alpha1.Endpoint, c client.Client, retryInterval, retryTimeout time.Duration) (*v1.ConfigMap, error) {
	configMap, err := newBucketConfigMap(ep, obc)
	if err != nil {
		return nil, nil
	}

	logD.Info("creating configMap", "namespace", configMap.Namespace, "name", configMap.Name)
	err = createUntilDefaultTimeout(configMap, c, retryInterval, retryTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to create configMap %q for claim %v: %v", configMap.Name, configMap.Name, err)
	}
	return configMap, nil
}

func deleteConfigMap(cm *v1.ConfigMap, ic *internalClient) error {
	if cm == nil {
		log.Info("got nil configMap pointer, skipping delete")
		return nil
	}
	if hasFinalizer(cm) {
		logD.Info("removing finalizer from configMap", "name", cm.Name)

		err := removeFinalizer(cm, ic)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Error(err, "configMap vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on configMap %s/%s: %v", cm.Namespace, cm.Name, err)
			}
		}
	}

	logD.Info("deleting configMap", "name", cm.Namespace+"/"+cm.Name)
	err := ic.Client.Delete(context.Background(), cm)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "configMap vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting configMap %s/%s: %v", cm.Namespace, cm.Name, err)
		}
	}
	return nil
}

func deleteSecret(sec *v1.Secret, ic *internalClient) error {
	if sec == nil {
		log.Info("got nil secret, skipping")
		return nil
	}
	if hasFinalizer(sec) {
		logD.Info("removing finalizer from Secret", "name", sec.Namespace+"/"+sec.Name)

		err := removeFinalizer(sec, ic)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Error(err, "secret vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on Secret %s/%s: %v", sec.Namespace, sec.Name, err)
			}
		}
	}

	logD.Info("deleting secret", "name", sec.Namespace+"/"+sec.Name)
	err := ic.Client.Delete(context.Background(), sec)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "secret vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting Secret %s/%s: %v", sec.Namespace, sec.Name, err)
		}
	}
	return nil
}

func deleteObjectBucket(ob *v1alpha1.ObjectBucket, ic *internalClient) error {
	if ob == nil {
		log.Error(fmt.Errorf("got nil objectBucket, skipping"), "")
		return nil
	}
	logD.Info("deleting ObjectBucket", "name", ob.Name)
	if hasFinalizer(ob) {
		err := removeFinalizer(ob, ic)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("ObjectBucket %v vanished before we could remove the finalizer, assuming deleted")
				return nil
			} else {
				return fmt.Errorf("error removing finalizer on ObjectBucket %s: %v", ob.Name, err)
			}
		}
	}

	err := ic.Client.Delete(context.Background(), ob)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "ObjectBucket vanished before we could delete it, skipping")
			return nil
		} else {
			return fmt.Errorf("error deleting ObjectBucket %s/%s: %v", ob.Namespace, ob.Name, err)
		}
	}
	return nil
}
