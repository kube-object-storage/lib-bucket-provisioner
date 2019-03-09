package util

import (
	"reflect"
	"testing"

	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/api/core/v1"

	"github.com/yard-turkey/lib-bucket-provisioner/pkg/api/provisioner"
	"github.com/yard-turkey/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

func TestNewCredentailsSecret(t *testing.T) {
	const (
		obcName      = "obc-testname"
		obcNamespace = "obc-testnamespace"
		authKey      = "test-auth-key"
		authSecret   = "test-auth-secret"
	)

	type args struct {
		opts *provisioner.BucketOptions
		ob   *v1alpha1.ObjectBucket
	}

	tests := []struct {
		name    string
		args    args
		want    *v1.Secret
		wantErr bool
	}{
		{
			name: "with nil ObjectBucket",
			args: args{
				opts: &provisioner.BucketOptions{
					ReclaimPolicy:     "",
					ObjectBucketName:  "",
					BucketName:        "",
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{},
					Parameters:        map[string]string{},
				},
				ob: nil,
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "with nil object bucket claim",
			args: args{
				opts: &provisioner.BucketOptions{
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{},
				},
				ob: nil,
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "with nil BucketOptions",
			args: args{
				opts: nil,
				ob: &v1alpha1.ObjectBucket{
					Spec: v1alpha1.ObjectBucketSpec{
						Authentication: &v1alpha1.AuthSource{
							AccessKeys: &v1alpha1.AccessKeys{
								AccessKeyId:     "access-key",
								SecretAccessKey: "secret-key",
							},
						},
					},
				},
			},
			want:    nil,
			wantErr: true,
		}, {
			name: "with defined access keys",
			args: args{
				opts: &provisioner.BucketOptions{
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{
						ObjectMeta: v12.ObjectMeta{
							Name:      obcName,
							Namespace: obcNamespace,
						},
					},
				},
				ob: &v1alpha1.ObjectBucket{
					Spec: v1alpha1.ObjectBucketSpec{
						Authentication: &v1alpha1.AuthSource{
							AccessKeys: &v1alpha1.AccessKeys{
								AccessKeyId:     authKey,
								SecretAccessKey: authSecret,
							},
						},
					},
				},
			},
			want: &v1.Secret{
				ObjectMeta: v12.ObjectMeta{
					Name:       obcName,
					Namespace:  obcNamespace,
					Finalizers: []string{Finalizer},
				},
				StringData: map[string]string{
					v1alpha1.AwsKeyField:    authKey,
					v1alpha1.AwsSecretField: authSecret,
				},
			},
			wantErr: false,
		}, {
			name: "with empty access keys",
			args: args{
				opts: &provisioner.BucketOptions{
					ObjectBucketClaim: &v1alpha1.ObjectBucketClaim{
						ObjectMeta: v12.ObjectMeta{
							Name:      obcName,
							Namespace: obcNamespace,
						},
					},
				},
				ob: &v1alpha1.ObjectBucket{
					Spec: v1alpha1.ObjectBucketSpec{
						Authentication: &v1alpha1.AuthSource{
							AccessKeys: &v1alpha1.AccessKeys{
								AccessKeyId:     "",
								SecretAccessKey: "",
							},
						},
					},
				},
			},
			want: &v1.Secret{
				ObjectMeta: v12.ObjectMeta{
					Name:       obcName,
					Namespace:  obcNamespace,
					Finalizers: []string{Finalizer},
				},
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
			got, err := NewCredentailsSecret(tt.args.opts, tt.args.ob)
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
