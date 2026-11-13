package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

func TestRPCPathShapes(t *testing.T) {
	for path, want := range map[string]string{
		"/acme.v1.Billing/Charge":        "Billing.Charge",
		"/$rpc/acme.Greeter/SayHello":    "Greeter.SayHello",
		"/twirp/acme.v1.Ledger/Post":     "Ledger.Post",
		"/api/v2/acme.Courier/Ship2":     "Courier.Ship2",
		"acme.Courier/Ship":              "Courier.Ship",
		"https://x.test/acme.Ledger/Add": "Ledger.Add",
		"${base}/acme.Ledger/Add":        "Ledger.Add",
		"/Greeter.SayHello":              "Greeter.SayHello",
		"/UserApi.GetUser":               "UserApi.GetUser",
	} {
		service, method, ok := RPCPath(path)
		if !ok || service+"."+method != want {
			t.Errorf("%q = %q %q %v, want %s", path, service, method, ok, want)
		}
	}
	for _, path := range []string{
		"/Settings/Profile", "/Billing/Charge", "/pkg/Billing/Charge", "/Index.Html", "/Data.json", "/api/Greeter.SayHello",
		"/Acme.Billing/Charge", "/acme.billing/Charge", "/acme.Billing/charge", "/acme.Billing/", "acme.Billing/Charge now",
		"/a b/acme.Billing/Charge", "//Billing.Charge", "/Billing.charge", "/billing.Charge", "/Billing.Charge/", "Billing.Charge", "",
	} {
		if service, method, ok := RPCPath(path); ok {
			t.Errorf("%q read as %q %q", path, service, method)
		}
	}
}

func TestRPCPathsAnyServiceNameAreMethodsAndEndpoints(t *testing.T) {
	source := `a.get(b+"/$rpc/acme.Greeter/SayHello",c);fetch("/acme.v1.Billing/Charge");d("/Ledger.Post");e("/Settings/Profile");f("/Index.Html")`
	result := Analyze([]byte(source))
	want := []string{"Billing.Charge", "Greeter.SayHello", "Ledger.Post"}
	if !reflect.DeepEqual(result.RPCMethods, want) {
		t.Fatalf("methods = %#v, want %#v", result.RPCMethods, want)
	}
	// The literal path is what a reader requests, so it is an endpoint too,
	// including the "$" path that the endpoint rules alone reject.
	for _, path := range []string{"/$rpc/acme.Greeter/SayHello", "/acme.v1.Billing/Charge", "/Ledger.Post"} {
		found := false
		for _, endpoint := range result.Endpoints {
			found = found || endpoint == path
		}
		if !found {
			t.Errorf("endpoint %q missing from %#v", path, result.Endpoints)
		}
	}
	if result.Stats.RPCMethods != 3 {
		t.Errorf("stats = %d", result.Stats.RPCMethods)
	}
}

func TestRPCWireIDsPairByCallParentheses(t *testing.T) {
	class := `class extends Base{constructor(a){super(a);this.x=[1,"/Decoy.Skip"];this.y=f("zz9ab",[2]);}}`
	source := `new Ctor("aB3xYz",Req,Res,[a,!1,"/Billing.Charge"]);` +
		`var x=new Ctor("Qw3Rt",Req,Res,[a,!1,"/Billing.Refund"]);` +
		`new Ctor("LONGid1",ReqA,` + class + `,[b,"/Greeter.SayHello"]);` +
		`call("noids",[a,"/Ledger.Post"]);` + // first argument is not a call with a following array of this shape
		`other(x,[a,"/Courier.Ship"]);` + // no id
		`g("ab",[a,"/Courier.Track"]);` + // id too short
		`h("not an id",[a,"/Courier.Cancel"]);`
	rpc := ReadRPC(source)
	want := map[string]string{"Billing.Charge": "aB3xYz", "Billing.Refund": "Qw3Rt", "Greeter.SayHello": "LONGid1", "Ledger.Post": "noids"}
	if !reflect.DeepEqual(rpc.WireIDs, want) {
		t.Fatalf("ids = %#v, want %#v", rpc.WireIDs, want)
	}
	for _, method := range []string{"Decoy.Skip", "Courier.Ship", "Courier.Track", "Courier.Cancel"} {
		if _, ok := rpc.WireIDs[method]; ok {
			t.Errorf("%s has an id", method)
		}
	}
	// The path in the class expression is not the last argument: no id, and it
	// is still a method.
	if !strings.Contains(strings.Join(rpc.Methods, " "), "Decoy.Skip") {
		t.Errorf("methods = %v", rpc.Methods)
	}
}

func TestRPCWireIDNeedsTheArrayToBeTheLastArgument(t *testing.T) {
	for _, source := range []string{
		`new Ctor("aB3xYz",[a,"/Billing.Charge"],extra)`,
		`new Ctor("aB3xYz",{k:[a,"/Billing.Charge"]})`,
		`new Ctor("aB3xYz",Req,"/Billing.Charge")`,
	} {
		if rpc := ReadRPC(source); len(rpc.WireIDs) != 0 {
			t.Errorf("%s: ids %v", source, rpc.WireIDs)
		}
	}
	// Strings, comments, and nested calls inside the arguments do not confuse the match.
	source := `new Ctor("aB3xYz",f(")]"),/* ) */ g(a,[1,2]),"(",[a,"/Billing.Charge"])`
	if got := ReadRPC(source).WireIDs["Billing.Charge"]; got != "aB3xYz" {
		t.Errorf("id = %q", got)
	}
	// A method registered under two ids carries both.
	both := `new C("aaa1",[x,"/Billing.Charge"]);new C("bbb2",[x,"/Billing.Charge"])`
	if got := ReadRPC(both).WireIDs["Billing.Charge"]; got != "aaa1,bbb2" {
		t.Errorf("ids = %q", got)
	}
}

func TestRPCStubsResolveTheHelperWithoutASuffix(t *testing.T) {
	source := `function reg(a,b){return function(){return a.client.call(a.svc,b,Array.prototype.slice.call(arguments))}}` +
		`function A(){Base.apply(this,arguments);this.svc="Billing";this.charge=reg(this,"charge")}` +
		`function B(){Base.apply(this,arguments);this.svc="acme.v1.Greeter";this.hello=reg(this,"sayHello");this.bye=reg(this,'sayBye')}`
	want := []string{"Billing.charge", "Greeter.sayBye", "Greeter.sayHello"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	// Other definition shapes: assigned function expression, either argument order,
	// and a property that is not the one the helper forwards.
	for _, helper := range []string{
		`var reg=function(a,b){return function(){return a.c.call(b,a.svc)}}`,
		`o={reg:function(x,y){return function(...r){return x.c.go(x.svc,y,r)}}};reg=o.reg`,
	} {
		got := RPCMethods(helper + `;function A(){this.svc="Billing";this.charge=reg(this,"charge")}`)
		if !reflect.DeepEqual(got, []string{"Billing.charge"}) {
			t.Errorf("%s: got %#v", helper, got)
		}
	}
	wrong := `function reg(a,b){return function(){return a.client.call(a.svc,b)}}function A(){this.other="Billing";this.charge=reg(this,"charge")}`
	if got := RPCMethods(wrong); got != nil {
		t.Errorf("other property: %#v", got)
	}
	// The property must hold exactly one string.
	two := `function reg(a,b){return function(){return a.client.call(a.svc,b)}}function A(){this.svc="Billing";this.svc="Ledger";this.charge=reg(this,"charge")}`
	if got := RPCMethods(two); got != nil {
		t.Errorf("two strings: %#v", got)
	}
	// A function that does not forward the property and the method together is not a helper.
	notHelper := `function reg(a,b){return function(){return a.client.call(a.svc)}}function A(){this.svc="Billing";this.charge=reg(this,"charge")}`
	if got := RPCMethods(notHelper); got != nil {
		t.Errorf("not a helper: %#v", got)
	}
}

func TestRPCStubsFallBackToWeakEvidenceWhenTheHelperIsUnknown(t *testing.T) {
	source := `function A(){Base.apply(this,arguments);this.i="OrderService";this.search=reg(this,"search");this.get=reg(this,"get");this.cancel=reg(this,"cancelOrder")}` +
		`function C(){Base.apply(this,arguments);this.i="InvoiceApi";this.get=reg(this,"get");this.pay=other(this,"payInvoice");this.send=reg(this,'sendInvoice')}`
	got := Analyze([]byte(source))
	want := []string{"InvoiceApi.get", "InvoiceApi.sendInvoice", "OrderService.cancelOrder", "OrderService.get", "OrderService.search"}
	if !reflect.DeepEqual(got.RPCMethods, want) {
		t.Fatalf("methods: got %#v want %#v", got.RPCMethods, want)
	}
	if got.Stats.RPCMethods != len(want) {
		t.Errorf("stats = %d", got.Stats.RPCMethods)
	}
}

func TestRPCStubFallbackNeedsAServiceLikeNameAndEnoughRegistrations(t *testing.T) {
	// A name without a service-like ending is not enough when the helper is unknown.
	if got := RPCMethods(`function A(){this.i="Billing";this.a=reg(this,"one");this.b=reg(this,"two");this.c=reg(this,"three")}`); got != nil {
		t.Errorf("no suffix: %#v", got)
	}
	// A helper counts after it has registered methods in bodies with a service name.
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

func TestRPCStubsIgnoreBindersFieldsAndEvents(t *testing.T) {
	source := `function reg(a,b){return function(){return a.client.call(a.svc,b)}}` +
		`function A(){this.a=bind(this,"onClick");this.b=bind(this,"onKey");this.c=bind(this,"render");this.d=bind(this,"close")}` +
		`var name="OrderService";` +
		`class V{constructor(){this.svc="Billing";_.bindAll(this,"render");Object.defineProperty(this,"y");Base.call(this,"z");this.q=new Thing(this,"event")}}` +
		`function D(){this.i="OrderService";this.i2="UserService";this.a=x(this,"one");this.b=x(this,"two");this.c=x(this,"three")}` +
		`class F{constructor(){__publicField(this,"a");__publicField(this,"b");__publicField(this,"c");__publicField(this,"d");this.name="BillingService"}}` +
		`function E(){this.svc="Billing";this.a=emit(this,"opened");this.b=emit(this,"closed");this.c=emit(this,"moved");this.d=bind(this,"x")}` +
		`function G(){this.svc="BillingService";this.a=bind(this,"one");this.b=bind(this,"two");this.c=bind(this,"three")}`
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

func TestRPCDottedNamesNeedEvidence(t *testing.T) {
	// A service-like name is evidence on its own.
	source := `a("OrderService.ListOrders");b('x.InvoiceService.Pay');c("pkg.TaskService.Run");d("UserApi.Get");` +
		`e("Symbol.iterator");f("Intl.DateTimeFormat");g("React.Fragment");h("Error.ZoneAwareError");i("keydown.Enter");j("Service.Run");` +
		`k("OrderService.listOrders");l("OrderService.");m("OrderService.Get Thing");o(OrderService.Cancel);p("Math.PI");q("AsyncContext.Snapshot");r("Badge.Info")`
	want := []string{"OrderService.ListOrders", "UserApi.Get"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	// A name that another recognizer declares as a service is evidence too, with
	// no suffix; the same strings alone are not.
	dotted := `call(a,"Billing.Refund");call(a,"Billing.Charge");call(a,"Greeter.Wave")`
	if got := RPCMethods(dotted); got != nil {
		t.Fatalf("dotted alone: %#v", got)
	}
	got := RPCMethods(dotted + `;fetch("/acme.v1.Billing/Void")`)
	if !reflect.DeepEqual(got, []string{"Billing.Charge", "Billing.Refund", "Billing.Void"}) {
		t.Fatalf("with a declared service: %#v", got)
	}
	// Platform globals never join, even when a path names them.
	if got := RPCMethods(`x("Intl.DateTimeFormat");y("/acme.Intl/Format")`); !reflect.DeepEqual(got, []string{"Intl.Format"}) {
		t.Fatalf("platform: %#v", got)
	}
}

func TestRPCConnectDescriptors(t *testing.T) {
	source := `const D={typeName:"acme.v1.Billing",methods:{charge:{name:"Charge",I:A,O:B,kind:n.MethodKind.Unary},watch:{name:"Watch",I:A,O:B,kind:n.MethodKind.ServerStreaming}}};` +
		"export const Greeter = {\n  typeName: 'Greeter',\n  methods: {\n    sayHello: { name: \"SayHello\", I: HelloRequest, O: HelloReply, kind: MethodKind.Unary },\n  },\n};"
	want := []string{"Billing.Charge", "Billing.Watch", "Greeter.SayHello"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, source := range []string{
		`const D={typeName:"Billing",methods:{charge:{name:"Charge",I:A,O:B}}}`,                      // no kind
		`const D={typeName:"Billing",other:{charge:{name:"Charge",kind:MethodKind.Unary}}}`,          // not the methods map
		`const D={typeName:"billing",methods:{charge:{name:"Charge",kind:MethodKind.Unary}}}`,        // not a service name
		`const D={typeName:"div",enter:1,leave:2}`,                                                   // an unrelated typeName
		`const D={typeName:"Billing",methods:{charge:{label:"Charge",kind:MethodKind.Unary}}}`,       // no name
		`const D={typeName:"Billing",methods:{charge:{meta:{name:"Charge"},kind:MethodKind.Unary}}}`, // name is nested
	} {
		if got := RPCMethods(source); got != nil {
			t.Errorf("%s: got %#v", source, got)
		}
	}
}

func TestRPCJSONRPCMessages(t *testing.T) {
	source := `send({jsonrpc:"2.0",id:1,method:"initialize",params:{method:"POST"}});` +
		`send({"jsonrpc":"2.0","method":"tools/call"});` +
		"const n = {\n  jsonrpc: '2.0',\n  method: 'notifications/ready',\n};" +
		`send({jsonrpc:"2.0",id:2,result:{method:"nested"}});` + // a response: the method is nested
		`send({jsonrpc:"2.0",method:name});` + // not a literal
		`send({jsonrpc:VERSION,method:"x"});` + // not a literal version
		`send({jsonrpc:"1.0",method:"old"});` +
		`send({method:"plain"});`
	want := []string{"JSONRPC.initialize", "JSONRPC.notifications/ready", "JSONRPC.tools/call"}
	if got := RPCMethods(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestRPCMethodsDeduplicateAcrossRecognizers(t *testing.T) {
	source := `fetch("/acme.v1.Billing/Charge");x("/Billing.Charge");const D={typeName:"acme.v1.Billing",methods:{c:{name:"Charge",kind:K.MethodKind.Unary}}}`
	if got := RPCMethods(source); !reflect.DeepEqual(got, []string{"Billing.Charge"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestNoRPCMethodsLeavesResultUnchanged(t *testing.T) {
	result := Analyze([]byte(`class A{constructor(){__publicField(this,"x");this.s=bind(this,"a")}} const s="ServiceWorker";x("/Settings/Profile");y("Symbol.iterator")`))
	if result.RPCMethods != nil || result.RPCWireIDs != nil || result.Stats.RPCMethods != 0 {
		t.Fatalf("got %#v", result)
	}
}

func TestSkipRPCLeavesRPCOut(t *testing.T) {
	result := AnalyzeWithOptions([]byte(`fetch("/acme.Billing/Charge")`), Options{SkipRPC: true})
	if result.RPCMethods != nil || len(result.Endpoints) != 1 {
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
	// So is the search for the call around a path.
	source = `new Ctor("aB3xYz",` + strings.Repeat("x,", maxCallScan) + `[a,"/Billing.Charge"])`
	rpc := ReadRPC(source)
	if len(rpc.WireIDs) != 0 || len(rpc.Methods) != 1 {
		t.Fatalf("got %#v", rpc)
	}
}
