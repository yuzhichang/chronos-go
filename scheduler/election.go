package main
/*
based on "ectdctl elect" impl code github.com/coreos/etcd/etcdctl/ctlv3/command/elect_command.go

I want to cover following cases in unit test:
etcd up, c1 up (expect it's elected as leader), c2 up(expect the leader is c1), c1 down (at c2, expect leader change to c2)
etcd up, c1 up, c2 up, etcd down, c1 down, etcd up (at c2, expect leader changes to c2)
etcd up, c1 up, etcd down (expect c1 retry connecting in endless loop), etcd up, c2 up (expect c1&c2 agree to the same leader in 60s)
c1 up (expect c1 retry connecting in endless loop), etcd up (at c1, expect it's elected as leader)
*/

import (
	"errors"
	log "github.com/golang/glog"
	"fmt"
	"time"
	"net/url"
	"strings"
	"golang.org/x/net/context"
	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
)

func parseResp(resp *clientv3.GetResponse) (string, string, error){
	for _, kv := range resp.Kvs {
		k, v := string(kv.Key), string(kv.Value)
		return k, v, nil
	}
	log.Info(fmt.Sprintf("failed to parse response %+v", resp))
	return "", "", errors.New(fmt.Sprintf("failed to parse response %+v", resp))
}

func observe(ctx context.Context, c *clientv3.Client, election string, leaderCh chan string) error {
	s, err := concurrency.NewSession(c)
	if err != nil {
		return err
	}
	e := concurrency.NewElection(s, election)
	
	donec := make(chan struct{})
	go func() {
		for resp := range e.Observe(ctx) {
			//Kvs could be empty(etcd down, leader down, etcd up):
			//&{Header:cluster_id:3373127551666285087 member_id:13506963981489885289 revision:23 raft_term:4  Kvs:[] More:false Count:0}
			k, v, err := parseResp(&resp)
			if err==nil {
				log.Info(fmt.Sprintf("leader: %s %s", k, v))
				leaderCh<- v
			} else {
				log.Info("got empty response from etcd")
			}
		}
		close(donec)
	}()

	<-donec
	
	select {
	case <-ctx.Done():
	default:
		log.Info("elect: observer lost")
		return errors.New("elect: observer lost")
	}

	return nil
}

func campaign(ctx context.Context, c *clientv3.Client, election string, prop string) error {
	/**
According to https://github.com/coreos/etcd/blob/master/etcdctl/README.md,
The lease length of a leader defaults to 60 seconds. If a candidate is abnormally terminated, election progress may be delayed by up to 60 seconds.
However I haven't notice that long delay.
*/
	s, err := concurrency.NewSession(c, concurrency.WithTTL(10))
	if err != nil {
		return err
	}
	e := concurrency.NewElection(s, election)

	log.Infof("my proposal: %v", prop)
	//Campaign puts a value as eligible for the election. It blocks until it is elected, an error occurs, or the context is cancelled.
	if err = e.Campaign(ctx, prop); err != nil {
		return err
	}

	// print key since elected
	resp, err := c.Get(ctx, e.Key())
	if err != nil {
		return err
	}
	k, v, err := parseResp(resp)
	if err==nil {
		log.Info(fmt.Sprintf("I'v been elected as leader: %s %s", k, v))
	} else {
		log.Info("get empty kv from etcd. maybe session expired?")
	}

	select {
	case <-s.Done():
		log.Info("elect: session expired")
		return errors.New("elect: session expired")
	}

	return e.Resign(context.TODO())
}

type urlParams struct {
	hosts    []string
	path     string
	userName string
	password string
}

// parseEtcdUrl parses the etcd url, for example etcd://127.0.0.1:2379/chronos-go
func parseEtcdUrl(etcdurls string) (*urlParams, error) {
	u, err := url.Parse(etcdurls)
	
	if err != nil {
		log.V(1).Infof("failed to parse url: %v", err)
		return nil, err
	}
	
	if u.Scheme != "etcd" {
		return nil, fmt.Errorf("invalid url scheme for etcd url: '%v'", u.Scheme)
	}
	
	var (
		username = ""
		password = ""
	)
	if u.User != nil {
		username = u.User.Username()
		passwd, _ := u.User.Password()
		password = passwd
	}
	
	return &urlParams{strings.Split(u.Host, ","), u.Path, username, password}, nil
}

func NewEtcdClient(etcdUrl string) (*clientv3.Client, string, error) {
	params, err := parseEtcdUrl(etcdUrl)
	if err != nil {
		log.Error(err)
		return nil, "", err
	}
	endpoints, path := params.hosts, params.path
	
	//grpc dialing occurs when constructing clientv3.Config.
	//Note that DialTimeout only applis to the first time connecting.
	//2016/08/31 11:42:32 Failed to dial 127.0.0.1:2379: context canceled; please retry.
	//grpc: timed out when dialing
	cfg := &clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 60 * time.Second,
	}
	client, err := clientv3.New(*cfg)
	if err != nil {
		log.Error(err)
		return nil, "", err
	}
	return client, path, nil
}

func Elect(client *clientv3.Client, path string, proposal string, leaderCh chan string) error {
	//https://blog.golang.org/context, Go Concurrency Patterns: Context
	//https://golang.org/pkg/context/
	ctx, _ := context.WithCancel(context.TODO())

	//Note: puting election and jobs at the same path level doesn't work!
	election := fmt.Sprintf("%s/election", path)
	go observe(ctx, client, election, leaderCh)
	//go campaign(client, election, prop)
	go func() {
	FOR_CAMP:
		for {
			campaign(ctx, client, election, proposal)
			select {
			case <-ctx.Done():
				break FOR_CAMP
			default: //default case makes select non-blocking
			}
		}
	}()

	select{} //block forever
	return nil
}
