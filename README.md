Introduction
--------
Chronos(https://github.com/mesos/chronos) is a famous job scheduler for Mesos. However it's nearly unmainted in years. There are many bugs reported without any reply, many pull requests opened without any comment.

I'm using Chronos in product environment and found several cirtical issues due to its bad design. Since Chronos code base is small(<10K lines), I decide to rewrite it in my favorit language - Golang.

What's Wrong with Chronos
--------
 - If Zookeeper split-brain and recovery later, mesos master cluster can recovery quickly, but chronos keeps cannon without manual restart.
 - Many places read/write a shared jobGraph without careful lock protection. There's race condition between http requests(read jobGraph), task statusUpdate (write jobGraph), and updating stream(read/write jobGraph). Http request may timed out/internal error etc.
 - Scala/Java code using mesos-0.28 driver(shared library written in C++). Mixing-languages introduces complexity and is error prone. C++ and Java code calls each other. Here's an example: https://github.com/mesos/chronos/issues/719.
 - Doesn't handle possible libprocess message-dropping. Persisted tasks in Zookeeper keeps increasing under heavy load.
 - To calculate the next schedule of all jobs, chronos scan all jobs every minute and updats streams. Handling task failure and updating job definition also updating the stream. It's urgly, complex, low-efficient, and error-prone.
 - Chronos runs each task in a newly created container. Docker daemon is very slow to starup containers(<200 per seconds). Frequent container creation/destroy affects system capacity(not scale well) and stability(frequent failure and soft lockup etc.). Idealy we could make all tasks of the same job share the same container to avoid container startup/teardown cost.

Features and Plans
--------
 - job management(CRUD, disable/enable) - web api and impl
 - job force run - web api and impl
 - generate tasks per cron-like spec (Chronos-go and Chronos support second granularity, while Cron supports minute granularity)
 - run tasks as docker container
 - detect mesos master from ZK
 - support local and remote mesos master
 - check active tasks to handle possible libprocess message dropping
 - HA, election at startup via etcd
 - HA, election state transition actions (web server redirect w/o, jobMgms start/stop, scheduler start/stop)
 - HA, jobInfo persistence in etcd, jobMgmt load/store jobs
 - job history (task list) via redis
 - (TODO) kill tasks of a job
 - (TODO) kill a job shall kill tasks of that job
 - (TODO) metrics api: impl and api
 - (TODO) Log task events into ES
 - (TODO) customize executor to avoid container creation/teardown
 - (TODO) Swagger API doc
 - (TODO) Make self as a container
 - (TODO) more schedule policy, such as try best to assign tasks, slave load-balance etc.


How to Run Sample Jobs
--------
 - Install and run docker-engine per https://docs.docker.com/engine/installation/.
 - Build and run etcd 3.0+ per https://github.com/coreos/etcd.
 - Install glide per https://github.com/Masterminds/glide.

```sh
zhichyu@meizu:~$ rm -fr proj
zhichyu@meizu:~$ mkdir -p proj/src
zhichyu@meizu:~$ cd proj/src/
zhichyu@meizu:~/proj/src$ git clone git@github.com:yuzhichang/chronos-go.git
zhichyu@meizu:~/proj/src$ cd chronos-go/compose
zhichyu@meizu:~/proj/src/chronos-go/compose$ docker-compose up -d
zhichyu@meizu:~/proj/src/chronos-go/compose$ cd ../scheduler
zhichyu@meizu:~/proj/src/chronos-go/scheduler$ glide install
zhichyu@meizu:~/proj/src/chronos-go/scheduler$ export GOPATH=$HOME/proj
zhichyu@meizu:~/proj/src/chronos-go/scheduler$ go build .
zhichyu@meizu:~/proj/src/chronos-go/scheduler$ ./scheduler -logtostderr -run_sample
I0905 22:37:40.212753   17429 election.go:80] my proposal: 127.0.0.1:8080
I0905 22:37:40.216029   17429 election.go:93] I'v been elected as leader: /chronos-go/election/506956fd23119aef 127.0.0.1:8080
I0905 22:37:40.239820   17429 election.go:47] leader: /chronos-go/election/506956fd23119aef 127.0.0.1:8080
I0905 22:37:40.239845   17429 main.go:191] I'm the leader!
I0905 22:37:40.239891   17429 main.go:194] state transition follower-> leader
I0905 22:37:40.250688   17429 scheduler.go:334] Initializing mesos scheduler driver
I0905 22:37:40.251776   17429 scheduler.go:833] Starting the scheduler driver...
I0905 22:37:40.251843   17429 http_transporter.go:383] listening on 127.0.0.1 port 40867
I0905 22:37:40.251861   17429 scheduler.go:850] Mesos scheduler driver started with PID=scheduler(1)@127.0.0.1:40867
I0905 22:37:40.251921   17429 scheduler.go:1053] Scheduler driver running.  Waiting to be stopped.
2016/09/05 22:37:40 Connected to 127.0.0.1:2181
2016/09/05 22:37:40 Authenticated: id=96534243288154134, timeout=40000
I0905 22:37:40.254682   17429 jobMgmt.go:67] added job  1 1 ping1
I0905 22:37:40.255350   17429 jobMgmt.go:67] added job  2 2 ping2
I0905 22:37:40.256679   17429 jobMgmt.go:67] added job  3 3 ping3
I0905 22:37:40.258732   17429 scheduler.go:419] New master master@127.0.0.1:5050 detected
I0905 22:37:40.258787   17429 scheduler.go:483] No credentials were provided. Attempting to register scheduler without authentication.
I0905 22:37:40.261351   17429 scheduler.go:583] Framework registered with ID=b3ce0688-d711-4a5e-9991-6bf1eb2a378a-0017
I0905 22:37:40.261613   17429 chronos.go:50] Framework Registered with Master  &MasterInfo{Id:*b3ce0688-d711-4a5e-9991-6bf1eb2a378a,Ip:*16777343,Port:*5050,Pid:*master@127.0.0.1:5050,Hostname:*127.0.0.1,Version:*0.28.1,Address:&Address{Hostname:*127.0.0.1,Ip:*127.0.0.1,Port:*5050,XXX_unrecognized:[],},XXX_unrecognized:[],}
I0905 22:37:40.261724   17429 chronos.go:165] decline all 1 offers since there is no task in backlog
I0905 22:37:45.398378   17429 chronos.go:165] decline all 1 offers since there is no task in backlog
I0905 22:37:47.257009   17429 jobMgmt.go:166] fouce run job  ping3
I0905 22:37:47.257038   17429 jobMgmt.go:29] triggered cron job ping3-1473125867257033635
I0905 22:37:50.406562   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19662 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:37:50.406624   17429 chronos.go:136] generated task ping3-1473125867257033635 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19662
I0905 22:37:50.406651   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19662
I0905 22:37:51.309592   17429 chronos.go:209] Status update: task ping3-1473125867257033635  is in state  TASK_RUNNING
I0905 22:37:51.362245   17429 chronos.go:209] Status update: task ping3-1473125867257033635  is in state  TASK_FINISHED
I0905 22:37:51.364686   17429 chronos.go:165] decline all 1 offers since there is no task in backlog
I0905 22:37:52.000632   17429 jobMgmt.go:29] triggered cron job ping1-1473125872000477832
I0905 22:37:54.258045   17429 jobMgmt.go:166] fouce run job  ping3
I0905 22:37:54.258131   17429 jobMgmt.go:29] triggered cron job ping3-1473125874258115238
I0905 22:37:56.417051   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19664 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:37:56.417164   17429 chronos.go:136] generated task ping1-1473125872000477832 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19664
I0905 22:37:56.417190   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19664
I0905 22:37:57.333813   17429 chronos.go:209] Status update: task ping1-1473125872000477832  is in state  TASK_RUNNING
I0905 22:37:57.369878   17429 chronos.go:209] Status update: task ping1-1473125872000477832  is in state  TASK_FINISHED
I0905 22:37:57.371910   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19665 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:37:57.371924   17429 chronos.go:136] generated task ping3-1473125874258115238 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19665
I0905 22:37:57.371930   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19665
I0905 22:37:58.001596   17429 jobMgmt.go:29] triggered cron job ping2-1473125878001528521
I0905 22:37:58.238770   17429 chronos.go:209] Status update: task ping3-1473125874258115238  is in state  TASK_RUNNING
I0905 22:37:58.282547   17429 chronos.go:209] Status update: task ping3-1473125874258115238  is in state  TASK_FINISHED
I0905 22:37:58.285256   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19666 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:37:58.285299   17429 chronos.go:136] generated task ping2-1473125878001528521 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19666
I0905 22:37:58.285307   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19666
I0905 22:37:59.134579   17429 chronos.go:209] Status update: task ping2-1473125878001528521  is in state  TASK_RUNNING
I0905 22:37:59.202210   17429 chronos.go:209] Status update: task ping2-1473125878001528521  is in state  TASK_FINISHED
I0905 22:37:59.207716   17429 chronos.go:165] decline all 1 offers since there is no task in backlog
I0905 22:38:00.000837   17429 jobMgmt.go:29] triggered cron job ping3-1473125880000783254
I0905 22:38:00.000891   17429 jobMgmt.go:29] triggered cron job ping2-1473125880000868793
I0905 22:38:00.000915   17429 jobMgmt.go:29] triggered cron job ping1-1473125880000897198
I0905 22:38:01.258349   17429 jobMgmt.go:166] fouce run job  ping3
I0905 22:38:01.258402   17429 jobMgmt.go:29] triggered cron job ping3-1473125881258395012
I0905 22:38:04.431277   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19668 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:38:04.431948   17429 chronos.go:136] generated task ping3-1473125880000783254 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19668
I0905 22:38:04.432070   17429 chronos.go:136] generated task ping2-1473125880000868793 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19668
I0905 22:38:04.432241   17429 chronos.go:197] launching 2 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19668
I0905 22:38:05.238099   17429 chronos.go:209] Status update: task ping2-1473125880000868793  is in state  TASK_RUNNING
I0905 22:38:05.308006   17429 chronos.go:209] Status update: task ping2-1473125880000868793  is in state  TASK_FINISHED
I0905 22:38:05.309968   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19669 on slave 127.0.0.1: cpus 1, mem 6859
I0905 22:38:05.309997   17429 chronos.go:201] decline 1 spare offers. currently 2 tasks in backlog
I0905 22:38:05.344770   17429 chronos.go:209] Status update: task ping3-1473125880000783254  is in state  TASK_RUNNING
I0905 22:38:05.409375   17429 chronos.go:209] Status update: task ping3-1473125880000783254  is in state  TASK_FINISHED
I0905 22:38:05.410586   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19670 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:38:05.410608   17429 chronos.go:136] generated task ping1-1473125880000897198 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19670
I0905 22:38:05.410615   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19670
^CI0905 22:38:05.554124   17429 main.go:210] exiting...
I0905 22:38:06.256919   17429 chronos.go:209] Status update: task ping1-1473125880000897198  is in state  TASK_RUNNING
I0905 22:38:06.327130   17429 chronos.go:209] Status update: task ping1-1473125880000897198  is in state  TASK_FINISHED
I0905 22:38:06.328723   17429 chronos.go:188] offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19671 on slave 127.0.0.1: cpus 2, mem 6959
I0905 22:38:06.328735   17429 chronos.go:136] generated task ping3-1473125881258395012 for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19671
I0905 22:38:06.328741   17429 chronos.go:197] launching 1 tasks for offer b3ce0688-d711-4a5e-9991-6bf1eb2a378a-O19671
I0905 22:38:07.189434   17429 chronos.go:209] Status update: task ping3-1473125881258395012  is in state  TASK_RUNNING
I0905 22:38:07.248017   17429 chronos.go:209] Status update: task ping3-1473125881258395012  is in state  TASK_FINISHED
I0905 22:38:07.249382   17429 chronos.go:165] decline all 1 offers since there is no task in backlog
I0905 22:38:07.555648   17429 main.go:212] exited
zhichyu@meizu:~/proj/src/chronos-go/scheduler$
```
