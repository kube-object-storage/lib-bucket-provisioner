package provisioner

import (
	"encoding/json"
	"k8s.io/client-go/kubernetes/fake"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	externalFake "github.com/yard-turkey/lib-bucket-provisioner/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewCredentialsSecret(t *testing.T) {
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
			finalizer,
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
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCredentailsSecret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewBucketConfigMap(t *testing.T) {

	const (
		host      = "http://www.test.com"
		name      = "bucket-name"
		port      = 11111
		ssl       = true
		region    = "region"
		subRegion = "sub-region"
	)

	objMeta := metav1.ObjectMeta{
		Name:       "test-obc",
		Namespace:  "test-obc-namespace",
		Finalizers: []string{finalizer},
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
					SSL:        ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(ssl),
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
					SSL:        ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(ssl),
					bucketRegion:    region,
					bucketSubRegion: "",
				},
			},
			wantErr: false,
		},
		{
			name: "with endpoint defined (non-SSL)",
			args: args{
				ep: &v1alpha1.Endpoint{
					BucketHost: host,
					BucketPort: port,
					BucketName: name,
					Region:     region,
					SubRegion:  subRegion,
					SSL:        !ssl,
				},
				obc: &v1alpha1.ObjectBucketClaim{
					ObjectMeta: objMeta,
					Spec: v1alpha1.ObjectBucketClaimSpec{
						BucketName: name,
						SSL:        !ssl,
					},
				},
			},
			want: &corev1.ConfigMap{
				ObjectMeta: objMeta,
				Data: map[string]string{
					bucketName:      name,
					bucketHost:      host,
					bucketPort:      strconv.Itoa(port),
					bucketSSL:       strconv.FormatBool(!ssl),
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
			} else if !reflect.DeepEqual(got, tt.want) {
				gotjson, _ := json.MarshalIndent(got, "", "\t")
				wantjson, _ := json.MarshalIndent(tt.want, "", "\t")
				t.Errorf("newBucketConfigMap() = %v, want %v", string(gotjson), string(wantjson))
			}
		})
	}
}
