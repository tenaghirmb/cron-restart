# cron-restart

The Kubernetes Restart Operator is designed to periodically restart common Kubernetes resources using the `kubectl rollout restart` command. This operator supports Deployments, StatefulSets, DaemonSets, and other resources that can be restarted using this command.

## Features

- **Scheduled Restarts**: Automatically restart resources on a predefined schedule.
- **Resource Support**: Works with any resource that supports `kubectl rollout restart`.
- **Custom Schedules**: Define custom restart schedules using Cron syntax.
- **Flexible Time Configuration**: Supports skipping specified dates and run once.

## Prerequisites

- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.
- Helm 3 (for installation via Helm).

## Installation

### Using Helm

1. **Package the Helm Chart**:
   ```bash
   helm package cron-restart
   ```

2. **Install the operator**:
   ```bash
   helm install cronrestart cron-restart-0.1.0.tgz -n kube-system
   ```

## Usage

Try out the examples in the examples folder.

1. Deploy resources in deployment_cronrestart.yaml
   ```bash
   kubectl apply -f deployment_cronrestart.yaml
   ```

2. Check the status of the deployment
   ```bash
   ➜  examples kubectl get deploy nginx-deployment-basic
   NAME                     READY   UP-TO-DATE   AVAILABLE   AGE
   nginx-deployment-basic   2/2     2            2           2m2s
   ```

3. Check the restart event
   ```bash
   ➜  examples kubectl describe deploy nginx-deployment-basic
   Name:                   nginx-deployment-basic
   Namespace:              default
   CreationTimestamp:      Mon, 03 Mar 2025 11:22:31 +0800
   Labels:                 app=nginx
   Annotations:            deployment.kubernetes.io/revision: 2
   Selector:               app=nginx
   Replicas:               2 desired | 2 updated | 2 total | 2 available | 0 unavailable
   StrategyType:           RollingUpdate
   MinReadySeconds:        0
   RollingUpdateStrategy:  25% max unavailable, 25% max surge
   Pod Template:
   Labels:       app=nginx
   Annotations:  kubectl.kubernetes.io/restartedAt: 2025-03-03T11:30:00+08:00
   Containers:
      nginx:
      Image:         nginx:1.7.9
      Port:          80/TCP
      Host Port:     0/TCP
      Environment:   <none>
      Mounts:        <none>
   Volumes:         <none>
   Node-Selectors:  <none>
   Tolerations:     <none>
   Conditions:
   Type           Status  Reason
   ----           ------  ------
   Available      True    MinimumReplicasAvailable
   Progressing    True    NewReplicaSetAvailable
   OldReplicaSets:  nginx-deployment-basic-84df99548d (0/0 replicas created)
   NewReplicaSet:   nginx-deployment-basic-58ddd489d (2/2 replicas created)
   Events:
   Type    Reason             Age    From                   Message
   ----    ------             ----   ----                   -------
   Normal  ScalingReplicaSet  8m15s  deployment-controller  Scaled up replica set nginx-deployment-basic-84df99548d to 2
   Normal  ScalingReplicaSet  46s    deployment-controller  Scaled up replica set nginx-deployment-basic-58ddd489d to 1
   Normal  ScalingReplicaSet  45s    deployment-controller  Scaled down replica set nginx-deployment-basic-84df99548d to 1
   Normal  ScalingReplicaSet  45s    deployment-controller  Scaled up replica set nginx-deployment-basic-58ddd489d to 2
   Normal  ScalingReplicaSet  44s    deployment-controller  Scaled down replica set nginx-deployment-basic-84df99548d to 0
   ```

4. Check controller's log
   ```bash
   ➜  examples kubectl logs -n kube-system kubernetes-cronrestarter-controller-86689855c9-mjplw
   2025-03-03T11:08:43+08:00	INFO	setup	starting manager
   2025-03-03T11:08:43+08:00	INFO	starting server	{"name": "health probe", "addr": "[::]:8081"}
   2025-03-03T11:08:43+08:00	INFO	Starting EventSource	{"controller": "cronrestarter", "controllerGroup": "autorestart.uni.com", "controllerKind": "CronRestarter", "source": "kind source: *v1.CronRestarter"}
   2025-03-03T11:08:43+08:00	INFO	Starting Controller	{"controller": "cronrestarter", "controllerGroup": "autorestart.uni.com", "controllerKind": "CronRestarter"}
   2025-03-03T11:08:43+08:00	INFO	Starting workers	{"controller": "cronrestarter", "controllerGroup": "autorestart.uni.com", "controllerKind": "CronRestarter", "worker count": 1}
   I0303 11:18:43.776888       1 cronmanager.go:98] GC loop started every 10m0s
   I0303 11:22:31.342943       1 cronrestarter_controller.go:73] Start to handle cronRestarter cronrestart-sample in default namespace
   I0303 11:22:31.345413       1 cronmanager.go:48] cronRestarter job restart of cronRestarter cronrestart-sample in default created, 1 active jobs exist
   I0303 11:22:31.354147       1 cronrestarter_controller.go:73] Start to handle cronRestarter cronrestart-sample in default namespace
   I0303 11:28:43.762628       1 cronmanager.go:98] GC loop started every 10m0s
   I0303 11:30:00.047956       1 cronrestarter_controller.go:73] Start to handle cronRestarter cronrestart-sample in default namespace
   2025-03-03T11:30:00+08:00	DEBUG	events	cron restarter job restart executed successfully. Deployment nginx-deployment-basic in namespace default has been restarted successfully. job: restart id: 451ff9ef-31e7-4e90-b605-03c5d7d5c511	{"type": "Normal", "object": {"kind":"CronRestarter","namespace":"default","name":"cronrestart-sample","uid":"ee4e061c-f9f7-4631-8178-d1e8b8859fd0","apiVersion":"autorestart.uni.com/v1","resourceVersion":"16572"}, "reason": "Succeed"}
   ```

5. Describe the cronrestarter
   ```bash
   ➜  examples kubectl describe cronrestarters cronrestart-sample
   Name:         cronrestart-sample
   Namespace:    default
   Labels:       <none>
   Annotations:  <none>
   API Version:  autorestart.uni.com/v1
   Kind:         CronRestarter
   Metadata:
   Creation Timestamp:  2025-03-03T03:22:31Z
   Generation:          1
   Resource Version:    16572
   UID:                 ee4e061c-f9f7-4631-8178-d1e8b8859fd0
   Spec:
   Jobs:
      Name:      restart
      Schedule:  0 */10 * * * *
   Restart Target Ref:
      API Version:  apps/v1
      Kind:         Deployment
      Name:         nginx-deployment-basic
   Status:
   Conditions:
      Job Id:           451ff9ef-31e7-4e90-b605-03c5d7d5c511
      Last Probe Time:  2025-03-03T03:30:00Z
      Message:          cron restarter job restart executed successfully. Deployment nginx-deployment-basic in namespace default has been restarted successfully. job: restart id: 451ff9ef-31e7-4e90-b605-03c5d7d5c511
      Name:             restart
      Run Once:         false
      Schedule:         0 */10 * * * *
      State:            Succeed
   Restart Target Ref:
      API Version:  apps/v1
      Kind:         Deployment
      Name:         nginx-deployment-basic
   Events:
   Type    Reason   Age    From           Message
   ----    ------   ----   ----           -------
   Normal  Succeed  5m58s  CronRestarter  cron restarter job restart executed successfully. Deployment nginx-deployment-basic in namespace default has been restarted successfully. job: restart id: 451ff9ef-31e7-4e90-b605-03c5d7d5c511
   ```

The `State` of the cronrestart job indicates its execution status. When the `State` is `Succeed`, it means the last execution was successful. When the `State` is `Submitted`, it means the cronrestart job has been submitted to the cron engine and is waiting to be executed. When the `State` is `Failed`, it means the last execution failed.

## Configuration

The following example demonstrates how to configure a `CronRestarter`.
```bash
apiVersion: autorestart.uni.com/v1
kind: CronRestarter
metadata:
  name: cronrestart-sample
spec:
  restartTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: nginx-deployment-basic
  excludeDates:
   # exclude November 10th
   - "* * * 10 11 *"
   # exclude every Friday 
   - "* * * * * 5"
  jobs:
    - name: "restart"
      runOnce: false
      schedule: "0 */10 * * * *"
    - name: "special-restart"
      runOnce: true
      schedule: "@date 2025-4-1 11:11:11"
``` 

### restartTargetRef

The `restartTargetRef` field specifies the workload to restart. If the workload supports the `kubectl rollout restart` command (such as `Deployment` and `StatefulSet`), `CronRestarter` should work well. Additionally, CronRestarter supports multiple cronrestart jobs in a single spec.

### excludeDates

The `excludeDates` field is an array of dates. The job will skip execution when the date matches. The minimum unit is a day. If you want to skip a specific date (e.g., November 10th), you can specify the excludeDates field as follows:
  ```bash
    excludeDates:
    - "* * * 10 11 *"
  ```

### jobs

The `Job` spec for cronrestart requires three fields:
* name
  `name` should be unique within a single cronrestart spec. You can distinguish different job execution statuses by their job names.
* runOnce
  If `runOnce` is set to `true`, the job will run only once and exit after the first execution.
* schedule
  The format of `schedule` is similar to that of `crontab`. The `kubernetes-cronrestarter-controller` uses an enhanced cron library in Go （<a target="_blank" href="https://github.com/ringtail/go-cron">go-cron</a>） which supports more expressive rules.
  
### cron expression

The cron expression format is described below:

Field name   | Mandatory? | Allowed values  | Allowed special characters
----------   | ---------- | --------------  | --------------------------
Seconds      | Yes        | 0-59            | * / , -
Minutes      | Yes        | 0-59            | * / , -
Hours        | Yes        | 0-23            | * / , -
Day of month | Yes        | 1-31            | * / , - ?
Month        | Yes        | 1-12 or JAN-DEC | * / , -
Day of week  | Yes        | 0-6 or SUN-SAT  | * / , - ?

#### Special Characters

- **Asterisk ( * )**
  - The asterisk indicates that the cron expression will match for all values of the field. For example, using an asterisk in the 5th field (month) means every month.

- **Slash ( / )**
  - Slashes are used to describe increments of ranges. For example, `3-59/15` in the 1st field (minutes) means the 3rd minute of the hour and every 15 minutes thereafter. The form `*/...` is equivalent to the form `first-last/...`, which means an increment over the largest possible range of the field. The form `N/...` means starting at N and using the increment until the end of that specific range. It does not wrap around.

- **Comma ( , )**
  - Commas are used to separate items of a list. For example, using `MON,WED,FRI` in the 5th field (day of week) means Mondays, Wednesdays, and Fridays.

- **Hyphen ( - )**
  - Hyphens are used to define ranges. For example, `9-17` means every hour between 9am and 5pm inclusive.

- **Question mark ( ? )**
  - A question mark can be used instead of `*` to leave either day-of-month or day-of-week blank.

#### Predefined Schedules

You may use one of several predefined schedules in place of a cron expression:

Entry                  | Description                                | Equivalent To
-----                  | -----------                                | -------------
@yearly (or @annually) | Run once a year, midnight, Jan. 1st        | `0 0 1 1 *`
@monthly               | Run once a month, midnight, first of month | `0 0 1 * *`
@weekly                | Run once a week, midnight between Sat/Sun  | `0 0 * * 0`
@daily (or @midnight)  | Run once a day, midnight                   | `0 0 * * *`
@hourly                | Run once an hour, beginning of hour        | `0 * * * *`

#### Intervals

You can also schedule a job to execute at fixed intervals, starting at the time it's added or when cron is run. This is supported by formatting the cron spec like this:

@every `<duration>`

where `<duration>` is a string accepted by `time.ParseDuration` (https://golang.org/pkg/time/#ParseDuration).

For example, `@every 1h30m10s` indicates a schedule that activates after 1 hour, 30 minutes, and 10 seconds, and then every interval after that.

**Note**: The interval does not take the job runtime into account. For example, if a job takes 3 minutes to run and is scheduled to run every 5 minutes, it will have only 2 minutes of idle time between each run.

For more scheduling options, please refer to the [cron package documentation](https://godoc.org/github.com/robfig/cron).

#### Specific Date (@date)

You can use a specific date to schedule a job for restarting workloads. This is useful for daily promotions, for example.

Entry                       | Description                                | Equivalent To
-----                       | -----------                                | -------------
@date 2025-4-1 21:54:00     | Run once when the date is reached          | `0 54 21 1 4 *`
                              
## Contributing

Contributions are welcome! Please submit an issue or pull request to contribute to this project.

## License

This project is licensed under the Apache License 2.0. See the LICENSE file for details.
