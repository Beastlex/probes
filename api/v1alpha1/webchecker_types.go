/*
Copyright 2025.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WebCheckerSpec defines the desired state of WebChecker.
type WebCheckerSpec struct {
	Host string `json:"host"`
	// +kubebuilder:default:="/"
	Path string `json:"path,omitempty"`
}

// WebCheckerStatus defines the observed state of WebChecker.
type WebCheckerStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// WebChecker is the Schema for the webcheckers API.
type WebChecker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WebCheckerSpec   `json:"spec,omitempty"`
	Status WebCheckerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WebCheckerList contains a list of WebChecker.
type WebCheckerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WebChecker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WebChecker{}, &WebCheckerList{})
}
