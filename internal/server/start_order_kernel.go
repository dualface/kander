package server

import (
	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/statemachine/startorder"
)

// The start order decisions are made by the generated kernel in
// internal/statemachine/startorder (formal/KanderFormal/Machines/StartOrder.lean).
// This file projects a stored order into a kernel state and writes the kernel's
// answer back; start_orders.go selects the error of a refused operation. See
// docs/formal-verification.md.

// startOrderParams are the kernel parameters of every operation: a generation
// maps to 0 for none, 1 for the operation's generation, and 2 for another one.
var startOrderParams = startorder.Params{MaxGen: 2}

var orderStates = map[string]startorder.St{
	"": startorder.StNone, api.StartOrderPending: startorder.StPending, api.StartOrderAccepted: startorder.StAccepted,
	api.StartOrderStarted: startorder.StStarted, api.StartOrderFailed: startorder.StFailed,
	api.StartOrderCancelled: startorder.StCancelled, api.StartOrderExpired: startorder.StExpired,
}

var orderStateNames = map[startorder.St]string{
	startorder.StNone: "", startorder.StPending: api.StartOrderPending, startorder.StAccepted: api.StartOrderAccepted,
	startorder.StStarted: api.StartOrderStarted, startorder.StFailed: api.StartOrderFailed,
	startorder.StCancelled: api.StartOrderCancelled, startorder.StExpired: api.StartOrderExpired,
}

// orderState projects o for an operation of generation gen (0 for an operation
// without one). An unknown stored state projects as final failed, so the kernel
// refuses every transition of an order a newer server wrote.
func orderState(o api.StartOrder, gen uint64) startorder.State {
	st, ok := orderStates[o.State]
	if !ok {
		st = startorder.StFailed
	}
	return startorder.State{St: st, Bgen: bucket(o.Generation, gen)}
}

// stepOrder asks the kernel to apply e to o for an operation of generation gen.
// An accepted event writes the new state back, and a claim binds gen.
func stepOrder(o *api.StartOrder, e startorder.Event, gen uint64) bool {
	next, ok := startorder.Step(orderState(*o, gen), e, startOrderParams)
	if !ok {
		return false
	}
	o.State = orderStateNames[next.St]
	if next.Bgen == 1 && o.Generation == 0 {
		o.Generation = gen
	}
	return true
}

// orderOpen reports whether o is not finished, by the kernel's query.
func orderOpen(o api.StartOrder) bool {
	return startorder.Open(orderState(o, 0), startOrderParams)
}

// orderCancellable reports whether a cancel would take o, by the kernel's query.
func orderCancellable(o api.StartOrder) bool {
	return startorder.Cancellable(orderState(o, 0), startOrderParams)
}
