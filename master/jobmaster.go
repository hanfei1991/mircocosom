package master

import (
	resource "github.com/hanfei1991/microcosom/master/resource_manager"
	"github.com/hanfei1991/microcosom/model"
)

type JobMaster interface {
	DispatchJob() error
	ID() model.JobID 
}