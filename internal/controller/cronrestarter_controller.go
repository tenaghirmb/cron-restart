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

package controller

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	log "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"

	cronrestartv1 "uni.com/cronrestart/api/v1"
)

// CronRestarterReconciler reconciles a CronRestarter object
type CronRestarterReconciler struct {
	client.Client
	scheme      *runtime.Scheme
	CronManager *CronManager
}

func NewCronRestarterReconciler(mgr manager.Manager) *CronRestarterReconciler {
	var stopChan chan struct{}
	cm := NewCronManager(mgr.GetConfig(), mgr.GetClient(), mgr.GetEventRecorderFor("CronRestarter"))
	r := &CronRestarterReconciler{Client: mgr.GetClient(), scheme: mgr.GetScheme(), CronManager: cm}
	go func(cronManager *CronManager, stopChan chan struct{}) {
		cm.Run(stopChan)
		<-stopChan
	}(cm, stopChan)
	return r
}

var _ reconcile.Reconciler = &CronRestarterReconciler{}

// +kubebuilder:rbac:groups=autorestart.uni.com,resources=cronrestarters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autorestart.uni.com,resources=cronrestarters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autorestart.uni.com,resources=cronrestarters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CronRestarter object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *CronRestarterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	// Fetch the CronRestarter instance
	log.Infof("Start to handle cronRestarter %s in %s namespace", request.Name, request.Namespace)
	instance := &cronrestartv1.CronRestarter{}
	err := r.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			log.Infof("GC start for: cronRestarter %s in %s namespace is not found", request.Name, request.Namespace)
			go r.CronManager.GC()
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, nil
	}

	//log.Infof("%v is handled by cron-restarter controller", instance.Name)
	conditions := instance.Status.Conditions

	leftConditions := make([]cronrestartv1.Condition, 0)
	// check scaleTargetRef and excludeDates
	if checkGlobalParamsChanges(instance.Status, instance.Spec) {
		for _, cJob := range conditions {
			err := r.CronManager.delete(cJob.JobId)
			if err != nil {
				log.Errorf("Failed to delete job %s in cronRestarter %s namespace %s, because of %v", cJob.Name, instance.Name, instance.Namespace, err)
			}
		}
		// update scaleTargetRef and excludeDates
		instance.Status.RestartTargetRef = instance.Spec.RestartTargetRef
		instance.Status.ExcludeDates = instance.Spec.ExcludeDates
	} else {
		// check status and delete the expired job
		for _, cJob := range conditions {
			skip := false
			for _, job := range instance.Spec.Jobs {
				if cJob.Name == job.Name {
					// schedule has changed or RunOnce changed
					if cJob.Schedule != job.Schedule || cJob.RunOnce != job.RunOnce {
						// jobId exists and remove the job from cronManager
						if cJob.JobId != "" {
							err := r.CronManager.delete(cJob.JobId)
							if err != nil {
								log.Errorf("Failed to delete expired job %s in cronRestarter %s namespace %s,because of %v", cJob.Name, instance.Name, instance.Namespace, err)
							}
						}
						continue
					}
					// if nothing changed
					skip = true
				}
			}

			// need remove this condition because this is not job spec
			if !skip {
				if cJob.JobId != "" {
					err := r.CronManager.delete(cJob.JobId)
					if err != nil {
						log.Errorf("Failed to delete expired job %s in cronRestarter %s namespace %s, because of %v", cJob.Name, instance.Name, instance.Namespace, err)
					}
				}
			}

			// if job nothing changed then append to left conditions
			if skip {
				leftConditions = append(leftConditions, cJob)
			}
		}
	}

	// update the left to next step
	instance.Status.Conditions = leftConditions
	leftConditionsMap := convertConditionMaps(leftConditions)

	noNeedUpdateStatus := true

	for _, job := range instance.Spec.Jobs {
		jobCondition := cronrestartv1.Condition{
			Name:          job.Name,
			Schedule:      job.Schedule,
			RunOnce:       job.RunOnce,
			LastProbeTime: metav1.Time{Time: time.Now()},
		}
		j, err := CronRestarterJobFactory(instance, job, r.Client)

		if err != nil {
			jobCondition.State = cronrestartv1.Failed
			jobCondition.Message = fmt.Sprintf("Failed to create cron restarter job %s in %s namespace %s,because of %v",
				job.Name, instance.Name, instance.Namespace, err)
			log.Errorf("Failed to create cron restarter job %s,because of %v", job.Name, err)
		} else {
			name := job.Name
			if c, ok := leftConditionsMap[name]; ok {
				jobId := c.JobId
				j.SetID(jobId)

				// run once and return when reaches the final state
				if runOnce(job) && (c.State == cronrestartv1.Succeed || c.State == cronrestartv1.Failed) {
					err := r.CronManager.delete(jobId)
					if err != nil {
						log.Errorf("cron restarter runonce job %s(%s) in %s namespace %s has ran once but fail to exit,because of %v",
							name, jobId, instance.Name, instance.Namespace, err)
					}
					continue
				}
			}

			jobCondition.JobId = j.ID()
			err := r.CronManager.createOrUpdate(j)
			if err != nil {
				if _, ok := err.(*NoNeedUpdate); ok {
					continue
				} else {
					jobCondition.State = cronrestartv1.Failed
					jobCondition.Message = fmt.Sprintf("Failed to update cron restarter job %s,because of %v", job.Name, err)
				}
			} else {
				jobCondition.State = cronrestartv1.Submitted
			}
		}
		noNeedUpdateStatus = false
		instance.Status.Conditions = updateConditions(instance.Status.Conditions, jobCondition)
	}
	// conditions are not changed and no need to update.
	if !noNeedUpdateStatus || len(leftConditions) != len(conditions) {
		err := r.Status().Update(ctx, instance)
		if err != nil {
			log.Errorf("Failed to update cron restarter %s in namespace %s status, because of %v", instance.Name, instance.Namespace, err)
		}
	}

	return ctrl.Result{}, nil
}

func updateConditions(conditions []cronrestartv1.Condition, condition cronrestartv1.Condition) []cronrestartv1.Condition {
	r := make([]cronrestartv1.Condition, 0)
	m := convertConditionMaps(conditions)
	m[condition.Name] = condition
	for _, condition := range m {
		r = append(r, condition)
	}
	return r
}

func runOnce(job cronrestartv1.Job) bool {
	if strings.Contains(job.Schedule, "@date ") || job.RunOnce {
		return true
	}
	return false
}

func convertConditionMaps(conditions []cronrestartv1.Condition) map[string]cronrestartv1.Condition {
	m := make(map[string]cronrestartv1.Condition)
	for _, condition := range conditions {
		m[condition.Name] = condition
	}
	return m
}

// if global params changed then all jobs need to be recreated.
func checkGlobalParamsChanges(status cronrestartv1.CronRestarterStatus, spec cronrestartv1.CronRestarterSpec) bool {
	if &status.RestartTargetRef != nil && (status.RestartTargetRef.Kind != spec.RestartTargetRef.Kind || status.RestartTargetRef.ApiVersion != spec.RestartTargetRef.ApiVersion ||
		status.RestartTargetRef.Name != spec.RestartTargetRef.Name) {
		return true
	}

	curExcludeDates := sets.NewString(spec.ExcludeDates...)
	preExcludeDates := sets.NewString(status.ExcludeDates...)

	return !curExcludeDates.Equal(preExcludeDates)
}

// SetupWithManager sets up the controller with the Manager.
func (r *CronRestarterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cronrestartv1.CronRestarter{}).
		Complete(r)
}
