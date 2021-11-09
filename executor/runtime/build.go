package runtime

import (
	"encoding/json"

	"github.com/hanfei1991/microcosom/model"
)

func newTaskContainer(task *model.Task, ctx *taskContext) *taskContainer {
	t := &taskContainer{
		cfg: task,
		id : task.ID,
		ctx: ctx,
	}
	return t
}

func newHashOp(cfg *model.HashOp) operator {
	return &opHash{}
}

func newReadTableOp(cfg *model.TableReaderOp) operator {
	return &opReceive{
		addr: cfg.Addr,
		data: make(chan *Record, 1024),
	}
}

func newSinkOp(cfg *model.TableSinkOp) operator {
	return &opSink{
		writer: fileWriter{
			filePath : cfg.File,
			tid : cfg.TableID,
		},
	}
}
func (s *Scheduler) connectTasks(sender, receiver *taskContainer) {
	ch := &Channel{
		innerChan: make(chan *Record, 1024),
		sendWaker: s.getWaker(sender),
		recvWaker: s.getWaker(receiver),
	}
	sender.output = append(sender.output, ch)
	receiver.inputs = append(receiver.inputs, ch)
}


func (s *Scheduler) SubmitTasks(tasks []*model.Task) error {
	taskSet := make(map[model.TaskID]*taskContainer)
	for _, t := range tasks {
		task := newTaskContainer(t, s.ctx)
		switch t.OpTp {
		case model.TableReaderType:
			op := &model.TableReaderOp {}
			json.Unmarshal(t.Op, op)
			task.op = newReadTableOp(op)
			task.setRunnable()
		case model.HashType:
			op := &model.HashOp {}
			json.Unmarshal(t.Op, op)
			task.op = newHashOp(op)
			task.tryBlock()
		case model.TableSinkType:
			op := &model.TableSinkOp{}
			json.Unmarshal(t.Op, op)
			task.op = newSinkOp(op)
			task.tryBlock()
		}
		taskSet[task.id] = task
	}

	for _, t := range taskSet {
		for _, tid := range t.cfg.Outputs {
			dst, ok := taskSet[tid]
			if !ok {
				s.connectTasks(t, dst)
			}
		}
		err := t.prepare()
		if err != nil {
			return err
		}
	}
	
	// add to queue, begin to run.
	for _, t := range taskSet {
		if t.status == int32(Runnable) {
			s.q.push(t)
		}
	}
	return nil
}
