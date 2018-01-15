// Copyright 2018 Granitic. All rights reserved.
// Use of this source code is governed by an Apache 2.0 license that can be found in the LICENSE file at the root of this project.

package schedule

import (
	"fmt"
	"github.com/graniticio/granitic/ioc"
	"github.com/graniticio/granitic/logging"
	"github.com/pkg/errors"
)

type TaskScheduler struct {
	componentContainer *ioc.ComponentContainer
	managedTasks       []*invocationManager
	State              ioc.ComponentState
	// Logger used by Granitic framework components. Automatically injected.
	FrameworkLogger logging.Logger
}

// Implements ioc.ContainerAccessor
func (ts *TaskScheduler) Container(container *ioc.ComponentContainer) {
	ts.componentContainer = container
}

// StartComponent Finds any schedules, parses them and verifies the component they reference implements schedule.TaskLogic
func (ts *TaskScheduler) StartComponent() error {

	ts.managedTasks = make([]*invocationManager, 0)

	if ts.State != ioc.StoppedState {
		return nil
	}

	ts.FrameworkLogger.LogInfof("Starting")
	ts.FrameworkLogger.LogDebugf("Searching for schedule.Task components")

	for _, component := range ts.componentContainer.AllComponents() {

		ts.FrameworkLogger.LogTracef("Considering %s", component.Name)

		if task, found := component.Instance.(*Task); found {
			if task.Id == "" {
				//Use the name of the component containing the task as an ID for the task if it isn't explicitly set
				task.Id = component.Name
			}

			ts.FrameworkLogger.LogDebugf("Found Task %s", task.FullName())

			if err := ts.validateAndPrepare(ts.componentContainer, task); err != nil {
				return errors.New(fmt.Sprintf("%s: %s", component.Name, err.Error()))
			}

		}
	}

	return nil
}

func (ts *TaskScheduler) AllowAccess() error {

	for _, tm := range ts.managedTasks {

		go tm.Start()

	}

	ts.FrameworkLogger.LogDebugf("%d task manager(s) started", len(ts.managedTasks))

	return nil
}

func (ts *TaskScheduler) PrepareToStop() {

	for _, tm := range ts.managedTasks {

		tm.PrepareToStop()

	}

}

func (ts *TaskScheduler) validateAndPrepare(cn *ioc.ComponentContainer, task *Task) error {

	if err := ts.findLogic(cn, task); err != nil {
		return err
	}

	if err := ts.setOverlapBehaviour(task); err != nil {
		return err
	}

	if task.Every == "" {
		m := fmt.Sprintf("You must set the 'Every' field to set an execution interval")
		return errors.New(m)
	}

	tm := NewInvocationManager(task)
	ts.managedTasks = append(ts.managedTasks, tm)
	tm.Log = ts.FrameworkLogger

	if interval, err := parseEvery(task.Every); err == nil {
		tm.Interval = interval
	} else {

		return err
	}

	return nil
}

func (ts *TaskScheduler) findLogic(cn *ioc.ComponentContainer, task *Task) error {
	if task.Component == "" {
		return errors.New("Missing Component (you must provide the name of the component that will execute your task")
	}

	tc := cn.ComponentByName(task.Component)

	if tc == nil {
		m := fmt.Sprintf("Component %s does not exist (no component with that name)", task.Component)
		return errors.New(m)
	}

	tl, okay := tc.Instance.(TaskLogic)

	if !okay {
		m := fmt.Sprintf("Component %s does not implement schedule.TaskLogic", task.Component)
		return errors.New(m)
	}

	task.logic = tl

	return nil
}

func (ts *TaskScheduler) setOverlapBehaviour(task *Task) error {
	switch task.Overlap {
	case "":
	case "SKIP":
		task.overlap = SKIP
	case "ALLOW":
		task.overlap = ALLOW
	default:
		m := fmt.Sprintf("Unsupported OverlapBehaviour %s - should be SKIP or ALLOW", task.Overlap)
		return errors.New(m)
	}

	return nil
}
