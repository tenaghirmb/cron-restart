package controller

import (
	"context"
	"fmt"
	"github.com/ringtail/go-cron"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
	cronrestartv1 "uni.com/cronrestart/api/v1"
)

const (
	MaxRetryTimes = 3
	GCInterval    = 10 * time.Minute
)

type NoNeedUpdate struct{}

func (n NoNeedUpdate) Error() string {
	return "NoNeedUpdate"
}

type CronManager struct {
	sync.Mutex
	cfg      *rest.Config
	client   client.Client
	jobQueue *sync.Map
	//cronProcessor CronProcessor
	cronExecutor  CronExecutor
	eventRecorder record.EventRecorder
}

func (cm *CronManager) createOrUpdate(j CronJob) error {
	if _, ok := cm.jobQueue.Load(j.ID()); !ok {
		err := cm.cronExecutor.AddJob(j)
		if err != nil {
			return fmt.Errorf("Failed to add job to cronExecutor,because of %v", err)
		}
		cm.jobQueue.Store(j.ID(), j)
		log.Infof("cronRestarter job %s of cronRestarter %s in %s created, %d active jobs exist", j.Name(), j.CronRestarterMeta().Name, j.CronRestarterMeta().Namespace,
			queueLength(cm.jobQueue))
	} else {
		loadJob, _ := cm.jobQueue.Load(j.ID())
		job, convert := loadJob.(*CronJobRestarter)
		if !convert {
			return fmt.Errorf("failed to convert job %v to cronRestarter", loadJob)
		}
		if ok := job.Equals(j); !ok {
			err := cm.cronExecutor.UpdateJob(j)
			if err != nil {
				return fmt.Errorf("failed to update job %s of cronRestarter %s in %s to cronExecutor, because of %v", job.Name(), job.CronRestarterMeta().Name, job.CronRestarterMeta().Namespace, err)
			}
			//update job queue
			cm.jobQueue.Store(j.ID(), j)
			log.Infof("cronRestarter job %s of cronRestarter %s in %s updated, %d active jobs exist", j.Name(), j.CronRestarterMeta().Name, j.CronRestarterMeta().Namespace, queueLength(cm.jobQueue))
		} else {
			return &NoNeedUpdate{}
		}
	}
	return nil
}

func (cm *CronManager) delete(id string) error {
	if loadJob, ok := cm.jobQueue.Load(id); ok {
		j, _ := loadJob.(*CronJobRestarter)
		err := cm.cronExecutor.RemoveJob(j)
		if err != nil {
			return fmt.Errorf("Failed to remove job from cronExecutor,because of %v", err)
		}
		cm.jobQueue.Delete(id)
		log.Infof("Remove cronRestarter job %s of cronRestarter %s in %s from jobQueue,%d active jobs left", j.Name(), j.CronRestarterMeta().Name, j.CronRestarterMeta().Namespace, queueLength(cm.jobQueue))
	}
	return nil
}

func (cm *CronManager) Run(stopChan chan struct{}) {
	cm.cronExecutor.Run()
	cm.gcLoop()
	<-stopChan
	cm.cronExecutor.Stop()
}

// GC loop
func (cm *CronManager) gcLoop() {
	ticker := time.NewTicker(GCInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Infof("GC loop started every %v", GCInterval)
				cm.GC()
			}
		}
	}()
}

// GC will collect all jobs which ref is not exists and recycle.
func (cm *CronManager) GC() {
	current := queueLength(cm.jobQueue)
	log.V(2).Infof("Current active jobs: %d,try to clean up the abandon ones.", current)

	gcJobFunc := func(key, j interface{}) bool {
		restarter := j.(*CronJobRestarter).RestarterRef
		job := j.(*CronJobRestarter)
		exitsts := true
		found, reason := cm.cronExecutor.FindJob(job)
		instance := &cronrestartv1.CronRestarter{}

		// check exists first
		if err := cm.client.Get(context.Background(), types.NamespacedName{
			Namespace: restarter.Namespace,
			Name:      restarter.Name,
		}, instance); err != nil {
			exitsts = false
			if errors.IsNotFound(err) {
				log.Infof("remove job %s(%s) of cronRestarter %s in namespace %s", job.Name(), job.SchedulePlan(), restarter.Name, restarter.Namespace)
				if found {
					err := cm.cronExecutor.RemoveJob(job)
					if err != nil {
						log.Errorf("Failed to gc job %s(%s) of cronRestarter %s in namespace %s", job.Name(), job.SchedulePlan(), restarter.Name, restarter.Namespace)
						return true
					}
				}
				cm.delete(job.ID())
			}
		}
		if !found {
			if exitsts {
				if reason == JobTimeOut {
					cm.eventRecorder.Event(instance, v1.EventTypeWarning, "OutOfDate", fmt.Sprintf("rerun out of date job: %s", job.Name()))
					log.Warningf("Failed to find job %s (job id: %s, plan %s) in cronRestarter %s in %s in cron engine and rerun the job.", job.Name(), job.ID(), job.SchedulePlan(), restarter.Name, restarter.Namespace)
					if msg, reRunErr := job.Run(); reRunErr != nil {
						log.Errorf("failed to rerun out of date job %s (job id: %s, plan %s) in cronRestarter %s in %s, msg:%s, err %v",
							job.Name(), job.ID(), job.SchedulePlan(), restarter.Name, restarter.Namespace, msg, reRunErr)
					}
				}

				log.Warningf("Failed to find job %s of cronRestarter %s in %s in cron engine and resubmit the job.", job.Name(), restarter.Name, restarter.Namespace)
				cm.cronExecutor.AddJob(job)
			}
		}
		return true
	}

	cm.jobQueue.Range(gcJobFunc)

	left := queueLength(cm.jobQueue)

	log.V(2).Infof("Current active jobs: %d, clean up %d jobs.", left, current-left)
}

func queueLength(que *sync.Map) int64 {
	len := int64(0)
	que.Range(func(k, v interface{}) bool {
		len++
		return true
	})
	return len
}

func (cm *CronManager) JobResultHandler(js *cron.JobResult) {
	job := js.Ref.(*CronJobRestarter)
	cronRestarter := js.Ref.(*CronJobRestarter).RestarterRef
	instance := &cronrestartv1.CronRestarter{}
	e := cm.client.Get(context.TODO(), types.NamespacedName{
		Namespace: cronRestarter.Namespace,
		Name:      cronRestarter.Name,
	}, instance)

	if e != nil {
		log.Errorf("Failed to fetch cronRestarter job %s of cronRestarter %s in namespace %s, because of %v", job.Name(), cronRestarter.Name, cronRestarter.Namespace, e)
		return
	}

	deepCopy := instance.DeepCopy()

	var (
		state     cronrestartv1.JobState
		message   string
		eventType string
	)

	err := js.Error
	if err != nil {
		state = cronrestartv1.Failed
		message = fmt.Sprintf("cron restarter failed to execute, because of %v", err)
		eventType = v1.EventTypeWarning
	} else {
		state = cronrestartv1.Succeed
		message = fmt.Sprintf("cron restarter job %s executed successfully. %s", job.name, js.Msg)
		eventType = v1.EventTypeNormal
	}

	condition := cronrestartv1.Condition{
		Name:          job.Name(),
		JobId:         job.ID(),
		RunOnce:       job.RunOnce,
		Schedule:      job.SchedulePlan(),
		LastProbeTime: metav1.Time{Time: time.Now()},
		State:         state,
		Message:       message,
	}

	conditions := instance.Status.Conditions

	var found = false
	for index, c := range conditions {
		if c.JobId == job.ID() || c.Name == job.Name() {
			found = true
			instance.Status.Conditions[index] = condition
		}
	}

	if !found {
		instance.Status.Conditions = append(instance.Status.Conditions, condition)
	}

	err = cm.updateCronRestarterStatusWithRetry(instance, deepCopy, job.name)
	if err != nil {
		if _, ok := err.(*NoNeedUpdate); ok {
			log.Warning("No need to update cronRestarter, because it is deleted before")
			return
		}
		cm.eventRecorder.Event(instance, v1.EventTypeWarning, "Failed", fmt.Sprintf("Failed to update cronRestarter status: %v", err))
	} else {
		cm.eventRecorder.Event(instance, eventType, string(state), message)
	}
}

func (cm *CronManager) updateCronRestarterStatusWithRetry(instance *cronrestartv1.CronRestarter, deepCopy *cronrestartv1.CronRestarter, jobName string) error {
	var err error
	if instance == nil {
		log.Warning("Failed to patch cronRestarter, because instance is deleted")
		return &NoNeedUpdate{}
	}
	for i := 1; i <= MaxRetryTimes; i++ {
		// leave ResourceVersion = empty
		err = cm.client.Status().Update(context.Background(), instance)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Error("Failed to update the status of cronRestarter, because instance is deleted")
				return &NoNeedUpdate{}
			}
			log.Errorf("Failed to update the status of cronRestarter %v, because of %v", instance, err)
			continue
		}
		break
	}
	if err != nil {
		log.Errorf("Failed to update the status of cronRestarter %s in %s after %d times, job %s, because of %v", instance.Name, instance.Namespace, MaxRetryTimes, jobName, err)
	}
	return err
}

func NewCronManager(cfg *rest.Config, client client.Client, recorder record.EventRecorder) *CronManager {
	cm := &CronManager{
		cfg:           cfg,
		client:        client,
		jobQueue:      &sync.Map{},
		eventRecorder: recorder,
	}
	cm.cronExecutor = NewCronRestartExecutor(nil, cm.JobResultHandler)
	return cm
}
