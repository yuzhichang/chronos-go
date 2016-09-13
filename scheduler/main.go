package main

import (
	"flag"
	"time"
	"fmt"
	"sync/atomic"
	log "github.com/golang/glog"
	"os"
	"os/signal"
	"syscall"
	"runtime/pprof"
	"github.com/coreos/etcd/clientv3"
)

const TASK_QUEUE_SIZE int = 10000

var (
	//configuration
	gMesosZk string
	gBindingAddress string
	gBindingPort uint16
	gWebPort uint16
	gAddSample bool
	gRunSample bool
	gEtcdUrl string
	gRedisServer string
	gRedisPassword string

	//election
	gLeaderAddr string
	gIsLeader int32
	//	gIsLeader bool //bool doesn't ensure volatile

	gJobMgmt *JobMgmt
	gScheduler *Chronos
	gTaskChan chan TaskInfo
	gTaskBacklog []TaskInfo
	gEtcdClient *clientv3.Client
	gEtcdPath string
)

func AddSampleJobs() map[int64] JobInfo{
	jobList := []JobInfo {
		{
			Name: "ping1",
			Enabled: true,
			Schedule: "*/13 * * * * ?",
			DockerImage: "ubuntu:14.04",
			Command: "hostname -I && ping -c1 127.0.0.1",
			Cpus: 2,
			Mem: 10,
		},
		{
			Name: "ping2",
			Enabled: true,
			Schedule: "*/29 * * * * ?",
			DockerImage: "ubuntu:14.04",
			Command: "hostname -I && ping -c1 127.0.0.1",
			Cpus: 1,
			Mem: 100,
		},
		{
			Name: "ping3",
			Enabled: true,
			Schedule: "*/37 * * * * ?",
			DockerImage: "ubuntu:14.04",
			Command: "hostname -I && ping -c1 127.0.0.1",
			Cpus: 1,
			Mem: 100,
		},
	}

	sampleJobMap := make(map[int64] JobInfo)
	for _, jobInfo := range jobList {
		jobId := gJobMgmt.AddJob(jobInfo)
		sampleJobMap[jobId] = jobInfo
	}
	return sampleJobMap
}

func RunSampleJobs() {
	sampleJobMap := AddSampleJobs()
	sampleJobIds := make([]int64, 0, len(sampleJobMap))
	for jobId, _ := range sampleJobMap {
		sampleJobIds = append(sampleJobIds, jobId)
	}
	jobId := sampleJobIds[len(sampleJobIds)-1]
	jobInfo := sampleJobMap[jobId]
	
	num := 0
	chan1 := time.After(7 * time.Second)
	chan2 := time.After(180 * time.Second)
	for {
		select {
		case <-chan1:
			chan1 = time.After(7 * time.Second)
			gJobMgmt.ForceRun(jobInfo)
		case <-chan2:
			chan2 = time.After(60 * time.Second)
			interval := 7+num%50
			switch {
			case num%2 == 0:
				jobInfo.Enabled = true
			default:
				jobInfo.Enabled = false
			}
			jobInfo.Name = fmt.Sprintf("update%v_%v", num, interval)
			jobInfo.Schedule = fmt.Sprintf("*/%v * * * * ?", interval)
			gJobMgmt.UpdateJob(jobId, jobInfo)
			num += 1
		}
	}
}

func switchToFollower() {
	gJobMgmt.Stop()
	gScheduler.Stop()
	close(gTaskChan)
}

func switchToLeader() {
	gTaskChan := make(chan TaskInfo, TASK_QUEUE_SIZE)
	gScheduler = NewScheduler(gMesosZk, gBindingAddress, gBindingPort, gTaskChan)
	gScheduler.Start()

	gJobMgmt = NewJobMgmt(gTaskChan)
	if err := gJobMgmt.RestorePersistedJobs(); err != nil {
		panic(err)
	}
	gJobMgmt.Start()

	if gRunSample {
		go RunSampleJobs()
	} else if gAddSample {
		AddSampleJobs()
	}
}

// ----------------------- func main() ------------------------- //
func main() {
	mesosZk := flag.String("mesos_zk", "zk://127.0.0.1:2181/mesos", "Detect mesos leader from the ZooKeeper url")
	bindingAddress := flag.String("binding_address", "127.0.0.1", "The IPv4 address part of the framework's upid. The default localhost cause the framework not able to talk with remove mesos master.")
	bindingPort := flag.Int("binding_port", 0, "The TCP port part of the framework's upid. The default 0 let the framework pick a random port at runtime.")
	webPort := flag.Int("web_port", 8080, "The TCP port on which the web server listens on.")
	addSample := flag.Bool("add_sample", false, "add sample jobs before servicing REST API")
	runSample := flag.Bool("run_sample", false, "run sample jobs instead of servicing REST API")
	etcdUrl := flag.String("etcd_url", "etcd://127.0.0.1:2379/chronos-go", "etcd cluster endpoints")
	redisServer := flag.String("redis_server", "127.0.0.1:6379", "redis server ip and port")
	flag.Parse()
	gMesosZk = *mesosZk
	gBindingAddress = *bindingAddress
	gBindingPort = uint16(*bindingPort)
	gAddSample = *addSample
	gRunSample = *runSample
	gEtcdUrl = *etcdUrl
	gRedisServer = *redisServer
	InitRedisConnPool(gRedisServer, "")

	go RunWebServer(uint16(*webPort))

	proposal := fmt.Sprintf("%s:%d", *bindingAddress, uint16(*webPort))
	leaderCh := make(chan string)
	etcdClient, path, err := NewEtcdClient(*etcdUrl)
	if err != nil {
		panic(err)
	}
	go Elect(etcdClient, path, proposal, leaderCh)
	gEtcdClient = etcdClient
	gEtcdPath = path
	
	go func() {
		sigu := make(chan os.Signal, 1)
		signal.Notify(sigu, syscall.SIGUSR1)
		for {
			select {
			case <-sigu:
				//https://golang.org/pkg/runtime/
				//The GOTRACEBACK variable controls the amount of output generated when a Go program fails due to an unrecovered panic or an unexpected runtime condition
				//http://stackoverflow.com/questions/19094099/how-to-dump-goroutine-stacktraces
				pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
				//SIGQUIT print stack of all goroutines and quit.
			}
		}
		
	}()

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill)
	atomic.StoreInt32(&gIsLeader, 0)
	for {
		select {
		case leader := <-leaderCh:
			gLeaderAddr = leader
			if proposal==leader {
				log.Info("I'm the leader!")
				if 0==atomic.LoadInt32(&gIsLeader) {
					atomic.StoreInt32(&gIsLeader, 1)
					log.Info("state transition follower-> leader")
					switchToLeader()
				} else {
					log.Info("state transition leader -> leader")
				}
			} else {
				log.Info("new leader's proposal: ", leader)
				if 0==atomic.LoadInt32(&gIsLeader) {
					log.Info("state transition follower -> follower")
				} else {
					atomic.StoreInt32(&gIsLeader, 0)
					log.Warning("state transition leader -> follower")
					switchToFollower()
				}
			}
		case <-sigc:
			log.Info("exiting...")
			time.Sleep(2*time.Second)
			log.Info("exited")
			return
		}
	}
}
