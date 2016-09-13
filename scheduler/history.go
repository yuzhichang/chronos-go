package main

/*
127.0.0.1:6379> hmset jobs/job1/state lastSuccess 0901 lastError 0831
OK
127.0.0.1:6379> hmget jobs/job1/state lastSuccess lastError
1) "0901"
2) "0831"
127.0.0.1:6379> 

127.0.0.1:6379> zadd jobs/job1/tasks 1 task1
(integer) 1
127.0.0.1:6379> zadd jobs/job1/tasks 2 task2
(integer) 1
127.0.0.1:6379> zadd jobs/job1/tasks 3 task3
(integer) 1
127.0.0.1:6379> ZREMRANGEBYSCORE jobs/job1/tasks -inf 2
(integer) 2
127.0.0.1:6379> ZRANGE jobs/job1/tasks 0 -1 WITHSCORES
1) "task3"
2) "3"
127.0.0.1:6379> 

127.0.0.1:6379> hsetnx tasks/task1 created 20160201
(integer) 1
127.0.0.1:6379> hget tasks/task1 created
"20160201"
127.0.0.1:6379> hsetnx tasks/task1 created 20160202
(integer) 0
127.0.0.1:6379> hsetnx tasks/task1 finished 20160203
(integer) 1
127.0.0.1:6379> hmget tasks/task1 created finished running
1) "20160201"
2) "20160203"
3) (nil)
127.0.0.1:6379> 


key: jobs/<jobName>/tasks
val: a sorted set of (taskName, taskTimestamp)

key: tasks/<taskName>
val: a hast table with following fields: succeed, TASK_STAGING, TASK_STARTING, TASK_RUNNING etc. each field value is timestamp when the event occurred.

*/

import (
	"fmt"
	"strconv"
	"time"
	"github.com/garyburd/redigo/redis"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

var (
	gRedisPool *redis.Pool
)


func InitRedisConnPool(server, password string) {
	gRedisPool = &redis.Pool{
        MaxIdle: 3,
        IdleTimeout: 240 * time.Second,
        Dial: func () (redis.Conn, error) {
            c, err := redis.Dial("tcp", server)
            if err != nil {
                return nil, err
            }
            if len(password) != 0 {
		if _, err := c.Do("AUTH", password); err != nil {
                    c.Close()
			return nil, err
		}
            }
            return c, err
        },
        TestOnBorrow: func(c redis.Conn, t time.Time) error {
            if time.Since(t) < time.Minute {
                return nil
            }
            _, err := c.Do("PING")
            return err
        },
    }
}

func HistPutTask(taskInfo TaskInfo) error {
	key := fmt.Sprintf("jobs/%s/tasks", taskInfo.JobInfo.Name)
	conn := gRedisPool.Get()
	defer conn.Close()
	_, err := conn.Do("ZADD", key, taskInfo.Timestamp, taskInfo.TaskName)
	return err
}

func HistPutTaskStatus(status *mesos.TaskStatus) error {
	taskName := status.TaskId.GetValue()
	key := fmt.Sprintf("tasks/%s", taskName)
	field := status.State.Enum().String()
	val := fmt.Sprintf("%v", *status.Timestamp)

	conn := gRedisPool.Get()
	defer conn.Close()
	state := status.GetState()
	if state == mesos.TaskState_TASK_FINISHED {
		conn.Send("HSETNX", key, "succeed", "1")
	} else if state == mesos.TaskState_TASK_LOST ||
		state == mesos.TaskState_TASK_KILLED ||
		state == mesos.TaskState_TASK_FAILED ||
		state == mesos.TaskState_TASK_ERROR {
		conn.Send("HSETNX", key, "succeed", "0")
	}
	_, err := conn.Do("HSETNX", key, field, val)
	return err
}

type TaskHistory struct {
	TaskName string
	Succeed int
}

func HistGetJob(jobName string) ([]TaskHistory, error) {
	conn := gRedisPool.Get()
	defer conn.Close()
	taskHist := make([]TaskHistory, 0)
	array, err := conn.Do("ZRANGE", fmt.Sprintf("jobs/%s/tasks", jobName), 0, -1)
	if err != nil {
		return taskHist, err
	}
	taskList := make([]string, 0)
	for _, ele := range array.([]interface{}) {
		switch e := ele.(type) {
		case []byte:
			taskList = append(taskList, string(e))
		default:
			panic("type assertion error")
		}
	}
	
	for _, taskName := range taskList {
		conn.Send("HGET", fmt.Sprintf("tasks/%s", taskName), "succeed")
	}
	conn.Flush()
	succeedList := make([]int, 0)
	for _, _ = range taskList {
		resp, err := conn.Receive()
		if err != nil {
			succeedList = append(succeedList, 0)
		} else {
			switch e := resp.(type) {
			case []byte:
				i, _ := strconv.Atoi(string(e))
				succeedList = append(succeedList, i)
			default:
				succeedList = append(succeedList, 0)
			}
		}
	}
	for i, taskName := range taskList {
		taskHist = append(taskHist, TaskHistory{TaskName: taskName, Succeed: succeedList[i]})
	}
	return taskHist, nil
}
