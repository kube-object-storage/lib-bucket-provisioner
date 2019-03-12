package util

import (
	"context"
	"reflect"
	"testing"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStorageClassForClaim(t *testing.T) {

	testObjectMeta := metav1.ObjectMeta{
		Name:      "testname",
		Namespace: "testnamespace",
		Finalizers: []string{
			Finalizer,
		},
	}

	type args struct {
		obc    *v1alpha1.ObjectBucketClaim
		client client.Client
	}

	tests := []struct {
		name    string
		args    args
		want    *storagev1.StorageClass
		wantErr bool
	}{
		{
			name: "nil OBC ptr",
			args: args{
				obc:    nil,
				client: fake.NewFakeClient(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "nil storage class name",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						StorageClassName: "",
					},
				},
				client: fake.NewFakeClient(),
			},
			want:    nil,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := StorageClassForClaim(tt.args.obc, tt.args.client, context.TODO())
			if (err != nil) != tt.wantErr {
				t.Errorf("StorageClassForClaim() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StorageClassForClaim() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewCredentailsSecret(t *testing.T) {
	const (
		obcName      = "obc-testname"
		obcNamespace = "obc-testnamespace"
		authKey      = "test-auth-key"
		authSecret   = "test-auth-secret"
	)

	testObjectMeta := metav1.ObjectMeta{
		Name:      obcName,
		Namespace: obcNamespace,
		Finalizers: []string{
			Finalizer,
		},
	}

	type args struct {
		obc            *v1alpha1.ObjectBucketClaim
		authentication *v1alpha1.Authentication
	}

	tests := []struct {
		name    string
		args    args
		want    *v1.Secret
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
		}, {
			name: "with nil Authentication ptr",
			args: args{
				obc:            &v1alpha1.ObjectBucketClaim{},
				authentication: nil,
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "with an authentication type defined (access keys)",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyId:     authKey,
						SecretAccessKey: authSecret,
					},
				},
			},
			want: &v1.Secret{
				ObjectMeta: testObjectMeta,
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    authKey,
					v1alpha1.AwsSecretField: authSecret,
				},
			},
			wantErr: false,
		}, {
			name: "with empty access keys",
			args: args{
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: testObjectMeta,
				},
				authentication: &v1alpha1.Authentication{
					AccessKeys: &v1alpha1.AccessKeys{
						AccessKeyId:     "",
						SecretAccessKey: "",
					},
				},
			},
			want: &v1.Secret{
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
			got, err := NewCredentailsSecret(tt.args.obc, tt.args.authentication)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCredentailsSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCredentailsSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateUntilDefaultTimeout(t *testing.T) {
	type args struct {
		obj runtime.Object
		c   client.Client
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CreateUntilDefaultTimeout(tt.args.obj, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("CreateUntilDefaultTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTranslateReclaimPolicy(t *testing.T) {
	type args struct {
		rp v1.PersistentVolumeReclaimPolicy
	}
	tests := []struct {
		name    string
		args    args
		want    v1alpha1.ReclaimPolicy
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TranslateReclaimPolicy(tt.args.rp)
			if (err != nil) != tt.wantErr {
				t.Errorf("TranslateReclaimPolicy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TranslateReclaimPolicy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateBucketName(t *testing.T) {
	type args struct {
		prefix string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateBucketName(tt.args.prefix); got != tt.want {
				t.Errorf("GenerateBucketName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewBucketConfigMap(t *testing.T) {
	type args struct {
		ep  *v1alpha1.Endpoint
		obc *v1alpha1.ObjectBucketClaim
	}
	tests := []struct {
		name string
		args args
		want *v1.ConfigMap
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewBucketConfigMap(tt.args.ep, tt.args.obc); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewBucketConfigMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
