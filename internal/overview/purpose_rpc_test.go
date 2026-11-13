package overview

import (
	"reflect"
	"strings"
	"testing"
)

// shipmentApp declares RPC methods as service stubs and as dotted names, and
// names the same cancel call as a path-style RPC too.
const shipmentApp = `function ShipmentClient(){Base.apply(this,arguments);this.i="ShipmentService";this.a=reg(this,"listShipments");this.b=reg(this,"cancelShipment");this.c=reg(this,"createShipment")}
var dotted=["ShipmentService.TrackShipment"],path="/acme.v1.ShipmentService/CancelShipment";
var names=["shipmentTable","shipment-list","shipment_row","shipmentCard","shipmentMenu","shipmentForm","shipmentTab","shipment-view"];
var text=["Your shipment was sent.","The shipment could not be saved."];`

func TestRPCMethodsAreAPICallsOfTheirArea(t *testing.T) {
	file := Analyze([]Source{{Name: "shipments.js", Data: []byte(shipmentApp)}}).Files[0]
	if file.Purpose == nil {
		t.Fatal("no purpose")
	}
	shipment, ok := featureByArea(file.Purpose, "shipment")
	if !ok {
		t.Fatalf("shipment area missing: %+v", file.Purpose.Features)
	}
	if shipment.APICalls != 4 || !reflect.DeepEqual(shipment.APIExamples, []string{"ShipmentService"}) {
		t.Errorf("shipment API evidence = %d %v", shipment.APICalls, shipment.APIExamples)
	}
	actions := strings.Join(shipment.Actions, ",")
	for _, action := range []string{"cancel", "create"} {
		if !strings.Contains(actions, action) {
			t.Errorf("shipment actions %v missing %q", shipment.Actions, action)
		}
	}
	// Three stubs and one dotted name; the path-style cancel is the same call
	// as the cancel stub.
	if totals := file.Purpose.Evidence; totals == nil || totals.APICalls != 4 || totals.Screens != 0 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestRPCMethodSeenAsPathIsCountedOnce(t *testing.T) {
	for _, source := range []string{
		`var p="/acme.v1.ShipmentService/CancelShipment",q="/ShipmentService.CancelShipment",r="/ShipmentService/CancelShipment"`,
		`var p="/ShipmentService.CancelShipment";var d="ShipmentService.CancelShipment"`,
	} {
		ev := collectEvidence(&File{}, lex(source), nil)
		ev.addRPCMethods([]string{"ShipmentService.cancelShipment"})
		if len(ev.items) != 1 || ev.items[0].group != "ShipmentService" || ev.items[0].screen {
			t.Errorf("%s: items = %+v", source, ev.items)
		}
	}
}

func TestRPCMethodsGroupByServiceAndSkipNonMethods(t *testing.T) {
	ev := &evidence{claimed: map[string]struct{}{}, byNoun: map[string][]int{}}
	ev.addRPCMethods([]string{"ShipmentService.getLabel", "ShipmentService.cancelShipment", "BasketService.cancelShipment", "NotAMethod"})
	if len(ev.items) != 3 {
		t.Fatalf("items = %+v", ev.items)
	}
	if support := ev.support("shipment"); len(support) != 3 {
		t.Errorf("shipment support = %+v", support)
	}
	if got := apiExamples(ev.items); !reflect.DeepEqual(got, []string{"ShipmentService", "BasketService"}) {
		t.Errorf("examples = %v", got)
	}
}
