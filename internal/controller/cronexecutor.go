package controller

import (
	"github.com/ringtail/go-cron"
	log "k8s.io/klog/v2"
	"time"
)

const (
	maxOutOfDateTimeout = time.Minute * 5
)

type FailedFindJobReason string

const JobTimeOut = FailedFindJobReason("JobTimeOut")

type CronExecutor interface {
	Run()
	Stop()
	AddJob(job CronJob) error
	UpdateJob(job CronJob) error
	RemoveJob(job CronJob) error
	FindJob(job CronJob) (bool, FailedFindJobReason)
	ListEntries() []*cron.Entry
}

type CronRestartExecutor struct {
	Engine *cron.Cron
}

func (ce *CronRestartExecutor) Run() {
	ce.Engine.Start()
}

func (ce *CronRestartExecutor) Stop() {
	ce.Engine.Stop()
}

func (ce *CronRestartExecutor) AddJob(job CronJob) error {
	err := ce.Engine.AddJob(job.SchedulePlan(), job)
	if err != nil {
		log.Errorf("Failed to add job to engine, because of %v", err)
	}
	return err
}

func (ce *CronRestartExecutor) UpdateJob(job CronJob) error {
	ce.Engine.RemoveJob(job.ID())
	err := ce.Engine.AddJob(job.SchedulePlan(), job)
	if err != nil {
		log.Errorf("Failed to update job to engine, because of %v", err)
	}
	return err
}

func (ce *CronRestartExecutor) RemoveJob(job CronJob) error {
	ce.Engine.RemoveJob(job.ID())
	return nil
}

func (ce *CronRestartExecutor) FindJob(job CronJob) (bool, FailedFindJobReason) {
	entries := ce.Engine.Entries()
	for _, e := range entries {
		if e.Job.ID() == job.ID() {
			if e.Next.Add(maxOutOfDateTimeout).After(time.Now()) {
				return true, ""
			}
			log.Warningf("The job %s(job ID %s) in cronrestarter %s namespace %s is out of date.", job.Name(), job.ID(), job.CronRestarterMeta().Name, job.CronRestarterMeta().Namespace)
			return false, JobTimeOut
		}
	}
	return false, ""
}

func (ce *CronRestartExecutor) ListEntries() []*cron.Entry {
	return ce.Engine.Entries()
}

func NewCronRestartExecutor(timezone *time.Location, handler func(job *cron.JobResult)) CronExecutor {
	if nil == timezone {
		timezone = time.Now().Location()
	}
	c := &CronRestartExecutor{
		Engine: cron.NewWithLocation(timezone),
	}
	c.Engine.AddResultHandler(handler)
	return c
}
