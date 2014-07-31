package executor

import (
	"code.google.com/p/goprotobuf/proto"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
	log "github.com/golang/glog"
	"github.com/mesosphere/mesos-go/mesos"
	"gopkg.in/v1/yaml"
)

const defaultChanSize = 1024

type kuberTask struct {
	mesosTaskInfo *mesos.TaskInfo
	pod           *kubelet.Pod
}

// KuberneteExecutor is an mesos executor that runs pods
// in a minion machine.
type KuberneteExecutor struct {
	kl         *kubelet.Kubelet // the kubelet instance.
	updateChan chan kubelet.PodUpdate
	driver     mesos.ExecutorDriver
	registered bool
	tasks      map[string]*kuberTask
}

// New creates a new kubernete executor.
func New(driver mesos.ExecutorDriver, kl *kubelet.Kubelet) *KuberneteExecutor {
	return &KuberneteExecutor{
		kl:         kl,
		updateChan: make(chan kubelet.PodUpdate, defaultChanSize),
		driver:     driver,
		registered: false,
		tasks:      make(map[string]*kuberTask),
	}
}

// Runkubelet runs the kubelet.
func (k *KuberneteExecutor) RunKubelet() {
	k.kl.Run(k.updateChan)
}

func (k *KuberneteExecutor) gatherContainerManifests() []api.ContainerManifest {
	var manifests []api.ContainerManifest
	for _, task := range k.tasks {
		manifests = append(manifests, task.pod.Manifest)
	}
	return manifests
}

// Registered is called when the executor is successfully registered with the slave.
func (k *KuberneteExecutor) Registered(driver mesos.ExecutorDriver,
	executorInfo *mesos.ExecutorInfo, frameworkInfo *mesos.FrameworkInfo, slaveInfo *mesos.SlaveInfo) {
	log.Infof("Executor %v of framework %v registered with slave %v\n",
		executorInfo, frameworkInfo, slaveInfo)
	k.registered = true
}

// Reregistered is called when the executor is successfully re-registered with the slave.
// This can happen when the slave fails over.
func (k *KuberneteExecutor) Reregistered(driver mesos.ExecutorDriver, slaveInfo *mesos.SlaveInfo) {
	log.Infof("Reregistered with slave %v\n", slaveInfo)
	k.registered = true
}

// Disconnected is called when the executor is disconnected with the slave.
func (k *KuberneteExecutor) Disconnected(driver mesos.ExecutorDriver) {
	log.Infof("Slave is disconnected\n")
	k.registered = false
}

// LaunchTask is called when the executor receives a request to launch a task.
func (k *KuberneteExecutor) LaunchTask(driver mesos.ExecutorDriver, taskInfo *mesos.TaskInfo) {
	log.Infof("Launch task %v\n", taskInfo)

	if !k.registered {
		log.Warningf("Ignore launch task because the executor is disconnected\n")
		k.sendStatusUpdate(taskInfo.GetTaskId(),
			mesos.TaskState_TASK_FAILED, "Executor not registered yet")
		return
	}

	taskId := taskInfo.GetTaskId().GetValue()
	if _, found := k.tasks[taskId]; found {
		log.Warningf("Task already launched\n")
		// Not to send back TASK_RUNNING here, because
		// may be duplicated messages or duplicated task id.
		return
	}

	// Get the container manifest from the taskInfo.
	var manifest api.ContainerManifest
	if err := yaml.Unmarshal(taskInfo.GetData(), &manifest); err != nil {
		log.Warningf("Failed to extract yaml data from the taskInfo.data %v\n", err)
		k.sendStatusUpdate(taskInfo.GetTaskId(),
			mesos.TaskState_TASK_FAILED, "Failed to extract yaml data")
		return
	}

	// Create the task.
	task := &kuberTask{
		mesosTaskInfo: taskInfo,
		pod: &kubelet.Pod{
			Name:      manifest.ID,
			Namespace: "etcd", // TODO(CD): smite all hardcoded values
			Manifest:  manifest,
		},
	}

	// Add the task.
	k.tasks[taskId] = task

	// Send the pod updates to the channel.
	// TODO(yifan): Replace SET with REMOVE when it's implemented.
	update := kubelet.PodUpdate{
		Pods: []kubelet.Pod{
			*task.pod,
		},
		Op: kubelet.SET,
	}
	k.updateChan <- update
	// TODO(yifan): Check the result of the launch event.

	k.sendStatusUpdate(taskInfo.GetTaskId(),
		mesos.TaskState_TASK_RUNNING, "Task running")
}

// KillTask is called when the executor receives a request to kill a task.
func (k *KuberneteExecutor) KillTask(driver mesos.ExecutorDriver, taskId *mesos.TaskID) {
	log.Infof("Kill task %v\n", taskId)

	if !k.registered {
		log.Warningf("Ignore kill task because the executor is disconnected\n")
		return
	}

	tid := taskId.GetValue()

	kuberTask, ok := k.tasks[tid]

	if !ok {
		log.Infof("Failed to kill task, unknown task %v\n", tid)
		return
	}

	delete(k.tasks, tid)

	// Send the pod updates to the channel.
	// TODO(yifan): Replace SET with REMOVE when it's implemented.
	update := kubelet.PodUpdate{
		Pods: []kubelet.Pod{
			kubelet.Pod{
				Namespace: kuberTask.pod.Namespace,
				Name: kuberTask.pod.Name,
			},
		},
		Op:   kubelet.REMOVE,
	}
	k.updateChan <- update
	// TODO(yifan): Check the result of the kill event.

	k.sendStatusUpdate(taskId, mesos.TaskState_TASK_KILLED, "Task killed")
}

// FrameworkMessage is called when the framework sends some message to the executor
func (k *KuberneteExecutor) FrameworkMessage(driver mesos.ExecutorDriver, message string) {
	log.Infof("Receives message from framework %v\n", message)
	// TODO(yifan): Check for update message.
}

// Shutdown is called when the executor receives a shutdown request.
func (k *KuberneteExecutor) Shutdown(driver mesos.ExecutorDriver) {
	log.Infof("Shutdown the executor\n")

	for tid, task := range k.tasks {
		delete(k.tasks, tid)

		// Send the pod updates to the channel.
		// TODO(yifan): Replace SET with REMOVE when it's implemented.
		update := kubelet.PodUpdate{
			Pods: []kubelet.Pod{},
			Op:   kubelet.SET,
		}
		k.updateChan <- update
		// TODO(yifan): Check the result of the kill event.

		k.sendStatusUpdate(task.mesosTaskInfo.GetTaskId(),
			mesos.TaskState_TASK_KILLED, "Executor shutdown")
	}
}

// Error is called when some error happens.
func (k *KuberneteExecutor) Error(driver mesos.ExecutorDriver, message string) {
	log.Errorf("Executor error: %v\n", message)
}

func (k *KuberneteExecutor) sendStatusUpdate(taskId *mesos.TaskID, state mesos.TaskState, message string) {
	statusUpdate := &mesos.TaskStatus{
		TaskId:  taskId,
		State:   &state,
		Message: proto.String(message),
	}
	// TODO(yifan): Maybe try to resend again in the future.
	if err := k.driver.SendStatusUpdate(statusUpdate); err != nil {
		log.Warningf("Failed to send status update%v, %v", err)
	}
}
