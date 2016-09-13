package main
/*
persistence of gJobMgmt.jobMap

based on "ectdctl get/del" impl code github.com/coreos/etcd/etcdctl/ctlv3/command/{get_command.go, del_command.go}.
*/

import (
	"errors"
	log "github.com/golang/glog"
	"fmt"
	"strconv"
	"encoding/json"
	"golang.org/x/net/context"
	"github.com/coreos/etcd/clientv3"
)

/*
$ ETCDCTL_API=3 ./etcdctl put /jobs/1 111
OK
$ ETCDCTL_API=3 ./etcdctl put /jobs/2 222
OK
$ ETCDCTL_API=3 ./etcdctl get --prefix /jobs
/jobs/1
111
/jobs/2
222

*/

func PersistPutJob(jobId int64, jobInfo JobInfo) error {
	key := fmt.Sprintf("%s/jobs/%d", gEtcdPath, jobId)
	val, err := json.Marshal(jobInfo)
	if err != nil {
		return err
	}
	valStr := string(val)
	_, err = gEtcdClient.Put(context.TODO(), key, valStr)
	if err != nil {
		return err
	}
	return nil
}

func PersistGetJob(jobId int64) (JobInfo, error) {
	jobInfo := JobInfo{}
	key := fmt.Sprintf("%s/jobs/%d", gEtcdPath, jobId)
	resp, err := gEtcdClient.Get(context.TODO(), key)
	if err != nil {
		return jobInfo, err
	}
	if len(resp.Kvs) != 1 {
		return jobInfo, errors.New(fmt.Sprintf("response Kvs length %d does not match expectation 1", len(resp.Kvs)))
	}
	k, v := string(resp.Kvs[0].Key), string(resp.Kvs[0].Value)
	if k != key {
		return jobInfo, errors.New(fmt.Sprintf("response key %v does not match expectation %v", k, key))
	}
	err = json.Unmarshal([]byte(v), &jobInfo)
	if err != nil {
		log.Info("failed to decode JobInfo %v", v)
		return jobInfo, errors.New(fmt.Sprintf("failed to decode JobInfo %v", v))
	}
	return jobInfo, nil
}

func PersistDelJob(jobId int64) error {
	key := fmt.Sprintf("%s/jobs/%d", gEtcdPath, jobId)
	_, err := gEtcdClient.Delete(context.TODO(), key)
	if err != nil {
		return err
	}
	return nil
}

func PersistGetJobs() (map[int64] JobInfo, error) {
	jobMap := make(map[int64] JobInfo)
	prefix := fmt.Sprintf("%s/jobs/", gEtcdPath)
	opts := []clientv3.OpOption{}
	opts = append(opts, clientv3.WithPrefix())
	resp, err := gEtcdClient.Get(context.TODO(), prefix, opts...)
	if err != nil {
		return jobMap, err
	}

	for _, kv := range resp.Kvs {
		k, v := string(kv.Key), string(kv.Value)
		if len(k) <= len(prefix) {
			log.Info("invalid key length %v", k)
			continue
		}
		jobId, err := strconv.Atoi(k[len(prefix):])
		if err != nil {
			log.Info("failed to decode jobId %v", k)
			continue
		}
		jobInfo := JobInfo{}
		err = json.Unmarshal([]byte(v), &jobInfo)
		if err != nil {
			log.Info("failed to decode JobInfo %v", v)
			continue
		}
		jobMap[int64(jobId)] = jobInfo
	}
	return jobMap, nil
}

func PersistDelJobs() error {
	key := fmt.Sprintf("%s/jobs/", gEtcdPath)
	opts := []clientv3.OpOption{}
	opts = append(opts, clientv3.WithPrefix())
	_, err := gEtcdClient.Delete(context.TODO(), key, opts...)
	if err != nil {
		return err
	}
	return nil
}
