package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/openebs/jiva/controller/rest"
	"github.com/openebs/jiva/types"
)

type ControllerClient struct {
	controller string
}

func NewControllerClient(controller string) *ControllerClient {
	if !strings.HasSuffix(controller, "/v1") {
		controller += "/v1"
	}
	return &ControllerClient{
		controller: controller,
	}
}

func (c *ControllerClient) Start(replicas ...string) error {
	volume, err := c.GetVolume()
	if err != nil {
		return err
	}

	return c.post(volume.Actions["start"], rest.StartInput{
		Replicas: replicas,
	}, nil)
}

func (c *ControllerClient) RevertVolume(name string) (*rest.Volume, error) {
	volume, err := c.GetVolume()
	if err != nil {
		return nil, err
	}

	input := &rest.RevertInput{
		Name: name,
	}

	output := &rest.Volume{}

	err = c.post(volume.Actions["revert"], input, output)
	if err != nil {
		return nil, err
	}

	return output, err
}

func (c *ControllerClient) Snapshot(name string) (string, error) {
	volume, err := c.GetVolume()
	if err != nil {
		return "", err
	}

	input := &rest.SnapshotInput{
		Name: name,
	}
	output := &rest.SnapshotOutput{}
	err = c.post(volume.Actions["snapshot"], input, output)
	if err != nil {
		return "", err
	}

	return output.Id, err
}

func (c *ControllerClient) RevertSnapshot(snapshot string) error {
	volume, err := c.GetVolume()
	if err != nil {
		return err
	}

	return c.post(volume.Actions["revert"], rest.RevertInput{
		Name: snapshot,
	}, nil)
}

func (c *ControllerClient) ListJournal(limit int) error {
	err := c.post("/journal", &rest.JournalInput{Limit: limit}, nil)
	return err
}

func (c *ControllerClient) ListReplicas() ([]rest.Replica, error) {
	var resp rest.ReplicaCollection
	err := c.get("/replicas", &resp)
	return resp.Data, err
}

func (c *ControllerClient) CreateReplica(address string) (*rest.Replica, error) {
	var resp rest.Replica
	err := c.post("/replicas", &rest.Replica{
		Address: address,
	}, &resp)
	return &resp, err
}

func (c *ControllerClient) CreateQuorumReplica(address string) (*rest.Replica, error) {
	var resp rest.Replica
	err := c.post("/quorumreplicas", &rest.Replica{
		Address: address,
	}, &resp)
	return &resp, err
}

func (c *ControllerClient) DeleteReplica(address string) (*rest.Replica, error) {
	reps, err := c.ListReplicas()
	if err != nil {
		return nil, err
	}

	for _, rep := range reps {
		if rep.Address == address {
			httpReq, err := http.NewRequest("DELETE", rep.Links["self"], nil)
			if err != nil {
				return nil, err
			}
			httpResp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				return nil, err
			}
			if httpResp.StatusCode >= 300 {
				content, _ := ioutil.ReadAll(httpResp.Body)
				return nil, fmt.Errorf("Bad response: %d %s: %s", httpResp.StatusCode, httpResp.Status, content)
			}
			return &rep, nil
		}
	}

	return nil, nil
}

func (c *ControllerClient) UpdateReplica(replica rest.Replica) (rest.Replica, error) {
	var resp rest.Replica
	err := c.put(replica.Links["self"], &replica, &resp)
	return resp, err
}

func (c *ControllerClient) GetReplica(address string) (*rest.Replica, error) {
	resp := &rest.Replica{}
	err := c.get("/replicas/"+address, &resp)
	return resp, err
}

func (c *ControllerClient) VerifyRebuildReplica(address string) error {
	replica, err := c.GetReplica(address)
	if err != nil {
		logrus.Errorf("getReplica in verifyRebuildReplica failed %s", address)
		return err
	}
	return c.post(replica.Actions["verifyrebuild"], &replica, nil)
}

func (c *ControllerClient) PrepareRebuild(address string) (*rest.PrepareRebuildOutput, error) {
	var output rest.PrepareRebuildOutput
	replica, err := c.GetReplica(address)
	if err != nil {
		logrus.Errorf("getReplica in prepareRebuild failed %s", address)
		return nil, err
	}
	err = c.post(replica.Actions["preparerebuild"], &replica, &output)
	return &output, err
}

func (c *ControllerClient) GetVolume() (*rest.Volume, error) {
	var volumes rest.VolumeCollection

	err := c.get("/volumes", &volumes)
	if err != nil {
		logrus.Errorf("GetVolume failed, %v", err)
		return nil, err
	}

	if len(volumes.Data) == 0 {
		logrus.Errorf("volume not found")
		return nil, errors.New("No volume found")
	}

	return &volumes.Data[0], nil
}

func (c *ControllerClient) Register(address string, revisionCount int64, peerDetails types.PeerDetails, replicaType string, upTime time.Duration, state string) error {
	err := c.post("/register", &rest.RegReplica{
		Address:     address,
		RevCount:    strconv.FormatInt(revisionCount, 10),
		PeerDetails: peerDetails,
		RepType:     replicaType,
		UpTime:      upTime,
		RepState:    state,
	}, nil)
	return err
}

func (c *ControllerClient) post(path string, req, resp interface{}) error {
	return c.do("POST", path, req, resp)
}

func (c *ControllerClient) put(path string, req, resp interface{}) error {
	return c.do("PUT", path, req, resp)
}

func (c *ControllerClient) do(method, path string, req, resp interface{}) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}

	bodyType := "application/json"
	url := path
	if !strings.HasPrefix(url, "http") {
		url = c.controller + path
	}

	logrus.Debugf("%s %s", method, url)
	httpReq, err := http.NewRequest(method, url, bytes.NewBuffer(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", bodyType)

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 300 {
		content, _ := ioutil.ReadAll(httpResp.Body)
		return fmt.Errorf("Bad response: %d %s: %s", httpResp.StatusCode, httpResp.Status, content)
	}

	if resp == nil {
		return nil
	}

	return json.NewDecoder(httpResp.Body).Decode(resp)
}

func (c *ControllerClient) get(path string, obj interface{}) error {
	resp, err := http.Get(c.controller + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(obj)
}
