/*
Copyright 2024.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type RestartTargetRef struct {
	ApiVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type Job struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	RunOnce  bool   `json:"runOnce,omitempty"`
}

type CronRestarterSpec struct {
	ExcludeDates     []string         `json:"excludeDates,omitempty"`
	RestartTargetRef RestartTargetRef `json:"restartTargetRef"`
	Jobs             []Job            `json:"jobs"`
}

type JobState string

const (
	Succeed   JobState = "Succeed"
	Failed    JobState = "Failed"
	Submitted JobState = "Submitted"
)

type Condition struct {
	Name          string      `json:"name"`
	JobId         string      `json:"jobId"`
	Schedule      string      `json:"schedule"`
	RunOnce       bool        `json:"runOnce"`
	State         JobState    `json:"state"`
	LastProbeTime metav1.Time `json:"lastProbeTime"`
	Message       string      `json:"message"`
}

type CronRestarterStatus struct {
	RestartTargetRef RestartTargetRef `json:"restartTargetRef,omitempty"`
	ExcludeDates     []string         `json:"excludeDates,omitempty"`
	Conditions       []Condition      `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type CronRestarter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CronRestarterSpec   `json:"spec,omitempty"`
	Status CronRestarterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type CronRestarterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronRestarter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronRestarter{}, &CronRestarterList{})
}
