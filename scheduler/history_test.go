package main

import (
	"fmt"
	"testing"
)

const (
	NUM_TASKS int = 3
)

func TestHistGetTasks(t *testing.T) {
	jobInfo := JobInfo {
		Name: "ping1",
		Enabled: true,
		Schedule: "*/13 * * * * ?",
		DockerImage: "ubuntu:14.04",
		Command: "hostname -I && ping -c1 127.0.0.1",
		Cpus: 2,
		Mem: 10,
	}
	taskList := make([]TaskInfo, 0)
	for i:=0; i<NUM_TASKS; i++ {
		taskList = append(taskList, TaskInfo {
			JobInfo: jobInfo,
			Timestamp: int64(i*100),
			TaskName: fmt.Sprintf("%s-%d", jobInfo.Name, i*100),
		})
	}

	for i:=0; i<NUM_TASKS; i++ {
		if err := HistPutTask(taskList[i]); err != nil {
			fmt.Println(err)
			t.FailNow()
		}
	}

	jobHist, err := HistGetJob(jobInfo.Name)
	if err != nil {
		fmt.Println(err)
		t.FailNow()
	}
	for _, taskHist := range jobHist {
		fmt.Printf("%+v\n", taskHist)
	}
}

