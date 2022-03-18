package example

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/hanfei1991/microcosm/lib"
	"github.com/pingcap/tiflow/dm/pkg/log"
)

func newExampleWorker() *exampleWorker {
	self := &exampleWorker{}
	self.BaseWorker = lib.MockBaseWorker(workerID, masterID, self)
	return self
}

func TestExampleWorker(t *testing.T) {
	t.Parallel()

	initLogger.Do(func() {
		_ = log.InitLogger(&log.Config{
			Level: "debug",
		})
	})

	worker := newExampleWorker()
	ctx := context.Background()
	err := worker.Init(ctx)
	require.NoError(t, err)

	// tick twice
	err = worker.Tick(ctx)
	require.NoError(t, err)
	err = worker.Tick(ctx)
	require.NoError(t, err)

	broker := worker.BaseWorker.(*lib.BaseWorkerForTesting).Broker
	broker.AssertPersisted(t, "/local/example")
	broker.AssertFileExists(t, "worker", "local/example", "1.txt")
	broker.AssertFileExists(t, "worker", "local/example", "2.txt")

	defer func() {
		err = os.RemoveAll("./unit_test_resources/worker")
		require.NoError(t, err)
	}()

	time.Sleep(time.Second)
	require.Eventually(t, func() bool {
		return worker.Status().Code == lib.WorkerStatusFinished
	}, time.Second, time.Millisecond*100)

	resp, err := worker.BaseWorker.MetaKVClient().Get(ctx, tickKey)
	require.NoError(t, err)
	etcdResp := resp.(*clientv3.GetResponse)
	require.Len(t, etcdResp.Kvs, 1)
	require.Equal(t, "2", string(etcdResp.Kvs[0].Value))

	lib.MockBaseWorkerCheckSendMessage(t, worker.BaseWorker.(*lib.BaseWorkerForTesting).DefaultBaseWorker, testTopic, testMsg)
	err = worker.Close(ctx)
	require.NoError(t, err)
}
