package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

func TestRPCStubsAcrossServices(t *testing.T) {
	source := `function A(){B.apply(this,arguments);this.i="OrderService";this.search=reg(this,"search");this.get=reg(this,"get");this.cancel=reg(this,"cancelOrder")}` +
		`function C(){B.apply(this,arguments);this.i="InvoiceService";this.get=reg(this,"get");this.pay=other(this,"payInvoice");this.send=reg(this,'sendInvoice')}`
	got := Analyze([]byte(source))
	want := []string{"InvoiceService.get", "InvoiceService.sendInvoice", "OrderService.cancelOrder", "OrderService.get", "OrderService.search"}
	if !reflect.DeepEqual(got.RPCMethods, want) {
		t.Fatalf("methods: got %#v want %#v", got.RPCMethods, want)
	}
	if got.Stats.RPCMethods != len(want) {
		t.Errorf("stats = %d", got.Stats.RPCMethods)
	}
}

func TestRPCStubHelpersAreCountedFileWide(t *testing.T) {
	// A service with one method is still a service once the helper has shown
	// that it registers methods.
	source := `function A(){this.i="OrderService";this.a=reg(this,"one");this.b=reg(this,"two")}` +
		`function B(){this.i="UserService";this.a=reg(this,"whoami")}`
	want := []string{"OrderService.one", "OrderService.two", "UserService.whoami"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	// Two registrations in all are too few to call a helper a stub helper.
	if got := RPCMethods(`function A(){this.i="OrderService";this.a=reg(this,"one");this.b=reg(this,"two")}`); got != nil {
		t.Fatalf("two registrations: %#v", got)
	}
}

func TestRPCStubsNeedAServiceNameInTheSameBody(t *testing.T) {
	source := `function A(){this.a=bind(this,"onClick");this.b=bind(this,"onKey");this.c=bind(this,"render");this.d=bind(this,"close")}` +
		`var name="OrderService";` +
		`class V{constructor(){_.bindAll(this,"render");this.x=Object.defineProperty(this,"y");Base.call(this,"z");this.q=new Thing(this,"event")}}` +
		`function D(){this.i="OrderService";this.i2="UserService";this.a=reg(this,"one");this.b=reg(this,"two");this.c=reg(this,"three")}`
	if got := RPCMethods(source); got != nil {
		t.Fatalf("false positives: %#v", got)
	}
}

func TestRPCStubsIgnoreNonRegistrationShapes(t *testing.T) {
	source := `function A(){this.i="OrderService";` +
		`this.a=reg(this,"one","extra");this.b=reg(this,name);this.c=reg(that,"two");this.d=reg(this,"a-b");this.e=obj.reg(this,"three");this.f=reg(this,"4x");` +
		`this.g=__publicField(this,"field",1)}`
	if got := RPCMethods(source); got != nil {
		t.Fatalf("got %#v", got)
	}
}

func TestRPCStubsStayInsideTheirFunctionBody(t *testing.T) {
	// The service name belongs to a different function, not to the registrations.
	source := `function A(){this.i="OrderService"}function B(){this.a=reg(this,"one");this.b=reg(this,"two");this.c=reg(this,"three")}`
	if got := RPCMethods(source); got != nil {
		t.Fatalf("got %#v", got)
	}
	// Braces inside strings do not end a body.
	source = `function A(){this.i="OrderService";this.t="{";this.a=reg(this,"one");this.b=reg(this,"}");this.c=reg(this,"two");this.d=reg(this,"three")}`
	if got := RPCMethods(source); !reflect.DeepEqual(got, []string{"OrderService.one", "OrderService.three", "OrderService.two"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestRPCDottedNames(t *testing.T) {
	source := `a("OrderService.ListOrders");b('x.InvoiceService.Pay');c("pkg.TaskService.Run");d("UserService.Get");` +
		`e("Symbol.iterator");f("Intl.DateTimeFormat");g("React.Fragment");h("Error.ZoneAwareError");i("keydown.Enter");j("Service.Run");` +
		`k("OrderService.listOrders");l("OrderService.");m("OrderService.Get Thing");n("/OrderService.Cancel");o(OrderService.Cancel)`
	want := []string{"OrderService.ListOrders", "UserService.Get"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRPCMethodsAreNotPathStyleEndpoints(t *testing.T) {
	result := Analyze([]byte(`fetch("/pkg.v1.OrderService/ListOrders");x("OrderService.CancelOrder")`))
	if !reflect.DeepEqual(result.Endpoints, []string{"/pkg.v1.OrderService/ListOrders"}) {
		t.Fatalf("endpoints: %#v", result.Endpoints)
	}
	if !reflect.DeepEqual(result.RPCMethods, []string{"OrderService.CancelOrder"}) {
		t.Fatalf("methods: %#v", result.RPCMethods)
	}
}

func TestNoRPCMethodsLeavesResultUnchanged(t *testing.T) {
	result := Analyze([]byte(`class A{constructor(){__publicField(this,"x");this.s=bind(this,"a")}} const s="ServiceWorker"`))
	if result.RPCMethods != nil || result.Stats.RPCMethods != 0 {
		t.Fatalf("got %#v", result)
	}
}

func TestRPCScopingIsBounded(t *testing.T) {
	// A registration with no braces around it within the scan limit is skipped
	// without scanning the whole input.
	source := `"OrderService";reg(this,"a");reg(this,"b");reg(this,"c");` + strings.Repeat("x", 3*maxBodyScan)
	if got := RPCMethods(source); got != nil {
		t.Fatalf("got %#v", got)
	}
}
