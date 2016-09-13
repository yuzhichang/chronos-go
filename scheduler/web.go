package main

//go:generate swagger generate spec -o swagger.json
import (
	"fmt"
	"errors"
	"strconv"
	"encoding/json"
	"sync/atomic"
	"net/http"
	"github.com/gorilla/mux"
	log "github.com/golang/glog"
)

const (
	HTTPprotocol = "http://"
)

/**
Endpoints	Method	Description
/api/jobs	GET	Return a list of jobs with optional parameters
/api/jobs	POST	Create a job
/api/jobs/XXX	PUT	Update a job's information
/api/jobs/XXX	DELETE	Delete a user

*/

//curl -Lv -H "Content-Type: application/json" -X POST -d '{"name":"job1","enabled":true,"schedule":"*/10 * * * * ?","dockerimage":"hub.cloud.ctripcorp.com/container/centos7_php_linuxjob:latest", "command": "hostname -I; ping -c3 127.0.0.1; php -v", "cpus": 1, "mem": 100}' http://localhost:8080/api/jobs
func JobCreate(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	jobInfo := JobInfo{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&jobInfo)
	if err != nil {
		log.Info("failed to decode request body as JobInfo json!")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	
	jobId := gJobMgmt.AddJob(jobInfo)
	output := fmt.Sprintf("%v", jobId)
	fmt.Fprintln(w, string(output))
}

//curl -Lv http://localhost:8080/api/jobs
func JobsRead(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	w.Header().Set("Pragma","no-cache")
	jobMap := gJobMgmt.GetJobs2()
	output, err := json.Marshal(jobMap)
	if err != nil {
		log.Error(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, string(output))
}

//curl -Lv -X DELETE http://localhost:8080/api/jobs
func JobsDelete(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	w.Header().Set("Pragma","no-cache")
	gJobMgmt.ClearJobs()
}

func parseId(w http.ResponseWriter, r *http.Request) (int64, error) {
	w.Header().Set("Pragma","no-cache")
	urlParams := mux.Vars(r)
	id, err := strconv.Atoi(urlParams["id"])
	if err != nil {
		log.Error("failed to parse job id")
		w.WriteHeader(http.StatusBadRequest)
		return 0, errors.New("failed to parse job id")
	}
	return int64(id), nil
}

//curl -Lv http://localhost:8080/api/jobs/1
func JobRead(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	if jobId, err := parseId(w, r); err == nil {
		jobInfo, err := gJobMgmt.GetJob(jobId)
		if err != nil{
			w.WriteHeader(http.StatusBadRequest)
			return
		} else {
			output,_ := json.Marshal(jobInfo)
			fmt.Fprintln(w,string(output))
		}
	}
}

//curl -Lv -H "Content-Type: application/json" -X PUT -d '{"name":"job1","enabled":false,"schedule":"*/10 * * * * ?","dockerimage":"hub.cloud.ctripcorp.com/container/centos7_php_linuxjob:latest", "command": "hostname -I; ping -c3 127.0.0.1; php -v", "cpus": 1, "mem": 100}' http://localhost:8080/api/jobs/1
func JobUpdate(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	if jobId, err := parseId(w, r); err == nil {
		jobInfo := JobInfo{}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&jobInfo)
		if err != nil {
			fmt.Println("failed to decode request body as JobInfo json!")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gJobMgmt.UpdateJob(jobId, jobInfo)
	}
}
	
//curl -Lv -X DELETE http://localhost:8080/api/jobs/1
func JobDelete(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	if jobId, err := parseId(w, r); err == nil {
		gJobMgmt.DelJob(jobId)
	}
}

//curl -Lv -H "Content-Type: application/json" -X POST -d '{"name":"job1","enabled":false,"schedule":"*/10 * * * * ?","dockerimage":"hub.cloud.ctripcorp.com/container/centos7_php_linuxjob:latest", "command": "hostname -I; ping -c3 127.0.0.1; php -v", "cpus": 1, "mem": 100}' http://localhost:8080/api/jobs/forcerun
func JobForceRun(w http.ResponseWriter, r *http.Request) {
	log.Info(*r)
	if 0==atomic.LoadInt32(&gIsLeader) {
		RedirectToLeader(w, r)
		return
	}
	jobInfo := JobInfo{}
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&jobInfo)
	if err != nil {
		fmt.Println("failed to decode request body as JobInfo json!")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	gJobMgmt.ForceRun(jobInfo)
}

func RedirectToLeader(w http.ResponseWriter, r *http.Request) {
	if len(gLeaderAddr)==0 {
		log.Info("404 bad request since I don't know who is the leader")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	redirectURL := HTTPprotocol + gLeaderAddr + r.RequestURI
	log.Info("redirecting to " + redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
}

//TODO: graceful shutdown http.ListenAndServe is a bit involved.
//https://github.com/golang/go/issues/4674
//http://siddontang.com/2015/01/25/stop-server-gracefully/
func RunWebServer(webPort uint16) {
	routes := mux.NewRouter()
	routes.HandleFunc("/api/jobs", JobCreate).Methods("POST") //Create
	routes.HandleFunc("/api/jobs", JobsRead).Methods("GET") //Read
	routes.HandleFunc("/api/jobs", JobsDelete).Methods("DELETE") //Delete
	routes.HandleFunc("/api/jobs/{id:[0-9]+}", JobRead).Methods("GET") //Read
	routes.HandleFunc("/api/jobs/{id:[0-9]+}", JobUpdate).Methods("PUT") //Update
	routes.HandleFunc("/api/jobs/{id:[0-9]+}", JobDelete).Methods("DELETE") //Delete
	routes.HandleFunc("/api/jobs/forcerun", JobForceRun).Methods("POST") //force run a job
	http.Handle("/", routes)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", webPort), nil))
}
