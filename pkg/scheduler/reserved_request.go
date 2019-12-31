/*
Copyright 2019 Cloudera, Inc.  All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import "sync"

// Reserved *single* request
type ReservedSchedulingRequest struct {
    // Public field, will not change overtime
    SchedulingAsk         *SchedulingAllocationAsk
    App                   *SchedulingApplication
    SchedulingNode        *SchedulingNode
    reservationRequestKey string

    // Following are private fields which subject to change

    // Where's the request reserved
    amount int

    lock sync.RWMutex
}

func NewReservedSchedulingRequest(ask *SchedulingAllocationAsk, app *SchedulingApplication, node *SchedulingNode) *ReservedSchedulingRequest {
    return &ReservedSchedulingRequest{
        SchedulingAsk:         ask,
        App:                   app,
        SchedulingNode:        node,
        amount:                1,
        reservationRequestKey: app.ApplicationInfo.ApplicationId + "|" + node.NodeId + "|" + ask.AskProto.AllocationKey,
    }
}

// Increase amount of the reservation, return amount after increase
func (m *ReservedSchedulingRequest) IncAmount() int {
    m.lock.Lock()
    defer m.lock.Unlock()

    m.amount++

    return m.amount
}

// Decrease amount of the reservation, return amount after increase.
// It is a no-op when amount is already <= 0 before decrease, and return false if such thing happens
func (m *ReservedSchedulingRequest) DecAmount() (int, bool) {
    m.lock.Lock()
    defer m.lock.Unlock()
    if m.amount <= 0 {
        return m.amount, false
    }
    m.amount--

    return m.amount, true
}

func (m *ReservedSchedulingRequest) GetAmount() int {
    m.lock.RLock()
    defer m.lock.RUnlock()

    return m.amount
}

// Get a unique key combined of application id and request key.
func (m *ReservedSchedulingRequest) GetReservationRequestKey() string {
    // No lock needed when accessing two final fields
    return m.reservationRequestKey
}

func (m *ReservedSchedulingRequest) Clone() *ReservedSchedulingRequest {
    m.lock.RLock()
    defer m.lock.RUnlock()
    return NewReservedSchedulingRequest(m.SchedulingAsk, m.App, m.SchedulingNode)
}