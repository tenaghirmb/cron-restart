package controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/ringtail/go-cron"
	uuid "github.com/satori/go.uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	log "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
	"time"
	v1 "uni.com/cronrestart/api/v1"
)

const (
	updateRetryInterval = 3 * time.Second
	maxRetryTimeout     = 10 * time.Second
	dateFormat          = "11-15-1990"
)

type TargetRef struct {
	RefName      string
	RefNamespace string
	RefKind      string
	RefGroup     string
	RefVersion   string
}

// needed when compare equals.
func (tr *TargetRef) toString() string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", tr.RefName, tr.RefNamespace, tr.RefKind, tr.RefGroup, tr.RefVersion)
}

type CronJob interface {
	ID() string
	Name() string
	SetID(id string)
	Equals(job CronJob) bool
	SchedulePlan() string
	Ref() *TargetRef
	CronRestarterMeta() *v1.CronRestarter
	Run() (msg string, err error)
}

type CronJobRestarter struct {
	TargetRef    *TargetRef
	RestarterRef *v1.CronRestarter
	id           string
	name         string
	Plan         string
	RunOnce      bool
	excludeDates []string
	client       client.Client
}

func (cr *CronJobRestarter) SetID(id string) {
	cr.id = id
}

func (cr *CronJobRestarter) Name() string {
	return cr.name
}

func (cr *CronJobRestarter) ID() string {
	return cr.id
}

func (cr *CronJobRestarter) Equals(j CronJob) bool {
	// update will create a new uuid
	if cr.id == j.ID() && cr.SchedulePlan() == j.SchedulePlan() && cr.Ref().toString() == j.Ref().toString() {
		return true
	}
	return false
}

func (cr *CronJobRestarter) SchedulePlan() string {
	return cr.Plan
}

func (cr *CronJobRestarter) Ref() *TargetRef {
	return cr.TargetRef
}

func (cr *CronJobRestarter) CronRestarterMeta() *v1.CronRestarter {
	return cr.RestarterRef
}

func (cr *CronJobRestarter) Run() (msg string, err error) {

	if skip, msg := IsTodayOff(cr.excludeDates); skip {
		return msg, nil
	}

	startTime := time.Now()
	times := 0
	for {
		now := time.Now()

		// timeout and exit
		if startTime.Add(maxRetryTimeout).Before(now) {
			return "", fmt.Errorf("failed to restart %s %s in %s namespace after retrying %d times and exit,because of %v", cr.TargetRef.RefKind, cr.TargetRef.RefName, cr.TargetRef.RefNamespace, times, err)
		}

		msg, err = cr.RestartRef()
		if err == nil {
			break
		}
		time.Sleep(updateRetryInterval)
		times = times + 1
	}

	return msg, err
}

func (cr *CronJobRestarter) RestartRef() (msg string, err error) {
	targetGVK := schema.GroupVersionKind{
		Group:   cr.TargetRef.RefGroup,
		Version: cr.TargetRef.RefVersion,
		Kind:    cr.TargetRef.RefKind,
	}

	// 获取资源
	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(targetGVK)
	if err := cr.client.Get(context.Background(), types.NamespacedName{Namespace: cr.TargetRef.RefNamespace, Name: cr.TargetRef.RefName}, target); err != nil {
		log.Errorf("failed to find source target %s %s in %s namespace", cr.TargetRef.RefKind, cr.TargetRef.RefName, cr.TargetRef.RefNamespace)
		return "", fmt.Errorf("failed to find source target %s %s in %s namespace", cr.TargetRef.RefKind, cr.TargetRef.RefName, cr.TargetRef.RefNamespace)
	}

	// 构建Patch数据
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, time.Now().Format(time.RFC3339))
	mergePatch := []byte(patch)

	// 应用Patch
	if err := cr.client.Patch(context.Background(), target, client.RawPatch(types.MergePatchType, mergePatch)); err != nil {
		return "", fmt.Errorf("failed to restart %s %s in %s namespace, because of %v", cr.TargetRef.RefKind, cr.TargetRef.RefName, cr.TargetRef.RefNamespace, err)
	}
	msg = fmt.Sprintf("%s %s in namespace %s has been restarted successfully. job: %s id: %s", cr.TargetRef.RefKind, cr.TargetRef.RefName, cr.TargetRef.RefNamespace, cr.Name(), cr.ID())
	return msg, nil
}

func checkRefValid(ref *TargetRef) error {
	if ref.RefVersion == "" || ref.RefGroup == "" || ref.RefName == "" || ref.RefNamespace == "" || ref.RefKind == "" {
		return errors.New("any properties in ref could not be empty")
	}
	return nil
}

func checkPlanValid(plan string) error {
	return nil
}

func CronRestarterJobFactory(instance *v1.CronRestarter, job v1.Job, client client.Client) (CronJob, error) {
	arr := strings.Split(instance.Spec.RestartTargetRef.ApiVersion, "/")
	group := arr[0]
	version := arr[1]
	ref := &TargetRef{
		RefName:      instance.Spec.RestartTargetRef.Name,
		RefKind:      instance.Spec.RestartTargetRef.Kind,
		RefNamespace: instance.Namespace,
		RefGroup:     group,
		RefVersion:   version,
	}

	if err := checkRefValid(ref); err != nil {
		return nil, err
	}
	if err := checkPlanValid(job.Schedule); err != nil {
		return nil, err
	}
	return &CronJobRestarter{
		id:           uuid.Must(uuid.NewV4(), nil).String(),
		TargetRef:    ref,
		RestarterRef: instance,
		name:         job.Name,
		Plan:         job.Schedule,
		RunOnce:      job.RunOnce,
		excludeDates: instance.Spec.ExcludeDates,
		client:       client,
	}, nil
}

func IsTodayOff(excludeDates []string) (bool, string) {

	if excludeDates == nil {
		return false, ""
	}

	now := time.Now()
	for _, date := range excludeDates {
		schedule, err := cron.Parse(date)
		if err != nil {
			log.Warningf("Failed to parse schedule %s,and skip this date,because of %v", date, err)
			continue
		}
		if nextTime := schedule.Next(now); nextTime.Format(dateFormat) == now.Format(dateFormat) {
			return true, fmt.Sprintf("skip scaling activity,because of excludeDate (%s).", date)
		}
	}
	return false, ""
}
