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
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

func TestNewCredentialsSecret(t *testing.T) {
	const (
		obcName      = "obc-testname"
		obcNamespace = "obc-testnamespace"
		authKey      = "test-auth-key"
		authSecret   = "test-auth-secret"
	)

	var T = true

	testObjectMeta := metav1.ObjectMeta{
		Name:       obcName,
		Namespace:  obcNamespace,
		Finalizers: []string{finalizer},
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         "objectbucket.io/v1alpha1",
				Kind:               "ObjectBucketClaim",
				Name:               obcName,
				UID:                "",
				Controller:         &T,
				BlockOwnerDeletion: &T,
			},
		},
	}

	type args struct {
		obc            *v1alpha1.ObjectBucketClaim
		authentication *v1alpha1.Authentication
	}

	tests := []struct {
		name    string
		args    args
		want    *corev1.Secret
		wantErr bool
	}{
		{
			name: "with nil ObjectBucketClaim ptr",
			args: args{
				authentication: &v1alpha1.Authentication{},
				obc:            nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "with nil Authentication ptr",
			args: args{
				obc:            &v1alpha1.ObjectBucketClaim{},
				authentication: nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "with an authentication type defined (access keys)",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyID:     authKey,
						SecretAccessKey: authSecret,
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    authKey,
					v1alpha1.AwsSecretField: authSecret,
				},
			},
			wantErr: false,
		},
		{
			name: "with empty access keys",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyID:     "",
						SecretAccessKey: "",
					},
				},
			},
			want: &corev1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    "",
					v1alpha1.AwsSecretField: "",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newCredentialsSecret(tt.args.obc, tt.args.authentication)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCredentailsSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !cmp.Equal(tt.want, got) {
				t.Errorf(cmp.Diff(tt.want, got))
			}
		})
	}
}

func TestNewBucketConfigMap(t *testing.T) {

	const (
		obcName   = "test-obc"
		host      = "http://www.test.com"
		name      = "bucket-name"
		port      = 11111
		region    = "region"
		subRegion = "sub-region"
	)
	var T = true

	objMeta := metav1.ObjectMeta{
		Name:       "test-obc",
		Namespace:  "test-obc-namespace",
		Finalizers: []string{finalizer},
		Labels:     nil,
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion:         "objectbucket.io/v1alpha1",
				Kind:               "ObjectBucketClaim",
				Name:               obcName,
				UID:                "",
				Controller:         &T,
				BlockOwnerDeletion: &T,
			},
		},
	}

	type args struct {
		ep  *v1alpha1.Endpoint
		obc *v1alpha1.ObjectBucketClaim
	}
	tests := []struct {
		name    string
		args    args
		want    *corev1.ConfigMap
		wantErr bool
	}{
		{
			name: "endpoint with region and subregion",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  subRegion,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketRegion:    region,
					bucketSubRegion: subRegion,
				},
			},
			wantErr: false,
		},
		{
			name: "endpoint with only region",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  "",
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketRegion:    region,
					bucketSubRegion: "",
				},
			},
			wantErr: false,
		},
		{
			name: "with endpoint defined",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  subRegion,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketRegion:    region,
					bucketSubRegion: subRegion,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, err := newBucketConfigMap(tt.args.ep, tt.args.obc)
			if (err != nil) == !tt.wantErr {
				t.Errorf("newBucketConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			} else if !cmp.Equal(tt.want, got) {
				t.Errorf(cmp.Diff(tt.want, got))
			}
		})
	}
}
