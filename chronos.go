/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"net"
	"time"
	"github.com/gogo/protobuf/proto"
	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	sched "github.com/mesos/mesos-go/scheduler"
)

const (
	MAX_RETRY     = 5
)

type Chronos struct {
	taskChan      chan TaskInfo
	chanMap       map[string] chan interface{}
	retryCh       chan TaskInfo
	taskBacklog   []TaskInfo

	executor      *mesos.ExecutorInfo
	driver        *sched.MesosSchedulerDriver
	tasksLaunched int
	tasksFinished int
	tasksErrored  int
}

func (chronos *Chronos) Registered(driver sched.SchedulerDriver, frameworkId *mesos.FrameworkID, masterInfo *mesos.MasterInfo) {
	log.Infoln("Framework Registered with Master ", masterInfo)
}

func (chronos *Chronos) Reregistered(driver sched.SchedulerDriver, masterInfo *mesos.MasterInfo) {
	log.Infoln("Framework Re-Registered with Master ", masterInfo)
	_, err := driver.ReconcileTasks([]*mesos.TaskStatus{})
	if err != nil {
		log.Errorf("failed to request task reconciliation: %v", err)
	}
}

func (chronos *Chronos) Disconnected(sched.SchedulerDriver) {
	log.Warningf("disconnected from master")
}

type Assign struct {
	SlaveId *mesos.SlaveID
	Cpus float64
	Mem float64
	RemainingCpus float64
	RemainingMem float64
	tasks []*mesos.TaskInfo
}

func generateMesosTask(taskInfo TaskInfo, sid *mesos.SlaveID) *mesos.TaskInfo {
	taskName := taskInfo.TaskName
	taskId := &mesos.TaskID{
		Value: proto.String(taskName),
	}
	
	dockerInfo := &mesos.ContainerInfo_DockerInfo{
		Image: proto.String(taskInfo.DockerImage),
	}
	containerInfo := &mesos.ContainerInfo{
		Type: mesos.ContainerInfo_DOCKER.Enum(),
		Docker: dockerInfo,
	}
	//Task should have at least one (but not both) of CommandInfo or ExecutorInfo present.
	task := &mesos.TaskInfo{
		Name:     proto.String(taskName),
		TaskId:   taskId,
		SlaveId:  sid,
		//Executor: sched.executor,
		Command: &mesos.CommandInfo{
			//According to mesos.pb.go, "If 'shell == true', the command will be launched via shell."
			Shell: proto.Bool(true),
			Value: proto.String(taskInfo.Command),
			User: proto.String("root"),
		},
		Container: containerInfo,
		Resources: []*mesos.Resource{
			util.NewScalarResource("cpus", taskInfo.Cpus),
			util.NewScalarResource("mem", taskInfo.Mem),
		},
	}
	return task
}

func checkTask(taskInfo TaskInfo, retryCh chan TaskInfo, donec chan interface{}) {
	it := 0
	for {
		select {
		case <-donec:
			return
		case <-time.After(30 * time.Second):
			//TODO: metrics
			if it++; it > MAX_RETRY {
				log.Warning(fmt.Sprintf("task %s is lost"))
				return
			}
			log.Warning(fmt.Sprintf("retry task %s, iteration %d", taskInfo.TaskName, it))
			retryCh<- taskInfo
		}
	}
}

func (chronos *Chronos) fifoPolicy(taskBacklog []TaskInfo,  assigns map[*mesos.OfferID] *Assign) []TaskInfo {
	numConsumed := 0
	for _, taskInfo := range taskBacklog {
		consumed := false
		for offerId, assign := range assigns {
			if assign.RemainingCpus >= taskInfo.Cpus && assign.RemainingMem >= taskInfo.Mem {
				donec := make(chan interface{})
				chronos.chanMap[taskInfo.TaskName] = donec
				go checkTask(taskInfo, chronos.retryCh, donec)
				task := generateMesosTask(taskInfo, assign.SlaveId)
				log.Infoln("generated task", *task.Name, "for offer", offerId.GetValue())
				assign.tasks = append(assign.tasks, task)
				assign.RemainingCpus -= taskInfo.Cpus
				assign.RemainingMem -= taskInfo.Mem
				consumed = true
				numConsumed+= 1
				break
			}
		}
		if !consumed {
			break
		}
	}
	return taskBacklog[numConsumed:]
}

func (chronos *Chronos) ResourceOffers(driver sched.SchedulerDriver, offers []*mesos.Offer) {
ForLoop:
	for {
		select {
		case taskInfo := <-chronos.taskChan:
			chronos.taskBacklog = append(chronos.taskBacklog, taskInfo)
		case taskInfo := <-chronos.retryCh:
			chronos.taskBacklog = append(chronos.taskBacklog, taskInfo)
		default:
			break ForLoop
		}
	}
	if len(chronos.taskBacklog) == 0 {
		log.Info("decline all ", len(offers), " offers since there is no task in backlog")
		ids := make([]*mesos.OfferID, len(offers))
		for i, offer := range offers {
			ids[i] = offer.Id
		}
		driver.LaunchTasks(ids, []*mesos.TaskInfo{}, &mesos.Filters{RefuseSeconds: proto.Float64(5)})
		return
	}
	assigns := make(map[*mesos.OfferID] *Assign, len(offers))
	for _, offer := range offers {
		var assign Assign
		assign.SlaveId = offer.SlaveId
		for _, resource := range offer.Resources {
			switch name := resource.GetName(); name {
			case "cpus":
				assign.Cpus = resource.GetScalar().GetValue()
				assign.RemainingCpus = assign.Cpus
			case "mem":
				assign.Mem = resource.GetScalar().GetValue()
				assign.RemainingMem = assign.Mem
			}
		}
		assigns[offer.Id] = &assign
		log.Info(fmt.Sprintf("offer %v on slave %s: cpus %v, mem %v", offer.Id.GetValue(), *offer.Hostname, assign.Cpus, assign.Mem))
	}
	chronos.taskBacklog = chronos.fifoPolicy(chronos.taskBacklog, assigns)
	spareIds := make([]*mesos.OfferID, 0)
	for offerId, assign := range assigns {
		if len(assign.tasks) == 0 {
			spareIds = append(spareIds, offerId);
			continue
		}
		log.Infoln("launching", len(assign.tasks), "tasks for offer", offerId.GetValue())
		driver.LaunchTasks([]*mesos.OfferID{offerId}, assign.tasks, &mesos.Filters{RefuseSeconds: proto.Float64(5)})
	}
	if len(spareIds)!=0 {
		log.Info("decline ", len(spareIds), " spare offers. currently ", len(chronos.taskBacklog), " tasks in backlog")
		driver.LaunchTasks(spareIds, []*mesos.TaskInfo{}, &mesos.Filters{RefuseSeconds: proto.Float64(5)})
	}
	return
}

func (chronos *Chronos) StatusUpdate(driver sched.SchedulerDriver, status *mesos.TaskStatus) {
	taskName := status.TaskId.GetValue()
	log.Infoln("Status update: task", taskName, " is in state ", status.State.Enum().String())
	if donec, found := chronos.chanMap[taskName]; found {
		//The first status could be other than TaskState_TASK_STAGING.
		close(donec)
		delete(chronos.chanMap, taskName)
	}

	if status.GetState() == mesos.TaskState_TASK_FINISHED {
		chronos.tasksFinished++
		driver.ReviveOffers() // TODO(jdef) rate-limit this
	}

	if status.GetState() == mesos.TaskState_TASK_LOST ||
		status.GetState() == mesos.TaskState_TASK_KILLED ||
		status.GetState() == mesos.TaskState_TASK_FAILED ||
		status.GetState() == mesos.TaskState_TASK_ERROR {
		chronos.tasksErrored++
	}
}

func (chronos *Chronos) OfferRescinded(_ sched.SchedulerDriver, oid *mesos.OfferID) {
	log.Errorf("offer rescinded: %v", oid)
}
func (chronos *Chronos) FrameworkMessage(_ sched.SchedulerDriver, eid *mesos.ExecutorID, sid *mesos.SlaveID, msg string) {
	log.Errorf("framework message from executor %q slave %q: %q", eid, sid, msg)
}
func (chronos *Chronos) SlaveLost(_ sched.SchedulerDriver, sid *mesos.SlaveID) {
	log.Errorf("slave lost: %v", sid)
}
func (chronos *Chronos) ExecutorLost(_ sched.SchedulerDriver, eid *mesos.ExecutorID, sid *mesos.SlaveID, code int) {
	log.Errorf("executor %q lost on slave %q code %d", eid, sid, code)
}
func (chronos *Chronos) Error(_ sched.SchedulerDriver, err string) {
	log.Errorf("Scheduler received error: %v", err)
}

func parseIP(address string) net.IP {
	addr, err := net.LookupIP(address)
	if err != nil {
		log.Fatal(err)
	}
	if len(addr) < 1 {
		log.Fatalf("failed to parse IP from address '%v'", address)
	}
	return addr[0]
}

// ----------------------- scheduler module interface ------------------------- //
func NewScheduler(mesosZk string, bindingAddress string, bindingPort uint16, taskChan chan TaskInfo) *Chronos {
	master := mesosZk
	chronos := &Chronos{
		taskBacklog: make([]TaskInfo, 0,  TASK_QUEUE_SIZE),
		taskChan: taskChan,
		chanMap: make(map[string] chan interface{}),
		retryCh: make(chan TaskInfo, TASK_QUEUE_SIZE),
	}
	// the framework
	fwinfo := &mesos.FrameworkInfo{
		User: proto.String(""), // Mesos-go will fill in user.
		Name: proto.String("chronos-go"),
	}

	addr := parseIP(bindingAddress)
	config := sched.DriverConfig{
		Scheduler:      chronos,
		Framework:      fwinfo,
		Master:         master,
		BindingAddress: addr,
		BindingPort:    bindingPort,
	}
	driver, err := sched.NewMesosSchedulerDriver(config)

	if err != nil {
		log.Errorln("Unable to create a SchedulerDriver ", err.Error())
	}
	chronos.driver = driver

	return chronos
}

func (chronos *Chronos) Start() {
	go chronos.driver.Run()
}

func (chronos *Chronos) Stop() {
	for taskName, donec := range chronos.chanMap {
		close(donec)
		delete(chronos.chanMap, taskName)
	}
	chronos.driver.Stop(true) //failover=true allows new leader register with the same framework id
}

