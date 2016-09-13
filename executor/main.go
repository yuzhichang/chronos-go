package main

import (
	"bytes"
	"flag"
	"fmt"
	"syscall"
	"os"
	"os/signal"
	osexec "os/exec"
	"time"
	exec "github.com/mesos/mesos-go/executor"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

var (
	Stopping chan string
	watchDog *time.Timer
	idleTimeout time.Duration
)

type chronosExecutor struct {
	tasksLaunched int
}

func NewChronosExecutor() *chronosExecutor {
	return &chronosExecutor{tasksLaunched: 0}
}

func runTaskData(data []byte) (err error) {
	parts := bytes.SplitN(data, []byte{'|'}, 2)
	var executorArgs, command string

	if len(parts) == 1 {
		command = string(parts[0])
	} else {
		executorArgs = string(parts[0])
		command = string(parts[1])
	}

	fmt.Printf("executorArgs is %s\n", executorArgs)
	fmt.Printf("command is %s\n", command)

	// TODO(jzl): replace "sh -c" with a more proper command runner
	cmd := osexec.Command("sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("gonna run %s\n", command)
	err = cmd.Start()
	if err != nil {
		return
	}

	err = cmd.Wait()
	if err != nil {
		return
	}
	return nil
}

func (cexec *chronosExecutor) Registered(driver exec.ExecutorDriver, execInfo *mesos.ExecutorInfo,
	fwinfo *mesos.FrameworkInfo, slaveInfo *mesos.SlaveInfo) {
	fmt.Println("Registered Executor on slave ", slaveInfo.GetHostname())
}

func (cexec *chronosExecutor) Reregistered(driver exec.ExecutorDriver, slaveInfo *mesos.SlaveInfo) {
	fmt.Println("Re-registered Executor on slave ", slaveInfo.GetHostname())
}

func (cexec *chronosExecutor) Disconnected(exec.ExecutorDriver) {
	fmt.Println("Executor disconnected.")
}

func (cexec *chronosExecutor) LaunchTask(driver exec.ExecutorDriver, taskInfo *mesos.TaskInfo) {
	fmt.Printf("Launching task %s(%d th/rd) with command %s\n", taskInfo.GetTaskId().String(),
		cexec.tasksLaunched, taskInfo.GetData())

	watchDog.Reset(idleTimeout)

	sendStatus := func(state mesos.TaskState) error {
		status := &mesos.TaskStatus{TaskId: taskInfo.GetTaskId(), State: state.Enum()}

		_, err := driver.SendStatusUpdate(status)
		if err != nil {
			return err
		}
		return nil
	}

	if err := sendStatus(mesos.TaskState_TASK_STARTING); err != nil {
		fmt.Println("error sending FAILED", err)
		return
	}

	cexec.tasksLaunched++

	if err := sendStatus(mesos.TaskState_TASK_RUNNING); err != nil {
		fmt.Println("error sending FAILED", err)
		return
	}

	err := runTaskData(taskInfo.GetData())

	if err != nil {
		if err := sendStatus(mesos.TaskState_TASK_FAILED); err != nil {
			fmt.Println("error sending FAILED", err)
			return
		}
	} else {
		if err := sendStatus(mesos.TaskState_TASK_FINISHED); err != nil {
			fmt.Println("error sending FINISHED", err)
			return
		}
	}
}

func (cexec *chronosExecutor) KillTask(driver exec.ExecutorDriver, taskId *mesos.TaskID) {
	fmt.Println("Kill task %s", taskId.Value)
}

func (cexec *chronosExecutor) FrameworkMessage(driver exec.ExecutorDriver, msg string) {
	fmt.Println("Got framework message: ", msg)
	switch {
	case msg == "exit":
		if _, err := driver.Stop(); err != nil {
			fmt.Println(err)
		}
	}
}

func (cexec *chronosExecutor) Shutdown(exec.ExecutorDriver) {
	fmt.Println("Shutting down the executor")
}

func (cexec *chronosExecutor) Error(driver exec.ExecutorDriver, err string) {
	fmt.Println("Got error message:", err)
}

func RunWatchDog() {
	select {
	case <-watchDog.C:
		Stopping<- "idle for too long, will exit."
	}
}

func main() {
	flag.DurationVar(&idleTimeout, "idle-timeout", 30*time.Minute,
		"maximum time to wait for new tasks.")
	Stopping = make(chan string, 1)
	flag.Parse()
	
	watchDog = time.NewTimer(idleTimeout)
	go RunWatchDog()

	fmt.Println("Starting Chronos Executor (Go)")

	dconfig := exec.DriverConfig{
		Executor: NewChronosExecutor(),
	}

	driver, err := exec.NewMesosExecutorDriver(dconfig)

	if err != nil {
		fmt.Println("Unable to create a ExecutorDriver ", err.Error())
	}

	_, err = driver.Start()
	if err != nil {
		fmt.Println("Got error:", err)
		return
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			select {
			case s := <-c:
				signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
				fmt.Printf("Got signal %s, will exit.\n", s)
				driver.Stop()
			case msg := <-Stopping:
				signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
				fmt.Printf("%s.\n", msg)
				driver.Stop()
			}
		}
	}()

	fmt.Println("Executor process has started and running.")
	_, err = driver.Join()
	if err != nil {
		fmt.Println("driver failed:", err)
	}
	fmt.Println("executor terminating")
}
