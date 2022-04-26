package client

import (
	"context"
	"time"

	"github.com/gogo/status"
	"github.com/google/uuid"
	"github.com/pingcap/errors"
	"github.com/pingcap/tiflow/dm/pkg/log"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"

	"github.com/hanfei1991/microcosm/pb"
	derrors "github.com/hanfei1991/microcosm/pkg/errors"
)

const preDispatchTaskRetryInterval = 1 * time.Second

// TaskDispatcher implements the logic to invoke two-phase task-dispatching.
// A separate struct is used to decouple the complexity of the two-phase
// protocol from the implementation of ExecutorClient.
// TODO think about whether we should refactor the ExecutorClient's interface.
type TaskDispatcher struct {
	client ExecutorClient

	timeout       time.Duration
	retryInterval time.Duration
}

// NewTaskDispatcher returns a new TaskDispatcher.
// timeout limits the total duration of a call to DispatchTask.
func NewTaskDispatcher(client ExecutorClient, timeout time.Duration) *TaskDispatcher {
	return &TaskDispatcher{
		client:        client,
		timeout:       timeout,
		retryInterval: preDispatchTaskRetryInterval,
	}
}

// DispatchTaskArgs contains the required parameters for creating a worker.
type DispatchTaskArgs struct {
	WorkerID     string
	MasterID     string
	WorkerType   int64
	WorkerConfig []byte
}

// DispatchTask performs the two-phase task dispatching by
// doing the relevant gRPC calls.
//
// - startWorkerTimer() is called before a worker is
//   launched on the executor.
// - abortWorker() is called if startWorkerTimer() has been called
//   but the server has aborted the request.
//
// For a sketch of the logic, please refer to lib/doc/create_worker.puml.
// TODO The UML diagrams are not finalized.
// TODO Find a way to automatically render the puml files on GitHub.
func (d *TaskDispatcher) DispatchTask(
	ctx context.Context,
	args *DispatchTaskArgs,
	startWorkerTimer func(),
	abortWorker func(),
) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	requestID, err := d.preDispatchTaskWithRetry(ctx, args)
	if err != nil {
		return derrors.ErrExecutorPreDispatchFailed.Wrap(err)
	}

	// The timer must be started before invoking ConfirmDispatchTask.
	startWorkerTimer()

	guaranteedFailure, err := d.confirmDispatchTask(ctx, requestID, args.WorkerID)
	if err != nil {
		if guaranteedFailure {
			// abortWorker only if the failure is guaranteed, i.e.,
			// caused by an error generated by the server, rather than
			// a gRPC internal error or network error.
			abortWorker()
			return derrors.ErrExecutorConfirmDispatchFailed.Wrap(err)
		}
		log.L().Warn("ConfirmDispatchTask encountered error, "+
			"but the server's state is undetermined",
			zap.Error(err))
		// We treat the undetermined state as success.
		// The caller should handle the situation on its own.
		return nil
	}

	return nil
}

func (d *TaskDispatcher) preDispatchTaskWithRetry(
	ctx context.Context, args *DispatchTaskArgs,
) (requestID string, retErr error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		requestID, shouldRetry, err := d.preDispatchTaskOnce(ctx, args)
		if err == nil {
			// Success
			return requestID, nil
		}
		if !shouldRetry {
			return "", err
		}

		if deadline, ok := ctx.Deadline(); ok {
			if time.Now().Add(d.retryInterval).After(deadline) {
				retErr := errors.Annotate(context.DeadlineExceeded,
					"would exceed deadline if waiting")
				return "", retErr
			}
		}

		// We are not using a rate limiter to simplify error handling.
		// For example, x/time/rate would retry unnamed errors like this:
		// https://cs.opensource.google/go/x/time/+/583f2d63:rate/rate.go;l=249;drc=583f2d630306214ee49ea373317b53b90026aab7
		timer := time.NewTimer(d.retryInterval)
		select {
		case <-ctx.Done():
			return "", derrors.ErrExecutorPreDispatchFailed.Wrap(ctx.Err())
		case <-timer.C:
		}
	}
}

func (d *TaskDispatcher) preDispatchTaskOnce(
	ctx context.Context, args *DispatchTaskArgs,
) (requestID string, shouldRetry bool, retErr error) {
	// requestID is regenerated each time for tracing purpose.
	requestID = uuid.New().String()

	// The response is irrelevant because it is empty.
	_, err := d.client.Send(ctx, &ExecutorRequest{
		Cmd: CmdPreDispatchTask,
		Req: &pb.PreDispatchTaskRequest{
			TaskTypeId: args.WorkerType,
			TaskConfig: args.WorkerConfig,
			MasterId:   args.MasterID,
			WorkerId:   args.WorkerID,
			RequestId:  requestID,
		},
	})
	if err != nil {
		st, ok := status.FromError(err)
		if !ok {
			// err is not an error that came from gRPC.
			// We are not responsible for handling it in this method,
			// so for safety, we are NOT retrying it.
			return "", false, errors.Trace(err)
		}

		switch st.Code() {
		// NOTE: Aborted and AlreadyExists are guaranteed NOT to be
		// generated by the gRPC framework.
		// Refer to https://pkg.go.dev/google.golang.org/grpc/codes
		case codes.Aborted:
			// The business logic should be notified.
			return "", false, errors.Trace(err)
		case codes.AlreadyExists:
			// Since we are generating unique UUIDs, this should not happen.
			log.L().Panic("Unexpected error", zap.Error(err))
			panic("unreachable")
		default:
			log.L().Warn("PreDispatchTask encountered error, retrying", zap.Error(err))
			return "", true, errors.Trace(err)
		}
	}
	return requestID, false, nil
}

func (d *TaskDispatcher) confirmDispatchTask(
	ctx context.Context,
	requestID string,
	workerID string,
) (guaranteedFailure bool, retErr error) {
	// The response is irrelevant because it is empty.
	_, err := d.client.Send(ctx, &ExecutorRequest{
		Cmd: CmdConfirmDispatchTask,
		Req: &pb.ConfirmDispatchTaskRequest{
			WorkerId:  workerID,
			RequestId: requestID,
		},
	})
	if err != nil {
		// The current implementation of the Executor does not support idempotency,
		// so we are not retrying.
		st := status.Convert(err)
		switch st.Code() {
		case codes.Aborted, codes.NotFound:
			// These cases indicate an error generated by the executor,
			// rather than by the gRPC library or the network layer.
			//
			// NOTE: Aborted and NotFound are guaranteed NOT to be
			// generated by the gRPC framework.
			// Refer to https://pkg.go.dev/google.golang.org/grpc/codes
			return true, errors.Trace(err)
		default:
			return false, errors.Trace(err)
		}
	}
	return false, nil
}
