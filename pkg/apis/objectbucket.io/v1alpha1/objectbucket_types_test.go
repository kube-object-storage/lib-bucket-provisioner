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

package v1alpha1

import (
	"reflect"
	"testing"
)

const (
	authKey    = "test-key"
	authSecret = "test-secret"
)

func TestAccessKeys_toMap(t *testing.T) {
	tests := []struct {
		name string
		ak   *AccessKeys
		want map[string]string
	}{
		{
			name: "with defined key values",
			ak: &AccessKeys{
				AccessKeyID:     authKey,
				SecretAccessKey: authSecret,
			},
			want: map[string]string{
				AwsKeyField:    authKey,
				AwsSecretField: authSecret,
			},
		}, {
			name: "without defined key values",
			ak:   &AccessKeys{},
			want: map[string]string{
				AwsKeyField:    "",
				AwsSecretField: "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ak.toMap(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AccessKeys.toMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAuthentication_ToMap(t *testing.T) {
	type fields struct {
		AccessKeys *AccessKeys
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Authentication{
				AccessKeys: tt.fields.AccessKeys,
			}
			if got := a.ToMap(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Authentication.ToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}
