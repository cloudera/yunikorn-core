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
	"strings"
	"testing"
	"time"

	"gotest.tools/assert"

	"github.com/apache/incubator-yunikorn-core/pkg/cache"
	"github.com/apache/incubator-yunikorn-core/pkg/common"
	"github.com/apache/incubator-yunikorn-core/pkg/common/resources"
	"github.com/apache/incubator-yunikorn-core/pkg/common/security"
	"github.com/apache/incubator-yunikorn-core/pkg/events"
	"github.com/apache/incubator-yunikorn-scheduler-interface/lib/go/si"
)

// test allocating calculation
func TestAppAllocating(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	if !resources.IsZero(app.allocating) {
		t.Fatalf("app should not have allocating resources: %v", app.allocating.Resources)
	}
	// simple one resource add
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 1})
	app.incAllocatingResource(res)
	assert.Equal(t, len(app.allocating.Resources), 1, "app allocating resources not showing correct resources numbers")
	if !resources.Equals(res, app.getAllocatingResource()) {
		t.Errorf("app allocating resources not incremented correctly: %v", app.allocating.Resources)
	}

	// inc with a second resource type: should merge
	res2 := resources.NewResourceFromMap(map[string]resources.Quantity{"second": 1})
	resTotal := resources.Add(res, res2)
	app.incAllocatingResource(res2)
	assert.Equal(t, len(app.allocating.Resources), 2, "app allocating resources not showing correct resources numbers")
	if !resources.Equals(resTotal, app.getAllocatingResource()) {
		t.Errorf("app allocating resources not incremented correctly: %v", app.allocating.Resources)
	}

	// dec just left with the second resource type
	app.decAllocatingResource(res)
	assert.Equal(t, len(app.allocating.Resources), 2, "app allocating resources not showing correct resources numbers")
	if !resources.Equals(res2, app.getAllocatingResource()) {
		t.Errorf("app allocating resources not decremented correctly: %v", app.allocating.Resources)
	}
	// dec with total: one resource type would go negative but cannot
	app.decAllocatingResource(resTotal)
	assert.Equal(t, len(app.allocating.Resources), 2, "app allocating resources not showing correct resources numbers")
	if !resources.IsZero(app.getAllocatingResource()) {
		t.Errorf("app should not have allocating resources: %v", app.allocating.Resources)
	}
}

// test basic reservations
func TestAppReservation(t *testing.T) {
	// init event cache
	events.CreateAndSetEventCache()
	defer events.ResetCache()
	eventCache := events.GetEventCache()
	eventCache.StartService()

	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	if app.hasReserved() {
		t.Fatal("new app should not have reservations")
	}
	if app.isReservedOnNode("") {
		t.Error("app should not have reservations for empty node ID")
	}
	if app.isReservedOnNode("unknown") {
		t.Error("new app should not have reservations for unknown node")
	}

	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// reserve illegal request
	err = app.reserve(nil, nil)
	if err == nil {
		t.Errorf("illegal reservation requested but did not fail: error %v", err)
	}

	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 15})
	askKey := "alloc-1"
	ask := newAllocationAsk(askKey, appID, res)
	nodeID := "node-1"
	node := newNode(nodeID, map[string]resources.Quantity{"first": 10})

	// too large for node
	err = app.reserve(node, ask)
	if err == nil {
		t.Errorf("requested reservation does not fit in node resource but did not fail: error %v", err)
	}

	res = resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask = newAllocationAsk(askKey, appID, res)
	app = newSchedulingApplication(appInfo)
	app.queue = queue
	var delta *resources.Resource
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	// reserve that works
	err = app.reserve(node, ask)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	if app.isReservedOnNode("") {
		t.Errorf("app should not have reservations for empty node ID")
	}
	if app.isReservedOnNode("unknown") {
		t.Error("app should not have reservations for unknown node")
	}
	if app.hasReserved() && !app.isReservedOnNode(nodeID) {
		t.Errorf("app should have reservations for node %s", nodeID)
	}

	// reserve the same reservation
	err = app.reserve(node, ask)
	if err == nil {
		t.Errorf("reservation should have failed: error %v", err)
	}

	// unreserve unknown node/ask
	var num int
	_, err = app.unReserve(nil, nil)
	if err == nil {
		t.Errorf("illegal reservation release but did not fail: error %v", err)
	}

	// 2nd reservation for app
	askKey2 := "alloc-res-2"
	nodeID2 := "node-res-2"
	ask2 := newAllocationAsk(askKey2, appID, res)
	node2 := newNode(nodeID2, map[string]resources.Quantity{"first": 10})
	delta, err = app.addAllocationAsk(ask2)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask2 should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	err = app.reserve(node, ask2)
	if err == nil {
		t.Errorf("reservation of node by second ask should have failed: error %v", err)
	}
	err = app.reserve(node2, ask2)
	if err != nil {
		t.Errorf("reservation of 2nd node should not have failed: error %v", err)
	}
	_, err = app.unReserve(node2, ask2)
	assertAllocAppAndNodeEvents(t, eventCache, askKey2, appID, nodeID2)
	assert.NilError(t, err, "remove of reservation of 2nd node should not have failed: error %v", err)

	// unreserve the same should not fail
	_, err = app.unReserve(node2, ask2)
	assertAllocAppAndNodeEvents(t, eventCache, askKey2, appID, nodeID2)
	assert.NilError(t, err, "remove twice of reservation of 2nd node should not have failed: error %v", err)

	// failure case: remove reservation from node, app still needs cleanup
	num, err = node.unReserve(app, ask)
	assert.NilError(t, err, "un-reserve on node should not have failed with error")
	assert.Equal(t, num, 1, "un-reserve on node should have removed reservation")
	num, err = app.unReserve(node, ask)
	assertAllocAppAndNodeEvents(t, eventCache, askKey, appID, nodeID)
	assert.NilError(t, err, "app has reservation should not have failed")
	assert.Equal(t, num, 1, "un-reserve on app should have removed reservation from app")
}

// test multiple reservations from one allocation
func TestAppAllocReservation(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	if app.hasReserved() {
		t.Fatal("new app should not have reservations")
	}
	if len(app.isAskReserved("")) != 0 {
		t.Fatal("new app should not have reservation for empty allocKey")
	}
	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// reserve 1 allocate ask
	allocKey := "alloc-1"
	nodeID1 := "node-1"
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask := newAllocationAskRepeat(allocKey, appID, res, 2)
	node1 := newNode(nodeID1, map[string]resources.Quantity{"first": 10})
	// reserve that works
	var delta *resources.Resource
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(resources.Multiply(res, 2), delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, resources.Multiply(res, 2), delta)
	}
	err = app.reserve(node1, ask)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	if len(app.isAskReserved("")) != 0 {
		t.Fatal("app should not have reservation for empty allocKey")
	}
	nodeKey1 := nodeID1 + "|" + allocKey
	askReserved := app.isAskReserved(allocKey)
	if len(askReserved) != 1 || askReserved[0] != nodeKey1 {
		t.Errorf("app should have reservations for %s on %s and has not", allocKey, nodeID1)
	}

	nodeID2 := "node-2"
	node2 := newNode(nodeID2, map[string]resources.Quantity{"first": 10})
	err = app.reserve(node2, ask)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	nodeKey2 := nodeID2 + "|" + allocKey
	askReserved = app.isAskReserved(allocKey)
	if len(askReserved) != 2 && (askReserved[0] != nodeKey2 || askReserved[1] != nodeKey2) {
		t.Errorf("app should have reservations for %s on %s and has not", allocKey, nodeID2)
	}

	// check exceeding ask repeat: nothing should change
	if app.canAskReserve(ask) {
		t.Error("ask has maximum repeats reserved, reserve check should have failed")
	}
	node3 := newNode("node-3", map[string]resources.Quantity{"first": 10})
	err = app.reserve(node3, ask)
	if err == nil {
		t.Errorf("reservation should have failed: error %v", err)
	}
	askReserved = app.isAskReserved(allocKey)
	if len(askReserved) != 2 && (askReserved[0] != nodeKey1 || askReserved[1] != nodeKey1) &&
		(askReserved[0] != nodeKey2 || askReserved[1] != nodeKey2) {
		t.Errorf("app should have reservations for node %s and %s and has not: %v", nodeID1, nodeID2, askReserved)
	}
	// clean up all asks and reservations
	reservedAsks := app.removeAllocationAsk("")
	if app.hasReserved() || node1.isReserved() || node2.isReserved() || reservedAsks != 2 {
		t.Errorf("ask removal did not clean up all reservations, reserved released = %d", reservedAsks)
	}
}

// test update allocation repeat
func TestUpdateRepeat(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// failure cases
	delta, err := app.updateAskRepeat("", 0)
	if err == nil || delta != nil {
		t.Error("empty ask key should not have been found")
	}
	delta, err = app.updateAskRepeat("unknown", 0)
	if err == nil || delta != nil {
		t.Error("unknown ask key should not have been found")
	}

	// working cases
	allocKey := "alloc-1"
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask := newAllocationAskRepeat(allocKey, appID, res, 1)
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	delta, err = app.updateAskRepeat(allocKey, 0)
	if err != nil || !resources.IsZero(delta) {
		t.Errorf("0 increase should return zero delta and did not: %v, err %v", delta, err)
	}
	delta, err = app.updateAskRepeat(allocKey, 1)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("increase did not return correct delta, err %v, expected %v got %v", err, res, delta)
	}

	// decrease to zero
	delta, err = app.updateAskRepeat(allocKey, -2)
	if err != nil || !resources.Equals(resources.Multiply(res, -2), delta) {
		t.Errorf("decrease did not return correct delta, err %v, expected %v got %v", err, resources.Multiply(res, -2), delta)
	}
	// decrease to below zero
	delta, err = app.updateAskRepeat(allocKey, -1)
	if err == nil || delta != nil {
		t.Errorf("decrease did not return correct delta, err %v, delta %v", err, delta)
	}
}

// test pending calculation and ask addition
func TestAddAllocAsk(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}

	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// failure cases
	delta, err := app.addAllocationAsk(nil)
	if err == nil {
		t.Errorf("nil ask should not have been added to app, returned delta: %v", delta)
	}
	allocKey := "alloc-1"
	res := resources.NewResource()
	ask := newAllocationAsk(allocKey, appID, res)
	delta, err = app.addAllocationAsk(ask)
	if err == nil {
		t.Errorf("zero resource ask should not have been added to app, returned delta: %v", delta)
	}
	res = resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask = newAllocationAskRepeat(allocKey, appID, res, 0)
	delta, err = app.addAllocationAsk(ask)
	if err == nil {
		t.Errorf("ask with zero repeat should not have been added to app, returned delta: %v", delta)
	}

	// working cases
	res = resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask = newAllocationAskRepeat(allocKey, appID, res, 1)
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	if !resources.Equals(res, app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected %v but was: %v", res, delta)
	}
	ask = newAllocationAskRepeat(allocKey, appID, res, 2)
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	if !resources.Equals(resources.Multiply(res, 2), app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected %v but was: %v", resources.Multiply(res, 2), delta)
	}

	// change both resource and count
	ask = newAllocationAskRepeat(allocKey, appID, resources.NewResourceFromMap(map[string]resources.Quantity{"first": 3}), 5)
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(res, delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, res, delta)
	}
	if !resources.Equals(resources.Multiply(res, 3), app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected %v but was: %v", resources.Multiply(res, 3), delta)
	}

	// test a decrease of repeat and back to start
	ask = newAllocationAskRepeat(allocKey, appID, res, 1)
	delta, err = app.addAllocationAsk(ask)
	if err != nil || !resources.Equals(resources.Multiply(res, -2), delta) {
		t.Errorf("ask should have been added to app, err %v, expected delta %v but was: %v", err, resources.Multiply(res, -2), delta)
	}
	if !resources.Equals(res, app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected %v but was: %v", res, delta)
	}
}

// test reservations removal by allocation
func TestRemoveReservedAllocAsk(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// create app and allocs
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask1 := newAllocationAsk("alloc-1", appID, res)
	delta, err := app.addAllocationAsk(ask1)
	if err != nil || !resources.Equals(res, delta) {
		t.Fatalf("resource ask1 should have been added to app: %v (err = %v)", delta, err)
	}
	allocKey := "alloc-2"
	ask2 := newAllocationAsk(allocKey, appID, res)
	delta, err = app.addAllocationAsk(ask2)
	if err != nil || !resources.Equals(res, delta) {
		t.Fatalf("resource ask2 should have been added to app: %v (err = %v)", delta, err)
	}
	// reserve one alloc and remove
	nodeID := "node-1"
	node := newNode(nodeID, map[string]resources.Quantity{"first": 10})
	err = app.reserve(node, ask2)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	if len(app.isAskReserved(allocKey)) != 1 || !node.isReserved() {
		t.Fatalf("app should have reservation for %v on node", allocKey)
	}
	before := app.GetPendingResource().Clone()
	reservedAsks := app.removeAllocationAsk(allocKey)
	delta = resources.Sub(before, app.GetPendingResource())
	if !resources.Equals(res, delta) || reservedAsks != 1 {
		t.Errorf("resource ask2 should have been removed from app: %v, (reserved released = %d)", delta, reservedAsks)
	}
	if app.hasReserved() || node.isReserved() {
		t.Fatal("app and node should not have reservations")
	}

	// reserve again: then remove from node before remove from app
	delta, err = app.addAllocationAsk(ask2)
	if err != nil || !resources.Equals(res, delta) {
		t.Fatalf("resource ask2 should have been added to app: %v (err = %v)", delta, err)
	}
	err = app.reserve(node, ask2)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	if len(app.isAskReserved(allocKey)) != 1 || !node.isReserved() {
		t.Fatalf("app should have reservation for %v on node", allocKey)
	}
	var num int
	num, err = node.unReserve(app, ask2)
	assert.NilError(t, err, "un-reserve on node should not have failed")
	assert.Equal(t, num, 1, "un-reserve on node should have removed reservation")

	before = app.GetPendingResource().Clone()
	reservedAsks = app.removeAllocationAsk(allocKey)
	delta = resources.Sub(before, app.GetPendingResource())
	if !resources.Equals(res, delta) || reservedAsks != 1 {
		t.Errorf("resource ask2 should have been removed from app: %v, (reserved released = %d)", delta, reservedAsks)
	}
	// app reservation is removed even though the node removal failed
	if app.hasReserved() || node.isReserved() {
		t.Fatal("app and node should not have reservations")
	}
	// add a new reservation: use the existing ask1
	err = app.reserve(node, ask1)
	if err != nil {
		t.Errorf("reservation should not have failed: error %v", err)
	}
	// clean up
	reservedAsks = app.removeAllocationAsk("")
	if !resources.IsZero(app.GetPendingResource()) || reservedAsks != 1 {
		t.Errorf("all resource asks should have been removed from app: %v, (reserved released = %d)", app.GetPendingResource(), reservedAsks)
	}
	// app reservation is removed due to ask removal
	if app.hasReserved() || node.isReserved() {
		t.Fatal("app and node should not have reservations")
	}
}

// test pending calculation and ask removal
func TestRemoveAllocAsk(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")
	app.queue = queue

	// failures cases: things should not crash (nothing happens)
	reservedAsks := app.removeAllocationAsk("")
	if !resources.IsZero(app.GetPendingResource()) || reservedAsks != 0 {
		t.Errorf("pending resource not updated correctly removing all, expected zero but was: %v", app.GetPendingResource())
	}
	reservedAsks = app.removeAllocationAsk("unknown")
	if !resources.IsZero(app.GetPendingResource()) || reservedAsks != 0 {
		t.Errorf("pending resource not updated correctly removing unknown, expected zero but was: %v", app.GetPendingResource())
	}

	// setup the allocs
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 5})
	ask := newAllocationAskRepeat("alloc-1", appID, res, 2)
	var delta1 *resources.Resource
	delta1, err = app.addAllocationAsk(ask)
	assert.NilError(t, err, "ask 1 should have been added to app, returned delta")
	ask = newAllocationAskRepeat("alloc-2", appID, res, 2)
	var delta2 *resources.Resource
	delta2, err = app.addAllocationAsk(ask)
	assert.NilError(t, err, "ask 2 should have been added to app, returned delta")
	if len(app.requests) != 2 {
		t.Fatalf("missing asks from app expected 2 got %d", len(app.requests))
	}
	if !resources.Equals(resources.Add(delta1, delta2), app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected %v but was: %v", resources.Add(delta1, delta2), app.GetPendingResource())
	}

	// test removes unknown (nothing happens)
	reservedAsks = app.removeAllocationAsk("unknown")
	if reservedAsks != 0 {
		t.Errorf("asks released which did not exist: %d", reservedAsks)
	}
	before := app.GetPendingResource().Clone()
	reservedAsks = app.removeAllocationAsk("alloc-1")
	delta := resources.Sub(before, app.GetPendingResource())
	if !resources.Equals(delta, delta1) || reservedAsks != 0 {
		t.Errorf("ask should have been removed from app, err %v, expected delta %v but was: %v, (reserved released = %d)", err, delta1, delta, reservedAsks)
	}
	reservedAsks = app.removeAllocationAsk("")
	if len(app.requests) != 0 || reservedAsks != 0 {
		t.Fatalf("asks not removed as expected 0 got %d, (reserved released = %d)", len(app.requests), reservedAsks)
	}
	if !resources.IsZero(app.GetPendingResource()) {
		t.Errorf("pending resource not updated correctly, expected zero but was: %v", app.GetPendingResource())
	}
}

// test allocating and allocated calculation
func TestAssumedAppCalc(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	assumed := app.getAssumeAllocated()
	if !resources.IsZero(assumed) {
		t.Errorf("app unconfirmed and allocated resources not set as expected 0, got %v", assumed)
	}
	res := map[string]string{"first": "1"}
	allocation, err := resources.NewResourceFromConf(res)
	assert.NilError(t, err, "failed to create basic resource")
	app.incAllocatingResource(allocation)
	assumed = app.getAssumeAllocated()
	if !resources.Equals(allocation, assumed) {
		t.Errorf("app unconfirmed and allocated resources not set as expected %v, got %v", allocation, assumed)
	}
	allocInfo := cache.CreateMockAllocationInfo("app-1", allocation, "uuid", "root.leaf", "node-1")
	cache.AddAllocationToApp(app.ApplicationInfo, allocInfo)
	assumed = app.getAssumeAllocated()
	allocation = resources.Multiply(allocation, 2)
	if !resources.Equals(allocation, assumed) {
		t.Errorf("app unconfirmed and allocated resources not set as expected %v, got %v", allocation, assumed)
	}
}

// This test must not test the sorter that is underlying.
// It tests the queue specific parts of the code only.
func TestSortRequests(t *testing.T) {
	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	if app.sortedRequests != nil {
		t.Fatalf("new app create should not have sorted requests: %v", app)
	}
	app.sortRequests(true)
	if app.sortedRequests != nil {
		t.Fatalf("after sort call (no pending resources) list must be nil: %v", app.sortedRequests)
	}

	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 1})
	for i := 1; i < 4; i++ {
		num := strconv.Itoa(i)
		ask := newAllocationAsk("ask-"+num, appID, res)
		ask.priority = int32(i)
		app.requests[ask.AskProto.AllocationKey] = ask
	}
	app.sortRequests(true)
	if len(app.sortedRequests) != 3 {
		t.Fatalf("app sorted requests not correct: %v", app.sortedRequests)
	}
	allocKey := app.sortedRequests[0].AskProto.AllocationKey
	delete(app.requests, allocKey)
	app.sortRequests(true)
	if len(app.sortedRequests) != 2 {
		t.Fatalf("app sorted requests not correct after removal: %v", app.sortedRequests)
	}
}

func TestStateChangeOnAskUpdate(t *testing.T) {
	// create a fake queue
	queue, err := createRootQueue(nil)
	assert.NilError(t, err, "queue create failed")

	appID := "app-1"
	appInfo := cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app := newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	// fake the queue assignment
	app.queue = queue
	// app should be new
	assert.Assert(t, app.isNew(), "New application did not return new state: %s", app.ApplicationInfo.GetApplicationState())
	res := resources.NewResourceFromMap(map[string]resources.Quantity{"first": 1})
	askID := "ask-1"
	ask := newAllocationAsk(askID, appID, res)
	_, err = app.addAllocationAsk(ask)
	assert.NilError(t, err, "ask should have been added to app, returned delta")
	// app with ask should be accepted
	assert.Assert(t, app.isAccepted(), "application did not change to accepted state: %s", app.ApplicationInfo.GetApplicationState())

	// removing the ask should move it to waiting
	released := app.removeAllocationAsk(askID)
	assert.Equal(t, released, 0, "allocation ask should not have been reserved")
	assert.Assert(t, app.isWaiting(), "application did not change to waiting state: %s", app.ApplicationInfo.GetApplicationState())

	// start with a fresh state machine
	appInfo = cache.NewApplicationInfo(appID, "default", "root.unknown", security.UserGroup{}, nil)
	app = newSchedulingApplication(appInfo)
	if app == nil || app.ApplicationInfo.ApplicationID != appID {
		t.Fatalf("app create failed which should not have %v", app)
	}
	// fake the queue assignment
	app.queue = queue
	// app should be new
	assert.Assert(t, app.isNew(), "New application did not return new state: %s", app.ApplicationInfo.GetApplicationState())
	_, err = app.addAllocationAsk(ask)
	assert.NilError(t, err, "ask should have been added to app, returned delta")
	// app with ask should be accepted
	assert.Assert(t, app.isAccepted(), "application did not change to accepted state: %s", app.ApplicationInfo.GetApplicationState())
	// add an alloc
	uuid := "uuid-1"
	allocInfo := cache.CreateMockAllocationInfo(appID, res, uuid, "root.unknown", "node-1")
	cache.AddAllocationToApp(appInfo, allocInfo)
	// app should be starting
	assert.Assert(t, app.isStarting(), "application did not return starting state after alloc: %s", app.ApplicationInfo.GetApplicationState())

	// removing the ask should not move anywhere as there is an allocation
	released = app.removeAllocationAsk(askID)
	assert.Equal(t, released, 0, "allocation ask should not have been reserved")
	assert.Assert(t, app.isStarting(), "application changed state unexpectedly: %s", app.ApplicationInfo.GetApplicationState())

	// remove the allocation
	cache.RemoveAllocationFromApp(appInfo, uuid)
	// set in flight allocation
	app.allocating = res
	// add an ask with the repeat set to 0 (cannot use the proper way)
	ask = newAllocationAskRepeat(askID, appID, res, 0)
	app.requests[askID] = ask

	// with allocations in flight we should not change state
	released = app.removeAllocationAsk(askID)
	assert.Equal(t, released, 0, "allocation ask should not have been reserved")
	assert.Assert(t, app.isStarting(), "application changed state unexpectedly: %s", app.ApplicationInfo.GetApplicationState())
}

func TestEmitAllocatedReservedEvents(t *testing.T) {
	events.CreateAndSetEventCache()
	defer events.ResetCache()
	eventCache := events.GetEventCache()
	eventCache.StartService()

	allocKey := "allocation-1"
	appID := "application-1"
	nodeID := "node-1"

	EmitAllocatedReservedEvents(allocKey, appID, nodeID)

	assertAllocAppAndNodeEvents(t, eventCache, allocKey, appID, nodeID)
}

func TestEmitUnReserveEventForNode(t *testing.T) {
	events.CreateAndSetEventCache()
	defer events.ResetCache()
	eventCache := events.GetEventCache()
	eventCache.StartService()

	allocKey := "alloc-2"
	appID := "application2"
	nodeID := "node-2"

	EmitUnReserveEventForNode(allocKey, appID, nodeID)

	err := common.WaitFor(1*time.Millisecond, 10*time.Millisecond, func() bool {
		return eventCache.Store.CountStoredEvents() >= 1
	})
	assert.NilError(t, err, "the event should have been processed")

	records := eventCache.Store.CollectEvents()
	assert.Equal(t, len(records), 1)
	record := records[0]
	msg := record.Message
	assert.Assert(t, strings.Contains(msg, allocKey), "allocation key not found in event message")
	assert.Assert(t, strings.Contains(msg, appID), "app ID not found in event message")
	assert.Assert(t, strings.Contains(msg, nodeID), "node ID not found in event message")
	assert.Equal(t, record.Type, si.EventRecord_NODE)
	assert.Equal(t, record.ObjectID, nodeID, "the object ID is the not of the node")
}

func assertAllocAppAndNodeEvents(t *testing.T, eventCache *events.EventCache, allocKey, appID, nodeID string) {
	err := common.WaitFor(1*time.Millisecond, 10*time.Millisecond, func() bool {
		return eventCache.Store.CountStoredEvents() >= 3
	})
	assert.NilError(t, err, "the event should have been processed")

	records := eventCache.Store.CollectEvents()
	assert.Equal(t, len(records), 3)
	var requestFound, appFound, nodeFound bool
	for _, record := range records {
		msg := record.Message
		assert.Assert(t, strings.Contains(msg, allocKey), "allocation key not found in event message")
		assert.Assert(t, strings.Contains(msg, appID), "app ID not found in event message")
		assert.Assert(t, strings.Contains(msg, nodeID), "node ID not found in event message")
		switch {
		case record.ObjectID == allocKey:
			requestFound = true
			assert.Equal(t, record.Type, si.EventRecord_REQUEST)
			assert.Equal(t, record.GroupID, appID)
		case record.ObjectID == appID:
			appFound = true
			assert.Equal(t, record.Type, si.EventRecord_APP)
		case record.ObjectID == nodeID:
			nodeFound = true
			assert.Equal(t, record.Type, si.EventRecord_NODE)
		default:
			t.Fatalf("unexpected event found")
		}
	}
	assert.Assert(t, requestFound, "request event not found")
	assert.Assert(t, appFound, "app event not found")
	assert.Assert(t, nodeFound, "node not found")
}
