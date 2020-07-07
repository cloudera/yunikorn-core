/*
 Licensed to the Apache Software Foundation (ASF) under one
 or more contributor license agreements.  See the NOTICE file
 distributed with this work for additional information
 regarding copyright ownership.  The ASF licenses this file
 to you under the Apache License, Version 2.0 (the
 "License"); you may not use this file except in compliance
 with the License.  You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package scheduler

import (
	"strconv"
	"testing"

	"gotest.tools/assert"

	"github.com/apache/incubator-yunikorn-core/pkg/cache"
	"github.com/apache/incubator-yunikorn-core/pkg/common/configs"
	"github.com/apache/incubator-yunikorn-core/pkg/common/resources"
	"github.com/apache/incubator-yunikorn-core/pkg/common/security"
)

// create the root queue, base for all testing
func createRootQueue(maxRes map[string]string) (*SchedulingQueue, error) {
	rootConf := configs.QueueConfig{
		Name:       "root",
		Parent:     true,
		Queues:     nil,
		Properties: make(map[string]string),
	}
	if maxRes != nil {
		rootConf.Resources = configs.Resources{
			Max: maxRes,
		}
	}
	root, err := cache.NewManagedQueue(rootConf, nil)
	// something failed in the cache return early
	if err != nil {
		return nil, err
	}
	return newSchedulingQueueInfo(root, nil), err
}

// wrapper around the create calls using the same syntax as an unmanaged queue
func createManagedQueue(parentQI *SchedulingQueue, name string, parent bool, maxRes map[string]string) (*SchedulingQueue, error) {
	childConf := configs.QueueConfig{
		Name:       name,
		Parent:     parent,
		Queues:     nil,
		Properties: make(map[string]string),
	}
	if maxRes != nil {
		childConf.Resources = configs.Resources{
			Max: maxRes,
		}
	}
	child, err := cache.NewManagedQueue(childConf, parentQI.QueueInfo)
	// something failed in the cache return early
	if err != nil {
		return nil, err
	}
	return newSchedulingQueueInfo(child, parentQI), err
}

// wrapper around the create calls using the same syntax as a managed queue
func createUnManagedQueue(parentQI *SchedulingQueue, name string, parent bool) (*SchedulingQueue, error) {
	child, err := cache.NewUnmanagedQueue(name, !parent, parentQI.QueueInfo)
	// something failed in the cache return early
	if err != nil {
		return nil, err
	}
	return newSchedulingQueueInfo(child, parentQI), err
}

// base test for creating a managed queue
func TestQueueBasics(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	// check the state of the queue
	if !root.isManaged() && !root.isLeafQueue() && !root.isRunning() {
		t.Error("root queue status is incorrect")
	}
	// allocations should be nil
	if !resources.IsZero(root.preempting) && !resources.IsZero(root.pending) {
		t.Error("root queue must not have allocations set on create")
	}
}

func TestManagedSubQueues(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")

	// single parent under root
	var parent *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	if parent.isLeafQueue() || !parent.isManaged() || !parent.isRunning() {
		t.Error("parent queue is not marked as running managed parent")
	}
	if len(root.childrenQueues) == 0 {
		t.Error("parent queue is not added to the root queue")
	}
	if parent.isRoot() {
		t.Error("parent queue says it is the root queue which is incorrect")
	}
	if parent.removeQueue() || len(root.childrenQueues) != 1 {
		t.Error("parent queue should not have been removed as it is running")
	}

	// add a leaf under the parent
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(parent, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	if len(parent.childrenQueues) == 0 {
		t.Error("leaf queue is not added to the parent queue")
	}
	if !leaf.isLeafQueue() || !leaf.isManaged() {
		t.Error("leaf queue is not marked as managed leaf")
	}

	// cannot remove child with app in it
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "test"})
	leaf.addSchedulingApplication(app)

	// both parent and leaf are marked for removal
	parent.QueueInfo.MarkQueueForRemoval()
	if !leaf.isDraining() || !parent.isDraining() {
		t.Error("queues are not marked for removal (not in draining state)")
	}
	// try to remove the parent
	if parent.removeQueue() {
		t.Error("parent queue should not have been removed as it has a child")
	}
	// try to remove the child
	if leaf.removeQueue() {
		t.Error("leaf queue should not have been removed")
	}
	// remove the app (dirty way)
	delete(leaf.applications, "test")
	if !leaf.removeQueue() && len(parent.childrenQueues) != 0 {
		t.Error("leaf queue should have been removed and parent updated and was not")
	}
}

func TestUnManagedSubQueues(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")

	// single parent under root
	var parent *SchedulingQueue
	parent, err = createUnManagedQueue(root, "parent", true)
	assert.NilError(t, err, "failed to create parent queue")
	if parent.isLeafQueue() || parent.isManaged() {
		t.Errorf("parent queue is not marked as parent")
	}
	if len(root.childrenQueues) == 0 {
		t.Errorf("parent queue is not added to the root queue")
	}
	// add a leaf under the parent
	var leaf *SchedulingQueue
	leaf, err = createUnManagedQueue(parent, "leaf", false)
	assert.NilError(t, err, "failed to create leaf queue")
	if len(parent.childrenQueues) == 0 {
		t.Error("leaf queue is not added to the parent queue")
	}
	if !leaf.isLeafQueue() || leaf.isManaged() {
		t.Error("leaf queue is not marked as managed leaf")
	}

	// cannot remove child with app in it
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "test"})
	leaf.addSchedulingApplication(app)

	// try to mark parent and leaf for removal
	parent.QueueInfo.MarkQueueForRemoval()
	if leaf.isDraining() || parent.isDraining() {
		t.Error("queues are marked for removal (draining state not for unmanaged queues)")
	}
	// try to remove the parent
	if parent.removeQueue() {
		t.Error("parent queue should not have been removed as it has a child")
	}
	// try to remove the child
	if leaf.removeQueue() {
		t.Error("leaf queue should not have been removed")
	}
	// remove the app (dirty way)
	delete(leaf.applications, "test")
	if !leaf.removeQueue() && len(parent.childrenQueues) != 0 {
		t.Error("leaf queue should have been removed and parent updated and was not")
	}
}

func TestPendingCalc(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")

	res := map[string]string{"memory": "100", "vcores": "10"}
	var allocation *resources.Resource
	allocation, err = resources.NewResourceFromConf(res)
	assert.NilError(t, err, "failed to create basic resource")
	leaf.incPendingResource(allocation)
	if !resources.Equals(root.pending, allocation) {
		t.Errorf("root queue pending allocation failed to increment expected %v, got %v", allocation, root.pending)
	}
	if !resources.Equals(leaf.pending, allocation) {
		t.Errorf("leaf queue pending allocation failed to increment expected %v, got %v", allocation, leaf.pending)
	}
	leaf.decPendingResource(allocation)
	if !resources.IsZero(root.pending) {
		t.Errorf("root queue pending allocation failed to decrement expected 0, got %v", root.pending)
	}
	if !resources.IsZero(leaf.pending) {
		t.Errorf("leaf queue pending allocation failed to decrement expected 0, got %v", leaf.pending)
	}
	// Not allowed to go negative: both will be zero after this
	newRes := resources.Multiply(allocation, 2)
	root.pending = newRes
	leaf.decPendingResource(newRes)
	// using the get function to access the value
	if !resources.IsZero(root.GetPendingResource()) {
		t.Errorf("root queue pending allocation failed to decrement expected zero, got %v", root.pending)
	}
	if !resources.IsZero(leaf.GetPendingResource()) {
		t.Errorf("leaf queue pending allocation should have failed to decrement expected zero, got %v", leaf.pending)
	}
}

func TestGetChildQueueInfos(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var parent *SchedulingQueue
	parent, err = createManagedQueue(root, "parent-man", true, nil)
	assert.NilError(t, err, "failed to create managed parent queue")
	for i := 0; i < 10; i++ {
		_, err = createManagedQueue(parent, "leaf-man"+strconv.Itoa(i), false, nil)
		if err != nil {
			t.Errorf("failed to create managed queue: %v", err)
		}
	}
	if len(parent.childrenQueues) != 10 {
		t.Errorf("managed leaf queues are not added to the parent queue, expected 10 children got %d", len(parent.childrenQueues))
	}

	parent, err = createUnManagedQueue(root, "parent-un", true)
	assert.NilError(t, err, "failed to create unamanged parent queue")
	for i := 0; i < 10; i++ {
		_, err = createUnManagedQueue(parent, "leaf-un-"+strconv.Itoa(i), false)
		if err != nil {
			t.Errorf("failed to create unmanaged queue: %v", err)
		}
	}
	if len(parent.childrenQueues) != 10 {
		t.Errorf("unmanaged leaf queues are not added to the parent queue, expected 10 children got %d", len(parent.childrenQueues))
	}

	// check the root queue
	if len(root.childrenQueues) != 2 {
		t.Errorf("parent queues are not added to the root queue, expected 2 children got %d", len(root.childrenQueues))
	}
}

func TestAddApplication(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf-man", false, nil)
	assert.NilError(t, err, "failed to create managed leaf queue")
	pending := resources.NewResourceFromMap(
		map[string]resources.Quantity{
			resources.MEMORY: 10,
		})
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "test"})
	app.pending = pending
	// adding the app must not update pending resources
	leaf.addSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 1, "Application was not added to the queue as expected")
	assert.Assert(t, resources.IsZero(leaf.pending), "leaf queue pending resource not zero")

	// add the same app again should not increase the number of apps
	leaf.addSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 1, "Application was not replaced in the queue as expected")
}

func TestAddApplicationWithTag(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	// only need to test leaf queues as we will never add an app to a parent
	var leaf, leafUn *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf-man", false, nil)
	assert.NilError(t, err, "failed to create managed leaf queue")
	leafUn, err = createUnManagedQueue(root, "leaf-unman", false)
	assert.NilError(t, err, "failed to create unmanaged leaf queue")
	appInfo := cache.NewApplicationInfo("app-1", "default", "root.leaf-man", security.UserGroup{}, nil)
	app1 := newSchedulingApplication(appInfo)

	// adding the app to managed/unmanaged queue must not update queue settings, works
	leaf.addSchedulingApplication(app1)
	assert.Equal(t, len(leaf.applications), 1, "Application was not added to the managed queue as expected")
	if leaf.getMaxResource() != nil {
		t.Errorf("Max resources should not be set on managed queue got: %s", leaf.getMaxResource().String())
	}
	appInfo = cache.NewApplicationInfo("app-2", "default", "root.leaf-un", security.UserGroup{}, nil)
	app2 := newSchedulingApplication(appInfo)
	leafUn.addSchedulingApplication(app2)
	assert.Equal(t, len(leaf.applications), 1, "Application was not added to the unmanaged queue as expected")
	if leafUn.getMaxResource() != nil {
		t.Errorf("Max resources should not be set on unmanaged queue got: %s", leafUn.getMaxResource().String())
	}

	maxRes := resources.NewResourceFromMap(
		map[string]resources.Quantity{
			"first": 10,
		})
	tags := make(map[string]string)
	tags[appTagNamespaceResourceQuota] = "{\"resources\":{\"first\":{\"value\":10}}}"
	// add apps again now with the tag set
	appInfo = cache.NewApplicationInfo("app-3", "default", "root.leaf-man", security.UserGroup{}, tags)
	app3 := newSchedulingApplication(appInfo)
	leaf.addSchedulingApplication(app3)
	assert.Equal(t, len(leaf.applications), 2, "Application was not added to the managed queue as expected")
	if leaf.getMaxResource() != nil {
		t.Errorf("Max resources should not be set on managed queue got: %s", leaf.getMaxResource().String())
	}
	appInfo = cache.NewApplicationInfo("app-4", "default", "root.leaf-un", security.UserGroup{}, tags)
	app4 := newSchedulingApplication(appInfo)
	leafUn.addSchedulingApplication(app4)
	assert.Equal(t, len(leaf.applications), 2, "Application was not added to the unmanaged queue as expected")
	if !resources.Equals(leafUn.getMaxResource(), maxRes) {
		t.Errorf("Max resources not set as expected: %s got: %v", maxRes.String(), leafUn.getMaxResource())
	}

	// set to illegal limit (0 value)
	tags[appTagNamespaceResourceQuota] = "{\"resources\":{\"first\":{\"value\":0}}}"
	appInfo = cache.NewApplicationInfo("app-4", "default", "root.leaf-un", security.UserGroup{}, tags)
	app4 = newSchedulingApplication(appInfo)
	leafUn.addSchedulingApplication(app4)
	assert.Equal(t, len(leaf.applications), 2, "Application was not added to the unmanaged queue as expected")
	if !resources.Equals(leafUn.getMaxResource(), maxRes) {
		t.Errorf("Max resources not set as expected: %s got: %v", maxRes.String(), leafUn.getMaxResource())
	}
}

func TestRemoveApplication(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf-man", false, nil)
	assert.NilError(t, err, "failed to create managed leaf queue")
	// try removing a non existing app
	nonExist := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "test"})
	leaf.removeSchedulingApplication(nonExist)
	assert.Equal(t, len(leaf.applications), 0, "Removal of non existing app updated unexpected")

	// add an app and remove it
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "exists"})
	leaf.addSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 1, "Application was not added to the queue as expected")
	assert.Assert(t, resources.IsZero(leaf.pending), "leaf queue pending resource not zero")
	leaf.removeSchedulingApplication(nonExist)
	assert.Equal(t, len(leaf.applications), 1, "Non existing application was removed from the queue")
	leaf.removeSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 0, "Application was not removed from the queue as expected")

	// try the same again now with pending resources set
	pending := resources.NewResourceFromMap(
		map[string]resources.Quantity{
			resources.MEMORY: 10,
		})
	app.pending.AddTo(pending)
	leaf.addSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 1, "Application was not added to the queue as expected")
	assert.Assert(t, resources.IsZero(leaf.pending), "leaf queue pending resource not zero")
	// update pending resources for the hierarchy
	leaf.incPendingResource(pending)
	leaf.removeSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 0, "Application was not removed from the queue as expected")
	assert.Assert(t, resources.IsZero(leaf.pending), "leaf queue pending resource not updated correctly")
	assert.Assert(t, resources.IsZero(root.pending), "root queue pending resource not updated correctly")
}

func TestQueueStates(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")

	// add a leaf under the root
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	err = leaf.QueueInfo.HandleQueueEvent(cache.Stop)
	if err != nil || !leaf.isStopped() {
		t.Errorf("leaf queue is not marked stopped: %v", err)
	}
	err = leaf.QueueInfo.HandleQueueEvent(cache.Start)
	if err != nil || !leaf.isRunning() {
		t.Errorf("leaf queue is not marked running: %v", err)
	}
	err = leaf.QueueInfo.HandleQueueEvent(cache.Remove)
	if err != nil || !leaf.isDraining() {
		t.Errorf("leaf queue is not marked draining: %v", err)
	}
	err = leaf.QueueInfo.HandleQueueEvent(cache.Start)
	if err == nil || !leaf.isDraining() {
		t.Errorf("leaf queue changed state which should not happen: %v", err)
	}
}

func TestAllocatingCalc(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")

	res := map[string]string{"first": "1"}
	var allocation *resources.Resource
	allocation, err = resources.NewResourceFromConf(res)
	assert.NilError(t, err, "failed to create basic resource")
	leaf.incAllocatingResource(allocation)
	if !resources.Equals(root.allocating, allocation) {
		t.Errorf("root queue allocating failed to increment expected %v, got %v", allocation, root.allocating)
	}
	if !resources.Equals(leaf.allocating, allocation) {
		t.Errorf("leaf queue allocating failed to increment expected %v, got %v", allocation, leaf.allocating)
	}
	leaf.decAllocatingResource(allocation)
	if !resources.IsZero(root.allocating) {
		t.Errorf("root queue allocating failed to decrement expected 0, got %v", root.allocating)
	}
	// Not allowed to go negative: both will be zero after this
	root.incAllocatingResource(allocation)
	leaf.decAllocatingResource(allocation)
	// using the get function to access the value
	if !resources.IsZero(root.getAllocatingResource()) {
		t.Errorf("root queue allocating failed to decrement expected zero, got %v", root.allocating)
	}
	if !resources.IsZero(leaf.getAllocatingResource()) {
		t.Errorf("leaf queue allocating should have failed to decrement expected zero, got %v", leaf.allocating)
	}
}

func TestPreemptingCalc(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")

	res := map[string]string{"first": "1"}
	var allocation *resources.Resource
	allocation, err = resources.NewResourceFromConf(res)
	assert.NilError(t, err, "failed to create basic resource")
	if !resources.IsZero(leaf.preempting) {
		t.Errorf("leaf queue preempting resources not set as expected 0, got %v", leaf.preempting)
	}
	if !resources.IsZero(root.preempting) {
		t.Errorf("root queue preempting resources not set as expected 0, got %v", root.preempting)
	}
	// preempting does not filter up the hierarchy, check that
	leaf.incPreemptingResource(allocation)
	// using the get function to access the value
	if !resources.Equals(allocation, leaf.getPreemptingResource()) {
		t.Errorf("queue preempting resources not set as expected %v, got %v", allocation, leaf.preempting)
	}
	if !resources.IsZero(root.getPreemptingResource()) {
		t.Errorf("root queue preempting resources not set as expected 0, got %v", root.preempting)
	}
	newRes := resources.Multiply(allocation, 2)
	leaf.decPreemptingResource(newRes)
	if !resources.IsZero(leaf.getPreemptingResource()) {
		t.Errorf("queue preempting resources not set as expected 0, got %v", leaf.preempting)
	}
	leaf.setPreemptingResource(newRes)
	if !resources.Equals(leaf.getPreemptingResource(), resources.Multiply(allocation, 2)) {
		t.Errorf("queue preempting resources not set as expected %v, got %v", newRes, leaf.preempting)
	}
}

func TestAssumedQueueCalc(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	assumed := leaf.getAssumeAllocated()
	if !resources.IsZero(assumed) {
		t.Errorf("queue unconfirmed and allocated resources not set as expected 0, got %v", assumed)
	}
	res := map[string]string{"first": "1"}
	var allocation *resources.Resource
	allocation, err = resources.NewResourceFromConf(res)
	assert.NilError(t, err, "failed to create basic resource")
	leaf.incAllocatingResource(allocation)
	assumed = leaf.getAssumeAllocated()
	if !resources.Equals(assumed, allocation) {
		t.Errorf("root queue allocating failed to increment expected %v, got %v", allocation, assumed)
	}
	// increase the allocated queue resource, use nodeReported true to bypass checks
	err = leaf.QueueInfo.IncAllocatedResource(allocation, true)
	assert.NilError(t, err, "failed to increase cache queue allocated resource")
	assumed = leaf.getAssumeAllocated()
	allocation = resources.Multiply(allocation, 2)
	if !resources.Equals(assumed, allocation) {
		t.Errorf("root queue allocating failed to increment expected %v, got %v", allocation, assumed)
	}
}

// This test must not test the sorter that is underlying.
// It tests the queue specific parts of the code only.
func TestSortApplications(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf, parent *SchedulingQueue
	// empty parent queue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue: %v")
	if apps := parent.sortApplications(); apps != nil {
		t.Errorf("parent queue should not return sorted apps: %v", apps)
	}

	// empty leaf queue
	leaf, err = createManagedQueue(parent, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	if len(leaf.sortApplications()) != 0 {
		t.Errorf("empty queue should return no app from sort: %v", leaf)
	}
	// new app does not have pending res, does not get returned
	appInfo := cache.NewApplicationInfo("app-1", "default", leaf.QueueInfo.GetQueuePath(), security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	app.queue = leaf
	leaf.addSchedulingApplication(app)
	if len(leaf.sortApplications()) != 0 {
		t.Errorf("app without ask should not be in sorted apps: %v", app)
	}
	var res, delta *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "1"})
	assert.NilError(t, err, "failed to create basic resource")
	// add an ask app must be returned
	delta, err = app.addAllocationAsk(newAllocationAsk("alloc-1", "app-1", res))
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("allocation ask delta expected %v, got %v (err = %v)", res, delta, err)
	}
	sortedApp := leaf.sortApplications()
	if len(sortedApp) != 1 || sortedApp[0].ApplicationInfo.ApplicationID != "app-1" {
		t.Errorf("sorted application is missing expected app: %v", sortedApp)
	}
	// set 0 repeat
	_, err = app.updateAskRepeat("alloc-1", -1)
	if err != nil || len(leaf.sortApplications()) != 0 {
		t.Errorf("app with ask but 0 pending resources should not be in sorted apps: %v (err = %v)", app, err)
	}
}

// This test must not test the sorter that is underlying.
// It tests the queue specific parts of the code only.
func TestSortQueue(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")

	var leaf, parent *SchedulingQueue
	// empty parent queue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	if len(parent.sortQueues()) != 0 {
		t.Errorf("parent queue should return sorted queues: %v", parent)
	}

	// leaf queue must be nil
	leaf, err = createManagedQueue(parent, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	if queues := leaf.sortQueues(); queues != nil {
		t.Errorf("leaf queue should return sorted queues: %v", queues)
	}
	if queues := parent.sortQueues(); len(queues) != 0 {
		t.Errorf("parent queue with leaf returned unexpectd queues: %v", queues)
	}
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "1"})
	assert.NilError(t, err, "failed to create basic resource")
	leaf.incPendingResource(res)
	if queues := parent.sortQueues(); len(queues) != 1 {
		t.Errorf("parent queue did not return expected queues: %v", queues)
	}
	err = leaf.QueueInfo.HandleQueueEvent(cache.Stop)
	assert.NilError(t, err, "failed to stop queue")
	if queues := parent.sortQueues(); len(queues) != 0 {
		t.Errorf("parent queue returned stopped queue: %v", queues)
	}
}

func TestHeadroom(t *testing.T) {
	// create the structure, all queues have no max capacity set
	// structure is:
	// root (max: nil)
	//   - parent (max: nil)
	//     - leaf1 (max: nil)

	// create the root: nil test
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var parent, leaf1 *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")

	headRoom := root.getHeadRoom()
	assert.Assert(t, resources.IsZero(headRoom), "headRoom of root should zero for empty cluster")
	headRoom = parent.getHeadRoom()
	assert.Assert(t, resources.IsZero(headRoom), "headRoom of parent should zero for empty cluster")
	headRoom = leaf1.getHeadRoom()
	assert.Assert(t, resources.IsZero(headRoom), "headRoom of leaf should zero for empty cluster")
}

func TestMaxHeadroom(t *testing.T) {
	// create the structure, all queues have no max capacity set
	// structure is:
	// root (max: nil)
	//   - parent (max: nil)
	//     - leaf1 (max: nil)  (alloc: 5,3)
	//     - leaf2 (max: nil)  (alloc: 5,3)

	// create the root: nil test
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	headRoom := root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "empty cluster with root queue should not have maxHeadRoom")
	var parent *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	leaf2, err = createManagedQueue(parent, "leaf2", false, nil)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating and allocated
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "1", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	leaf2.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "4", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	err = leaf1.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf1")
	err = leaf2.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf2")

	headRoom = root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of root should always be nil")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of parent should be nil because no max set for any queue")
	headRoom = leaf1.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of leaf1 should be nil because no max set for any queue")
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of leaf2 should be nil because no max set for any queue")
}

func TestHeadroomParent(t *testing.T) {
	// create the structure, set max capacity in root, parent and one leaf queue
	// root max is larger than all, no impact (like root max nil)
	// structure is:
	// root	max (20,10)
	// - parent	max (20,8)
	//   - leaf1 max (nil)    alloc (5,3)
	//   - leaf2 max (10,8)   alloc (5,3)
	// set the max on the root
	resMap := map[string]string{"first": "20", "second": "10"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	// set the max on the parent
	resMap = map[string]string{"first": "20", "second": "8"}
	var parent *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	// leaf1 queue no limit
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	// leaf2 queue
	resMap = map[string]string{"first": "10", "second": "8"}
	leaf2, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating and allocated
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "1", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	leaf2.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "4", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	err = leaf1.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf1")
	err = leaf2.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf2")

	// headRoom root should be this (20-10, 10-6)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "10", "second": "4"})
	assert.NilError(t, err, "failed to create resource")
	headRoom := root.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "parent queue head room expected: %s, got: %s", res, headRoom)

	// headRoom parent should be this (20-10, 8-6)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "10", "second": "2"})
	headRoom = parent.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "parent queue head room expected: %s, got: %s", res, headRoom)
	// leaf1 has no max set so same as parent
	headRoom = leaf1.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "leaf1 queue head room expected: %s, got: %s", res, headRoom)

	// headRoom leaf2 will be smaller of leaf2 (10-5, 8-3) & parent (10,2)
	// parent queue has partial lower head room and leaf1 gets limited to parent headroom
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = leaf2.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "leaf2 queue head room expected: %s, got: %s", res, headRoom)
}

func TestHeadroomRootMax(t *testing.T) {
	// create the structure, set max capacity in root, parent and one leaf queue
	// root max partially overrules parent and also indirectly leaf
	// structure is:
	// root (max: 17,10)
	//   - parent (max: 20,8)
	//     - leaf1 (max: nil)   (alloc: 6,4)
	//     - leaf2 (max: 10,8)  (alloc: 5,3)
	resMap := map[string]string{"first": "17", "second": "10"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	var parent *SchedulingQueue
	resMap = map[string]string{"first": "20", "second": "8"}
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	resMap = map[string]string{"first": "10", "second": "8"}
	leaf2, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating and allocated
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "4"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	err = leaf2.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf2")

	// root headroom should be set
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	headRoom := root.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "root queue headRoom expected: %s, got: %s", res, headRoom)

	// smaller of root and parent
	// parent headroom = parentMax - leaf1 - leaf2 = (9, 1)
	// parent gets partially limited to root (6,3)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "multi test: parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf1 headroom same as parent
	headRoom = leaf1.getHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "multi test: leaf1 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
	// leaf2 headroom smaller of max for queue and parent headroom
	// leaf headroom = leafMax - allocated = (5, 5)
	// leaf gets partially limited by parent (6,1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "multi test: leaf2 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
}

func TestMaxHeadroomParent(t *testing.T) {
	// create the structure, set max capacity in root level, verify that will be ignored
	// structure is:
	// root (max: 1, 1)
	//   - parent (max: 20,8)
	//     - leaf1 (max: nil)  (alloc: 5,3)
	//     - leaf2 (max: nil)  (alloc: 6,4)
	resMap := map[string]string{"first": "1", "second": "1"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	resMap = map[string]string{"first": "20", "second": "8"}
	var parent *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	leaf2, err = createManagedQueue(parent, "leaf2", false, nil)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating and allocated
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "1", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	leaf2.incAllocatingResource(res)
	leaf2.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "4", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	err = leaf1.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf1")
	err = leaf2.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf2")

	// root max headroom should be nil
	headRoom := root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of root should always be nil")

	// parent max headroom = parentMax - leaf1 - leaf2
	// parent max headroom = (20 - 5 - 6, 8 - 3 - 4) = (9, 1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "9", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "root test: parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf max headroom = parent max head room (no max set on leafs)
	headRoom = leaf1.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "root test: leaf1 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "root test: leaf2 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
}

func TestMaxHeadroomLeaf(t *testing.T) {
	// create the structure, set max capacity in parent and a leaf queue
	// structure is:
	// root (max: 1,1)
	//   - parent (max: 20,8)
	//     - leaf1 (max: nil)   (alloc: 6,4)
	//     - leaf2 (max: 10,8)  (alloc: 5,3)
	resMap := map[string]string{"first": "1", "second": "1"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	var parent *SchedulingQueue
	resMap = map[string]string{"first": "20", "second": "8"}
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	resMap = map[string]string{"first": "10", "second": "8"}
	leaf2, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating and allocated
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "4"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	err = leaf2.QueueInfo.IncAllocatedResource(res, true)
	assert.NilError(t, err, "failed to set allocated resource on leaf2")

	// root max headroom should be nil
	headRoom := root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of root should always be nil")

	// parent max headroom = parentMax - leaf1Allocated - leaf2Allocated
	// parent max headroom = (20 - 5 - 6, 8 - 3 - 4) = (9, 1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "9", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "leaf test: parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf1 max headroom same as parent
	headRoom = leaf1.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "leaf test: leaf1 queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf2 with max set
	// leaf2 max headroom = MIN(parentHeadRoom, leaf1Max - leaf1Allocated)
	// leaf2 max headroom = MIN((9,1), (10-5, 8-3)) = MIN((9,1), (5,5)) = (5, 1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "leaf test: leaf2 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
}

func TestMaxHeadroomUpscale(t *testing.T) {
	// create the structure, set max capacity in parent and a leaf queue
	// upscale is set on the leaf queue without a max
	// structure is:
	// root (max: 1,1)
	//   - parent (max: 20,10)
	//     - leaf1 (max: nil)  (alloc: 6,4)  (upscale: 4,2)
	//     - leaf2 (max: 8,3)  (alloc: 5,3)
	resMap := map[string]string{"first": "1", "second": "1"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	var parent *SchedulingQueue
	resMap = map[string]string{"first": "20", "second": "10"}
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	resMap = map[string]string{"first": "8", "second": "3"}
	leaf2, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "4"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	leaf2.incAllocatingResource(res)
	// upscaling
	res, err = resources.NewResourceFromConf(map[string]string{"first": "4", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incUpScalingResource(res)

	// root max headroom should be nil
	headRoom := root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of root should always be nil")

	// parent max headroom = parentMax - leaf1 - leaf2 - upscaling
	// parent max headroom = (20 - 5 - 6 - 4, 10 - 3 - 4 - 2) = (5, 1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf1 max headroom no max set: same as parent
	headRoom = leaf1.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: leaf1 queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf2 with max set (no upscale)
	// leaf2 max headroom = MIN(parentMaxHeadRoom, leaf1Max - leaf1Allocated)
	// leaf2 max headroom = MIN((5,1), (8-5, 3-3)) = MIN((5,1), (3,0)) = (3,0)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "3", "second": "0"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: leaf2 queue maxHeadRoom expected: %s, got: %s", res, headRoom)
}

func TestMaxHeadroomUpscaleWithMax(t *testing.T) {
	// create the structure, set max capacity in parent and a leaf queue
	// upscale is set on the leaf queue with max set
	// structure is:
	// root (max: 20,10)
	//   - parent (max: 20,10)
	//     - leaf1 (max: nil)   (alloc: 5,3)
	//     - leaf2 (max: 12,6)  (alloc: 6,4)  (upscale: 4,2)
	resMap := map[string]string{"first": "20", "second": "10"}
	root, err := createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	var parent *SchedulingQueue
	resMap = map[string]string{"first": "20", "second": "10"}
	parent, err = createManagedQueue(root, "parent", true, resMap)
	assert.NilError(t, err, "failed to create parent queue")
	var leaf1, leaf2 *SchedulingQueue
	leaf1, err = createManagedQueue(parent, "leaf1", false, nil)
	assert.NilError(t, err, "failed to create leaf1 queue")
	resMap = map[string]string{"first": "12", "second": "6"}
	leaf2, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")

	// allocating
	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "6", "second": "4"})
	assert.NilError(t, err, "failed to create resource")
	leaf2.incAllocatingResource(res)
	// upscaling
	res, err = resources.NewResourceFromConf(map[string]string{"first": "4", "second": "2"})
	assert.NilError(t, err, "failed to create resource")
	leaf2.incUpScalingResource(res)

	// root max headroom should be nil
	headRoom := root.getMaxHeadRoom()
	assert.Assert(t, headRoom == nil, "maxHeadRoom of root should always be nil")

	// parent max headroom = parentMax - leaf1 - leaf2 - upscaling
	// parent max headroom = (20 - 5 - 6 - 4, 10 - 3 - 4 - 2) = (5,1)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "1"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf1 max headroom no max set: same as parent
	headRoom = leaf1.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: leaf1 queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// leaf2 with max set and upscale
	// leaf2 max headroom = MIN(parentMaxHeadRoom, leaf2max - leaf2 - leaf2upscale)
	// leaf2 max headroom = MIN((5,1), (12 - 6 - 4, 6 - 4 - 2)) = MIN((5,1), (2,0)) = (2,0)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "2", "second": "0"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = leaf2.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: leaf2 queue maxHeadRoom expected: %s, got: %s", res, headRoom)

	// increase allocated on the first queue to use all resources
	res, err = resources.NewResourceFromConf(map[string]string{"first": "9", "second": "3"})
	assert.NilError(t, err, "failed to create resource")
	leaf1.incAllocatingResource(res)

	// parent should now have a negative maxHeadRoom (all upscale resources)
	res, err = resources.NewResourceFromConf(map[string]string{"first": "-4", "second": "-2"})
	assert.NilError(t, err, "failed to create resource")
	headRoom = parent.getMaxHeadRoom()
	assert.Assert(t, resources.Equals(res, headRoom), "upscale test: allocated parent queue maxHeadRoom expected: %s, got: %s", res, headRoom)
	// parent should now have a zero headRoom, root max is exactly same as allocated
	headRoom = parent.getHeadRoom()
	assert.Assert(t, resources.IsZero(headRoom), "upscale test: parent queue headRoom expected zero resource got: %s", headRoom)
}

func TestGetMaxUsage(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	maxUsage := root.getMaxResource()
	if maxUsage != nil {
		t.Errorf("empty cluster with root queue should not have max set: %v", maxUsage)
	}

	var parent *SchedulingQueue
	// empty parent queue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	maxUsage = parent.getMaxResource()
	if maxUsage != nil {
		t.Errorf("empty cluster parent queue should not have max set: %v", maxUsage)
	}

	// set the max on the root: recreate the structure to pick up changes
	var res *resources.Resource
	resMap := map[string]string{"first": "10", "second": "5"}
	res, err = resources.NewResourceFromConf(resMap)
	assert.NilError(t, err, "failed to create resource")
	root, err = createRootQueue(resMap)
	assert.NilError(t, err, "failed to create root queue with limit")
	maxUsage = root.getMaxResource()
	if !resources.Equals(res, maxUsage) {
		t.Errorf("root queue should have max set expected %v, got: %v", res, maxUsage)
	}
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	maxUsage = parent.getMaxResource()
	if !resources.Equals(res, maxUsage) {
		t.Errorf("parent queue should have max from root set expected %v, got: %v", res, maxUsage)
	}

	// leaf queue with limit: contrary to root should get min from both merged
	var leaf *SchedulingQueue
	resMap = map[string]string{"first": "5", "second": "10"}
	leaf, err = createManagedQueue(parent, "leaf", false, resMap)
	assert.NilError(t, err, "failed to create leaf queue")
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "5"})
	assert.NilError(t, err, "failed to create resource")
	maxUsage = leaf.getMaxResource()
	if !resources.Equals(res, maxUsage) {
		t.Errorf("leaf queue should have merged max set expected %v, got: %v", res, maxUsage)
	}

	// replace parent with one with limit on different resource
	resMap = map[string]string{"third": "2"}
	parent, err = createManagedQueue(root, "parent2", true, resMap)
	assert.NilError(t, err, "failed to create parent2 queue")
	maxUsage = parent.getMaxResource()
	res, err = resources.NewResourceFromConf(map[string]string{"first": "0", "second": "0", "third": "0"})
	if err != nil || !resources.Equals(res, maxUsage) {
		t.Errorf("parent2 queue should have max from root set expected %v, got: %v (err %v)", res, maxUsage, err)
	}
	resMap = map[string]string{"first": "5", "second": "10"}
	leaf, err = createManagedQueue(parent, "leaf2", false, resMap)
	assert.NilError(t, err, "failed to create leaf2 queue")
	maxUsage = leaf.getMaxResource()
	res, err = resources.NewResourceFromConf(map[string]string{"first": "0", "second": "0", "third": "0"})
	if err != nil || !resources.Equals(res, maxUsage) {
		t.Errorf("leaf2 queue should have reset merged max set expected %v, got: %v (err %v)", res, maxUsage, err)
	}
}

func TestReserveApp(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	assert.Equal(t, len(leaf.reservedApps), 0, "new queue should not have reserved apps")
	// no checks this works for everything
	appName := "something"
	leaf.reserve(appName)
	assert.Equal(t, len(leaf.reservedApps), 1, "app should have been reserved")
	assert.Equal(t, leaf.reservedApps[appName], 1, "app should have one reservation")
	leaf.reserve(appName)
	assert.Equal(t, leaf.reservedApps[appName], 2, "app should have two reservations")
	leaf.unReserve(appName)
	leaf.unReserve(appName)
	assert.Equal(t, len(leaf.reservedApps), 0, "queue should not have any reserved apps, all reservations were removed")

	leaf.unReserve("unknown")
	assert.Equal(t, len(leaf.reservedApps), 0, "unreserve of unknown app should not have changed count or added app")
}

func TestGetApp(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	// check for init of the map
	if unknown := leaf.getApplication("unknown"); unknown != nil {
		t.Errorf("un registered app found using appID which should not happen: %v", unknown)
	}

	// add app and check proper returns
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "app-1"})
	leaf.addSchedulingApplication(app)
	assert.Equal(t, len(leaf.applications), 1, "queue should have one app registered")
	if leaf.getApplication("app-1") == nil {
		t.Errorf("registered app not found using appID")
	}
	if unknown := leaf.getApplication("unknown"); unknown != nil {
		t.Errorf("un registered app found using appID which should not happen: %v", unknown)
	}
}

func TestIsEmpty(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	assert.Equal(t, root.isEmpty(), true, "new root queue should have been empty")
	var leaf *SchedulingQueue
	leaf, err = createManagedQueue(root, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")
	assert.Equal(t, root.isEmpty(), false, "root queue with child leaf should not have been empty")
	assert.Equal(t, leaf.isEmpty(), true, "new leaf should have been empty")

	// add app and check proper returns
	app := newSchedulingApplication(&cache.ApplicationInfo{ApplicationID: "app-1"})
	leaf.addSchedulingApplication(app)
	assert.Equal(t, leaf.isEmpty(), false, "queue with registered app should not be empty")
}

func TestUpScalingCalc(t *testing.T) {
	// create the root
	root, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	var parent, leaf *SchedulingQueue
	parent, err = createManagedQueue(root, "parent", true, nil)
	assert.NilError(t, err, "failed to create parent queue")
	leaf, err = createManagedQueue(parent, "leaf", false, nil)
	assert.NilError(t, err, "failed to create leaf queue")

	var res *resources.Resource
	res, err = resources.NewResourceFromConf(map[string]string{"first": "5", "second": "7"})
	assert.NilError(t, err, "failed to create basic resource")
	leaf.incUpScalingResource(res)
	assert.Assert(t, resources.IsZero(root.upScaling), "root upscaling should be zero, got: %s", root.upScaling)
	assert.Assert(t, resources.Equals(res, parent.upScaling), "parent queue upscaling increment expected: %s, got: %s", res, parent.upScaling)
	assert.Assert(t, resources.Equals(res, leaf.upScaling), "leaf queue upscaling increment expected: %s, got: %s", res, leaf.upScaling)

	res, err = resources.NewResourceFromConf(map[string]string{"first": "3", "second": "4"})
	assert.NilError(t, err, "failed to create basic resource")
	leaf.decUpScalingResource(res)

	var left *resources.Resource
	left, err = resources.NewResourceFromConf(map[string]string{"first": "2", "second": "3"})
	assert.NilError(t, err, "failed to create basic resource")
	assert.Assert(t, resources.IsZero(root.upScaling), "root upscaling should be zero, got: %s", root.upScaling)
	assert.Assert(t, resources.Equals(left, parent.upScaling), "parent queue upscaling decrement expected: %s, got: %s", left, parent.upScaling)
	assert.Assert(t, resources.Equals(left, leaf.upScaling), "leaf queue upscaling decrement expected: %s, got: %s", left, leaf.upScaling)

	var parentAdd *resources.Resource
	parentAdd, err = resources.NewResourceFromConf(map[string]string{"first": "2"})
	parent.incUpScalingResource(parentAdd)
	assert.NilError(t, err, "failed to create basic resource")
	// Not allowed to go negative for any resource type
	// leaf should come back as 0 real (-1,-1), parent as (1,0) real (1,-1)
	left, err = resources.NewResourceFromConf(map[string]string{"first": "1"})
	assert.NilError(t, err, "failed to create basic resource")
	leaf.decUpScalingResource(res)
	assert.Assert(t, resources.IsZero(root.upScaling), "root upscaling should be zero, got: %s", root.upScaling)
	assert.Assert(t, resources.Equals(left, parent.upScaling), "parent queue upscaling decrement expected: %s, got: %s", left, parent.upScaling)
	assert.Assert(t, resources.IsZero(leaf.upScaling), "leaf queue upscaling decrement expected zero, got: %s", leaf.upScaling)
}
