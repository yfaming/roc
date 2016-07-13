// Copyright 2014 The roc Author. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.


package rocserv

import (
	"fmt"
	"time"
	"encoding/json"
	"strconv"
	"sort"
	"crypto/sha1"
	"crypto/md5"

	// now use 73a8ef737e8ea002281a28b4cb92a1de121ad4c6
    //"github.com/coreos/go-etcd/etcd"

    etcd "github.com/coreos/etcd/client"

	"github.com/sdming/gosnow"

	"github.com/shawnfeng/sutil"
	"github.com/shawnfeng/sutil/slog"

	"github.com/shawnfeng/dbrouter"

	"golang.org/x/net/context"
)


type ServInfo struct {
	Type string         `json:"type"`
	Addr string         `json:"addr"`
	//Processor string    `json:"processor"`
}

func (m *ServInfo) String() string {
	return fmt.Sprintf("type:%s addr:%s", m.Type, m.Addr)
}

type RegData struct {
	Servs map[string]*ServInfo   `json:"servs"`
}

// ServBase Interface
type ServBase interface {
	// key is processor to ServInfo
	RegisterService(servs map[string]*ServInfo) error
	Servname() string
	Servid() int
	// 服务副本名称, servename + servid
	Copyname() string

	// 获取服务的配置
	ServConfig(cfg interface{}) error
	// 任意路径的配置信息
	//ArbiConfig(location string) (string, error)

	// id生成逻辑
	GenSnowFlakeId() (int64, error)
	// 获取snowflakeid生成时间戳，单位ms
	GetSnowFlakeIdStamp(sid int64) int64
	// 按给定的时间点构造一个起始snowflakeid，一般用于区域判断
	GetSnowFlakeIdWithStamp(stamp int64) int64

	GenUuid() string
	GenUuidSha1() string
	GenUuidMd5() string

	// db router
	Dbrouter() *dbrouter.Router
}

//====================
// id生成逻辑
type IdGenerator struct {
	snow *gosnow.SnowFlake
}

func (m *IdGenerator) GenSnowFlakeId() (int64, error) {
	id, err := m.snow.Next()
	return int64(id), err
}

func (m *IdGenerator) GetSnowFlakeIdStamp(sid int64) int64 {
	return gosnow.Since+sid>>22
}

func (m *IdGenerator) GetSnowFlakeIdWithStamp(stamp int64) int64 {
	return (stamp - gosnow.Since) << 22
}



func (m *IdGenerator) GenUuid() string {
	return sutil.GetUUID()
}


func (m *IdGenerator) GenUuidSha1() string {
	h := sha1.Sum([]byte(m.GenUuid()))
	return fmt.Sprintf("%x", h)
}


func (m *IdGenerator) GenUuidMd5() string {
	h := md5.Sum([]byte(m.GenUuid()))
	return fmt.Sprintf("%x", h)
}




//====================================
func getValue(client etcd.KeysAPI, path string) ([]byte, error) {
    r, err := client.Get(context.Background(), path, &etcd.GetOptions{Recursive: true, Sort: false})
	if err != nil {
		return nil, err
	}

	if r.Node == nil || r.Node.Dir {
		return nil, fmt.Errorf("etcd node value err location:%s", path)
	}

	return []byte(r.Node.Value), nil


}

func genSid(client etcd.KeysAPI, path, skey string) (int, error) {
	fun := "genSid -->"
    r, err := client.Get(context.Background(), path, &etcd.GetOptions{Recursive: true, Sort: false})
	if err != nil {
		return -1, err
	}

	js, _ := json.Marshal(r)

	slog.Infof("%s", js)

	if r.Node == nil || !r.Node.Dir {
		return -1, fmt.Errorf("node error location:%s", path)
	}

	slog.Infof("%s serv:%s len:%d", fun, r.Node.Key, r.Node.Nodes.Len())

	// 获取已有的servid，按从小到大排列
	ids := make([]int, 0)
	for _, n := range r.Node.Nodes {
		sid := n.Key[len(r.Node.Key)+1:]
		id, err := strconv.Atoi(sid)
		if err != nil || id < 0 {
			slog.Errorf("%s sid error key:%s", fun, n.Key)
		} else {
			ids = append(ids, id)
			if n.Value == skey {
				// 如果已经存在的sid使用的skey和设置一致，则使用之前的sid
				return id, nil
			}
		}
	}

	sort.Ints(ids)
	sid := 0
	for _, id := range ids {
		// 取不重复的最小的id
		if sid == id {
			sid++
		} else {
			break
		}
	}

	nserv := fmt.Sprintf("%s/%d", r.Node.Key, sid)
	r, err = client.Create(context.Background(), nserv, skey)
	if err != nil {
		return -1, err
	}

	jr, _ := json.Marshal(r)
	slog.Infof("%s newserv:%s rep:%s", fun, nserv, jr)

	return sid, nil

}

func retryGenSid(client etcd.KeysAPI, path, skey string, try int) (int, error) {
	fun := "retryGenSid -->"
	for i := 0; i < try; i++ {
		// 重试3次
		sid, err := genSid(client, path, skey)
		if err != nil {
			slog.Errorf("%s gensid try:%d path:%s err:%s", fun, i, path, err)
		} else {
			return sid, nil
		}
	}

	return -1, fmt.Errorf("gensid error try:%d", try)
}

func initSnowflake(servid int) (*gosnow.SnowFlake, error) {
	if servid < 0 {
		return nil, fmt.Errorf("init snowflake use nagtive servid")
	}
	gosnow.Since = time.Date(2014, 11, 1, 0, 0, 0, 0, time.UTC).UnixNano() / 1000000
	v, err := gosnow.NewSnowFlake(uint32(servid))
	if err != nil {
		return nil, err
	}


	return v, nil
}


