package main

import (
	"time"
	"fmt"
	"sync/atomic"
	"sync"
	"errors"
	log "github.com/golang/glog"
	"github.com/yuzhichang/cron"
)

type JobInfo struct {
	Name string `json:"name"`
	Enabled bool `json:"enabled"`
	Schedule string `json:"schedule"`
	DockerImage string `json:"dockerimage"`
	Command string `json:"command"`
	Cpus float64 `json:"cpus"`
	Mem float64 `json:"mem"`
}

func (jobInfo JobInfo) Run() {
	taskInfo := TaskInfo {
		JobInfo: jobInfo,
		Timestamp: time.Now().UnixNano(),
	}
	taskInfo.TaskName = fmt.Sprintf("%v-%v", jobInfo.Name, taskInfo.Timestamp)
	log.Infoln(fmt.Sprintf("triggered cron job %s", taskInfo.TaskName))
	gJobMgmt.taskChan<- taskInfo
}

type TaskInfo struct {
	JobInfo
	Timestamp int64
	TaskName string
}

type JobMgmt struct {
	rwMutex sync.RWMutex
	taskChan chan TaskInfo
	cron *cron.Cron
	jobMap map[int64] JobInfo
	jobIdMap map[int64] int64  //jobId -> cronJobId, both are monotonously increasing.
	master string
	nextJobId int64
}

func (jobMgmt *JobMgmt) AddJob(jobInfo JobInfo) int64 {
	jobId := atomic.AddInt64(&jobMgmt.nextJobId, 1)
	jobMgmt.rwMutex.Lock()
	defer jobMgmt.rwMutex.Unlock()
	var cronJobId2 int64 = 0
	if jobInfo.Enabled {
		cronJobId, err := jobMgmt.cron.AddJob(jobInfo.Schedule, jobInfo)
		if err != nil {
			panic(err)
		}
		jobMgmt.jobIdMap[jobId] = cronJobId
		cronJobId2 = cronJobId
	}
	jobMgmt.jobMap[jobId] = jobInfo
	if err := PersistPutJob(jobId, jobInfo); err != nil{
		//TODO: metrics
		log.Error(err)
	}
	log.Infoln("added job ", jobId, cronJobId2, jobInfo.Name)
	return jobId
}

func (jobMgmt *JobMgmt) DelJob(jobId int64) {
	jobMgmt.rwMutex.Lock()
	defer jobMgmt.rwMutex.Unlock()
	cronJobId, ok := jobMgmt.jobIdMap[jobId]
	if ok {
		jobMgmt.cron.DelJob(cronJobId)
		delete(jobMgmt.jobIdMap, jobId)
	}
	delete(jobMgmt.jobMap, jobId)
	if err := PersistDelJob(jobId); err != nil {
		//TODO: metrics
		log.Error(err)
	}
	log.Infoln("deleted job ", jobId, cronJobId)
}

func (jobMgmt *JobMgmt) ClearJobs() {
	jobMgmt.rwMutex.Lock()
	defer jobMgmt.rwMutex.Unlock()
	//http://stackoverflow.com/questions/13812121/how-to-clear-a-map-in-go
	for jobId, cronJobId := range jobMgmt.jobIdMap {
		jobMgmt.cron.DelJob(cronJobId)
		delete(jobMgmt.jobIdMap, jobId)
	}
	for jobId := range jobMgmt.jobMap {
		delete(jobMgmt.jobMap, jobId)
	}
	if err := PersistDelJobs(); err != nil {
		//TODO: metrics
		log.Error(err)
	}
	log.Infoln("cleared jobs")
}

func (jobMgmt *JobMgmt) UpdateJob(jobId int64, jobInfo JobInfo) {
	jobMgmt.rwMutex.Lock()
	defer jobMgmt.rwMutex.Unlock()
	cronJobId1, ok := jobMgmt.jobIdMap[jobId]
	if ok {
		jobMgmt.cron.DelJob(cronJobId1)
	}
	var cronJobId2 int64 = 0
	if jobInfo.Enabled {
		cronJobId, err := jobMgmt.cron.AddJob(jobInfo.Schedule, jobInfo)
		if err != nil {
			panic(err)
		}
		jobMgmt.jobIdMap[jobId] = cronJobId
		cronJobId2 = cronJobId
	}
	
	jobMgmt.jobMap[jobId] = jobInfo
	if err := PersistPutJob(jobId, jobInfo); err != nil {
		log.Error(err)
	}
	log.Infoln(fmt.Sprintf("updated job %v %v->%v %v", jobId, cronJobId1, cronJobId2, jobInfo.Name))
}

func (jobMgmt *JobMgmt) GetJob(jobId int64) (JobInfo, error){
	jobMgmt.rwMutex.RLock()
	defer jobMgmt.rwMutex.RUnlock()
	jobInfo, ok := jobMgmt.jobMap[jobId]
	switch ok {
	case true:
		return jobInfo, nil
	default:
		return jobInfo, errors.New("not found")
	}
}

func (jobMgmt *JobMgmt) GetJobs() map[int64] JobInfo{
	jobMgmt.rwMutex.RLock()
	defer jobMgmt.rwMutex.RUnlock()
	jobMap := make(map[int64] JobInfo, len(jobMgmt.jobMap))
	for jobId, jobInfo := range jobMgmt.jobMap {
		//NOTE: Storing pointer looks moare reasonable, however &jobInfo is consistant during loop!
		jobMap[jobId] = jobInfo
	}
	return jobMap
}

//http://stackoverflow.com/questions/24284612/failed-to-json-marshal-map-with-non-string-keys/24284721#24284721
//"Map values encode as JSON objects. The map's key type must be string; the object keys are used directly as map keys."
func (jobMgmt *JobMgmt) GetJobs2() map[string] JobInfo{
	jobMgmt.rwMutex.RLock()
	defer jobMgmt.rwMutex.RUnlock()
	jobMap := make(map[string] JobInfo, len(jobMgmt.jobMap))
	for jobId, jobInfo := range jobMgmt.jobMap {
		//NOTE: Storing pointer looks moare reasonable, however &jobInfo is consistant during loop!
		jobMap[fmt.Sprintf("%v", jobId)] = jobInfo
	}
	return jobMap
}

func (jobMgmt *JobMgmt) ForceRun(jobInfo JobInfo) {
	log.Infoln("fouce run job ", jobInfo.Name)
	jobInfo.Run()
}


// ----------------------- scheduler module interface ------------------------- //
func NewJobMgmt(taskChan chan TaskInfo) *JobMgmt{
	return &JobMgmt {
		taskChan: taskChan,
		cron: cron.New(),
		jobIdMap: make(map[int64] int64),
		jobMap: make(map[int64] JobInfo),
	}
}

func (jobMgmt *JobMgmt) Start() {
	jobMgmt.cron.Start()

}

func (jobMgmt *JobMgmt) Stop() {
	jobMgmt.cron.Stop()
}

//Must be invoked before Start()
func (jobMgmt *JobMgmt) RestorePersistedJobs() error {
	jobMap, err := PersistGetJobs()
	if err != nil {
		//TOD: metrics
		return err
	}
	jobMgmt.rwMutex.Lock()
	defer jobMgmt.rwMutex.Unlock()
	jobMgmt.jobMap = jobMap
	for jobId, cronJobId := range jobMgmt.jobIdMap {
		jobMgmt.cron.DelJob(cronJobId)
		delete(jobMgmt.jobIdMap, jobId)
	}
	for jobId, jobInfo := range jobMap {
		var cronJobId2 int64 = 0
		if jobInfo.Enabled {
			cronJobId, err := jobMgmt.cron.AddJob(jobInfo.Schedule, jobInfo)
			if err != nil {
				panic(err)
			}
			jobMgmt.jobIdMap[jobId] = cronJobId
			cronJobId2 = cronJobId
		}
		log.Infoln("restored job ", jobId, cronJobId2, jobInfo.Name)
	}
	return nil
}
