package main

import (
	"testing"
	"os"
)

var (
	gJobList []JobInfo
)

func TestPutGetJob(t *testing.T) {
	for i, jobInfo := range gJobList {
		if err :=  PersistPutJob(int64(i), jobInfo); err != nil {
			t.FailNow()
		}
		jobInfo2, err := PersistGetJob(int64(i))
		if err!= nil || jobInfo2 != jobInfo {
			t.FailNow()
		}
	}
	jobMap, err := PersistGetJobs()
	if err != nil || len(jobMap) != len(gJobList) {
		t.FailNow()
	}
	for i, jobInfo := range gJobList {
		if jobInfo2, found :=  jobMap[int64(i)]; !found || jobInfo != jobInfo2 {
			t.FailNow()
		}
	}
}

func TestDelJob(t *testing.T) {
	for i, jobInfo := range gJobList {
		if err :=  PersistPutJob(int64(i), jobInfo); err != nil {
			t.FailNow()
		}
		if err :=  PersistDelJob(int64(i)); err != nil {
			t.FailNow()
		}
	}
}

func TestDelJobs(t *testing.T) {
	for i, jobInfo := range gJobList {
		if err :=  PersistPutJob(int64(i), jobInfo); err != nil {
			t.FailNow()
		}
	}
	if err := PersistDelJobs(); err != nil {
		t.FailNow()
	}
	jobMap, err := PersistGetJobs()
	if err != nil || len(jobMap) != 0 {
		t.FailNow()
	}
}

func TestMain(m *testing.M) {
	etcdClient, path, err := NewEtcdClient("etcd://127.0.0.1:2379/chronos-go")
	if err != nil {
		panic(err)
	}
	//Note: Can use global variables defined at other files of the same package.
	gEtcdClient = etcdClient
	gEtcdPath = path

	gJobList = []JobInfo {
		{
			Name: "job_java_1",
			Enabled: true,
			Schedule: "*/10 * * * * ?",
			DockerImage: "hub.cloud.ctripcorp.com/container/centos7_java_linuxjob:latest",
			Command: "hostname -I; ping -c3 127.0.0.1; java -version",
			Cpus: 2,
			Mem: 10,
		},
		{
			Name: "job_php_1",
			Enabled: true,
			Schedule: "*/30 * * * * ?",
			DockerImage: "hub.cloud.ctripcorp.com/container/centos7_php_linuxjob:latest",
			Command: "hostname -I; ping -c3 127.0.0.1; php -v",
			Cpus: 1,
			Mem: 100,
		},
		{
			Name: "job_php_2",
			Enabled: true,
			Schedule: "*/30 * * * * ?",
			DockerImage: "hub.cloud.ctripcorp.com/container/centos7_php_linuxjob:latest",
			Command: "hostname -I; ping -c3 127.0.0.1; php -v",
			Cpus: 1,
			Mem: 100,
		},
	}

	os.Exit(m.Run())
}

