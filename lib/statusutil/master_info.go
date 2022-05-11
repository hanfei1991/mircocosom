package statusutil

import (
	"context"
	"sync"

	"go.uber.org/atomic"

	libModel "github.com/hanfei1991/microcosm/lib/model"
	"github.com/hanfei1991/microcosm/pkg/p2p"
)

// MasterInfoProvider is an object that can provide necessary
// information so that the Writer can contact the master.
type MasterInfoProvider interface {
	MasterID() libModel.MasterID
	MasterNode() p2p.NodeID
	Epoch() libModel.Epoch
	RefreshMasterInfo(ctx context.Context) error
}

type MockMasterInfoProvider struct {
	mu         sync.RWMutex
	masterID   libModel.MasterID
	masterNode p2p.NodeID
	epoch      libModel.Epoch

	refreshCount atomic.Int64
}

func (p *MockMasterInfoProvider) MasterID() libModel.MasterID {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.masterID
}

func (p *MockMasterInfoProvider) MasterNode() p2p.NodeID {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.masterNode
}

func (p *MockMasterInfoProvider) Epoch() libModel.Epoch {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.epoch
}

func (p *MockMasterInfoProvider) RefreshMasterInfo(ctx context.Context) error {
	p.refreshCount.Add(1)
	return nil
}

func (p *MockMasterInfoProvider) Set(masterID libModel.MasterID, masterNode p2p.NodeID, epoch libModel.Epoch) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.masterID = masterID
	p.masterNode = masterNode
	p.epoch = epoch
}

func (p *MockMasterInfoProvider) RefreshCount() int {
	return int(p.refreshCount.Load())
}
